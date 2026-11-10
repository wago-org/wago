//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func statsModule(t *testing.T, data []byte) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestProductionStatsOptionGates(t *testing.T) {
	m := statsModule(t, binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 7, 0x0b}}))
	for _, opts := range []CompileOptions{
		{Profile: true}, {SourceMaps: true}, {UnwindMaps: true}, {CollectInlineReport: true},
	} {
		if opts.Profile && profileEnabled {
			continue
		}
		stats := ModuleStats{Funcs: []*CodegenStats{{CodeBytes: 71}}}
		before := stats
		opts.Stats, opts.DeferCodeMapping = &stats, true
		staged := false
		opts.MemoryPressure = func() { staged = true }
		image, err := CompileModuleWith(m, opts)
		if image != nil || !errors.Is(err, ErrUnsupported) || staged || !reflect.DeepEqual(stats, before) {
			t.Fatal("unsupported diagnostics reached staging or changed sink", opts, err)
		}
	}
	stats := ModuleStats{Funcs: []*CodegenStats{{CodeBytes: 71}}}
	image, err := CompileModuleWith(m, CompileOptions{Stats: &stats, DeferCodeMapping: true})
	if diagnosticsEnabled {
		if err != nil || image == nil || len(stats.Funcs) != 1 || stats.Funcs[0].CodeBytes != len(image.Code) {
			t.Fatal("enabled sizes-only diagnostics", err)
		}
	} else if image != nil || !errors.Is(err, ErrUnsupported) || stats.Funcs[0].CodeBytes != 71 {
		t.Fatal("ordinary build accepted diagnostics or changed sink", err)
	}
}

func TestProductionFinalizedStatsPreserveCode(t *testing.T) {
	if !diagnosticsEnabled {
		t.Skip("compiler diagnostics omitted")
	}
	sharedSource := binaryModule(
		fixtureFunc{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}},
		fixtureFunc{params: []byte{0x7e}, result: 0x7e, body: []byte{0x20, 0, 0x0b}},
	)
	for _, tc := range []struct {
		name     string
		source   []byte
		fallback bool
		imports  int
	}{
		{"shared", sharedSource, false, 0},
		{"integer", sharedSource, true, 0},
		{"direct", binaryModule(sumCallFixture(0)), false, 0},
		{"import", hostImportModule(2, 1, []byte{0x7e}, true), false, 2},
		{"swar", binaryModule(swarCallFixtures()...), false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := statsModule(t, tc.source)
			opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, ForceFallback: tc.fallback, ImportBindings: hostImportBindings(tc.imports)}
			plain, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			for _, heap := range []bool{false, true} {
				var stats ModuleStats
				opts.DeferCodeMapping, opts.Stats = heap, &stats
				image, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				if image.CodeImage != nil {
					t.Cleanup(func() { _ = image.CodeImage.Close() })
				}
				if !bytes.Equal(image.Code, plain.Code) || !reflect.DeepEqual(image.Entry, plain.Entry) || !reflect.DeepEqual(image.InternalEntry, plain.InternalEntry) {
					t.Fatal("statistics changed emitted code or entries")
				}
				if len(stats.Funcs) != len(m.Code) || len(stats.Funcs) != len(image.Entry) {
					t.Fatal("import prefix leaked into local stats")
				}
				end, functionBytes := 0, 0
				for i, function := range stats.Funcs {
					if function == nil || function.FuncIdx != i || function.CodeBytes <= 0 || function.CodeBytes&3 != 0 || function.FrameBytes <= 0 || function.FrameBytes&15 != 0 || function.FrameBytes > MaxFrameBytes || function.GCCodeBytes.Total != function.CodeBytes {
						t.Fatal("invalid finalized function measurement", i, function)
					}
					start := image.Entry[i]
					if start < end || start-end >= 16 {
						t.Fatal("function intervals overlap or omit nonalignment bytes")
					}
					for p := end; p < start; p += 4 {
						if !bytes.Equal(image.Code[p:p+4], []byte{0x13, 0, 0, 0}) {
							t.Fatal("unattributed bytes are not alignment")
						}
					}
					end = start + function.CodeBytes
					functionBytes += function.CodeBytes
					if function.Name != "" || function.Spills != 0 || function.Reloads != 0 || function.BoundsChecks != 0 || function.Calls != nil || function.Peephole != nil || function.FinalizerFallback != "" || function.GCCodeBytes != (shared.GCNativeCodeBytes{Total: function.CodeBytes}) {
						t.Fatal("unmeasured attribution populated", function)
					}
				}
				if end != len(image.Code) || functionBytes > len(image.Code) || len(image.Code)-functionBytes > 12*(len(stats.Funcs)-1) {
					t.Fatal("finalized byte accounting does not cover function image")
				}
				if stats.CodeSites != nil || stats.UnwindRanges != nil || stats.SourceRanges != nil || stats.SourceFrames != nil || stats.ProfileRegions != nil {
					t.Fatal("unqualified profile attribution")
				}
			}
		})
	}
}

func TestFinalizedStatsStorageAndAllocation(t *testing.T) {
	c := &Compiled{functions: make([]FunctionInfo, MaxFunctions)}
	for i := range c.functions {
		c.functions[i] = FunctionInfo{CodeBytes: 64 + i*4, FrameBytes: 48 + i*16, Spills: 99, Reloads: 100, DirectCalls: 11}
	}
	if got := testing.AllocsPerRun(10, func() { collectFinalizedStats(c, nil) }); got != 0 {
		t.Fatal("nil destination allocated", got)
	}
	var stats ModuleStats
	if !diagnosticsEnabled {
		if got := testing.AllocsPerRun(10, func() { collectFinalizedStats(c, &stats) }); got != 0 || stats.Funcs != nil {
			t.Fatal("ordinary diagnostics retained", got)
		}
		return
	}
	if got := testing.AllocsPerRun(10, func() { collectFinalizedStats(c, &stats) }); got != 2 {
		t.Fatal("statistics must own exactly two bounded arrays", got)
	}
	for i, f := range stats.Funcs {
		if f.FuncIdx != i || f.CodeBytes != c.functions[i].CodeBytes || f.FrameBytes != c.functions[i].FrameBytes || f.Spills != 0 || f.Reloads != 0 || f.Calls != nil {
			t.Fatal("measurement differs from final frame or invents operation counts", i)
		}
	}
	c.functions[0].CodeBytes++
	if stats.Funcs[0].CodeBytes != 64 {
		t.Fatal("stats borrowed compiler metadata")
	}
	old := stats.Funcs
	collectFinalizedStats(&Compiled{functions: []FunctionInfo{{CodeBytes: 16, FrameBytes: 32}}}, &stats)
	if len(stats.Funcs) != 1 || stats.Funcs[0].CodeBytes != 16 || old[0].CodeBytes != 64 {
		t.Fatal("reuse overwrote previously published snapshot")
	}
}

func TestProductionStatsResetAndFailure(t *testing.T) {
	if !diagnosticsEnabled {
		t.Skip("compiler diagnostics omitted")
	}
	stats := ModuleStats{
		Funcs:     []*CodegenStats{{CodeBytes: 71, Calls: map[string]int{"stale": 1}}},
		CodeSites: []shared.NativeCodeSite{{}}, UnwindRanges: []jitprofile.UnwindRange{{}},
		SourceRanges: []shared.NativeSourceRange{{}}, SourceFrames: []shared.NativeInlineFrame{{}}, ProfileRegions: []jitprofile.Region{{}},
	}
	before := stats
	m := statsModule(t, binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 7, 0x0b}}))
	m.Code[0].BodyBytes = []byte{0x20, 9, 0x0b}
	if image, err := CompileModuleWith(m, CompileOptions{Stats: &stats, DeferCodeMapping: true}); image != nil || err == nil || !reflect.DeepEqual(stats, before) {
		t.Fatal("failed compile changed successful snapshot", err)
	}
	importOnly := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("f")...), 0, 0))),
	)
	for _, tc := range []struct {
		data    []byte
		imports int
	}{
		{wasmtest.Module(), 0}, {importOnly, 1},
	} {
		stats = before
		image, err := CompileModuleWith(statsModule(t, tc.data), CompileOptions{Stats: &stats, DeferCodeMapping: true, ImportBindings: hostImportBindings(tc.imports)})
		if err != nil || image == nil || len(image.Code) != 0 || !reflect.DeepEqual(stats, ModuleStats{}) {
			t.Fatal("zero-local module retained old attribution", tc.imports, err, stats)
		}
	}
}
