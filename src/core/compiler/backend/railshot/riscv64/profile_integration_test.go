//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo && wago_profile

package riscv64

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Validate against decoded opcode boundaries, not just the surrounding function.
// This catches local-declaration rebasing errors and attribution to immediates.
func profileOpcodeOffsets(t *testing.T, m *wasm.Module) []map[uint32]bool {
	t.Helper()
	out := make([]map[uint32]bool, len(m.Code))
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i, fn := range m.Code {
		out[i] = make(map[uint32]bool)
		r := wasm.ReaderFrom(fn.BodyBytes)
		for r.HasNext() {
			out[i][fn.LocalDeclBytes+uint32(r.Offset())] = true
			op, err := r.Byte()
			if err != nil {
				t.Fatal(err)
			}
			var imm wasm.InstructionImmediate
			if err := classifier.ClassifyInto(&r, op, &imm); err != nil {
				t.Fatal(err)
			}
		}
	}
	return out
}

func profileCheckDirectory(t *testing.T, m *wasm.Module, code []byte, stats ModuleStats, imported int, sources bool) {
	t.Helper()
	if err := jitprofile.ValidateRegions(stats.ProfileRegions, uint64(len(code))); err != nil {
		t.Fatal(err)
	}
	if err := jitprofile.ValidateSources(stats.SourceRanges, uint64(len(code))); err != nil {
		t.Fatal(err)
	}
	if len(stats.ProfileRegions) == 0 || len(stats.Funcs) != len(m.Code) {
		t.Fatal("missing finalized directories")
	}
	if stats.CodeSites != nil || stats.UnwindRanges != nil || stats.SourceFrames != nil {
		t.Fatal("invented sites, inline frames, or unwind rules")
	}
	if sources != (len(stats.SourceRanges) != 0) {
		t.Fatal("source option did not control sparse map")
	}
	opcodes := profileOpcodeOffsets(t, m)
	for _, region := range stats.ProfileRegions {
		if region.Kind == "padding" {
			if region.Function != -1 {
				t.Fatal("padding has a function owner")
			}
			continue
		}
		if region.Function < imported || region.Function >= imported+len(m.Code) {
			t.Fatal("region omitted import index prefix", region)
		}
		if region.Kind != "guest-body" && region.Kind != "cold-trap" && region.Kind != "runtime-helper" {
			t.Fatal("unknown finalized region", region)
		}
	}
	for _, source := range stats.SourceRanges {
		index := int(source.Function) - imported
		if index < 0 || index >= len(opcodes) || !opcodes[index][source.WasmOffset] || source.InlineParent != 0 {
			t.Fatal("source is not an original opcode", source)
		}
		covered := uint64(0)
		for _, region := range stats.ProfileRegions {
			start, end := max(source.Offset, region.Offset), min(source.Offset+source.Size, region.Offset+region.Size)
			if start >= end {
				continue
			}
			if region.Function != int(source.Function) || region.Kind != "guest-body" && region.Kind != "cold-trap" {
				t.Fatal("source covers a wrapper, helper, padding, or wrong owner", source, region)
			}
			covered += end - start
		}
		if covered != source.Size {
			t.Fatal("source escaped region partition", source)
		}
	}
}

func TestProductionProfileFinalizedCodeAndSources(t *testing.T) {
	sharedSource := binaryModule(
		fixtureFunc{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}},
		fixtureFunc{params: []byte{0x7e}, result: 0x7e, body: []byte{0x20, 0, 0x0b}},
	)
	for _, tc := range []struct {
		name     string
		source   []byte
		fallback bool
		imports  int
		foreign  bool
	}{
		{"shared", sharedSource, false, 0, false},
		{"fallback", sharedSource, true, 0, false},
		{"direct", binaryModule(sumCallFixture(0)), false, 0, false},
		{"host", hostImportModule(2, 1, []byte{0x7e}, true), false, 2, false},
		{"fp", binaryModule(floatFixture(false, 0x92, false, false), floatFixture(true, 0xa0, false, false)), false, 0, false},
		{"swar-large-frame", binaryModule(swarCallFixtures()...), false, 0, false},
		{"native-table-helper", ownedTableFixture(), false, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := statsModule(t, tc.source)
			opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: tc.foreign, ForceFallback: tc.fallback, ImportBindings: hostImportBindings(tc.imports)}
			plain, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			for _, sources := range []bool{false, true} {
				for _, heap := range []bool{false, true} {
					t.Run(fmt.Sprintf("sources%t-heap%t", sources, heap), func(t *testing.T) {
						var stats ModuleStats
						opts.Profile, opts.SourceMaps, opts.Stats, opts.DeferCodeMapping = true, sources, &stats, heap
						image, err := CompileModuleWith(m, opts)
						if err != nil {
							t.Fatal(err)
						}
						if image.CodeImage != nil {
							defer image.CodeImage.Close()
						}
						if !bytes.Equal(image.Code, plain.Code) || !reflect.DeepEqual(image.Entry, plain.Entry) || !reflect.DeepEqual(image.InternalEntry, plain.InternalEntry) {
							t.Fatal("profile changed emitted code or entries")
						}
						profileCheckDirectory(t, m, image.Code, stats, tc.imports, sources)
						if tc.foreign {
							// There must be a helper between two body spans: entry/exit
							// wrappers alone cannot satisfy the inline-thunk gap check.
							found := false
							for i := 1; i+1 < len(stats.ProfileRegions); i++ {
								a, b, c := stats.ProfileRegions[i-1], stats.ProfileRegions[i], stats.ProfileRegions[i+1]
								found = found || a.Kind == "guest-body" && b.Kind == "runtime-helper" && c.Kind == "guest-body" && a.Function == b.Function && b.Function == c.Function
							}
							if !found {
								t.Fatal("native table thunk lacks explicit source gap")
							}
						}
					})
				}
			}
		})
	}
}

func TestProductionProfileFoldedAndColdOrigins(t *testing.T) {
	folded := fixtureFunc{result: 0x7f, body: []byte{0x41, 10, 0x41, 20, 0x6a, 0x0b}}
	m := statsModule(t, binaryModule(folded))
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Profile: true, SourceMaps: true, Stats: &stats, DeferCodeMapping: true}); err != nil {
		t.Fatal(err)
	}
	seen := map[uint32]bool{}
	for _, source := range stats.SourceRanges {
		seen[source.WasmOffset] = true
	}
	base := uint32(m.Code[0].LocalDeclBytes)
	if !reflect.DeepEqual(seen, map[uint32]bool{base + 4: true, base + 5: true}) {
		t.Fatal("folded materialization lost operator origin", seen)
	}

	divide := fixtureFunc{params: []byte{0x7f, 0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x20, 1, 0x6d, 0x0b}}
	for _, vector := range []bool{false, true} {
		functions := []fixtureFunc{divide}
		if vector {
			functions = append(functions, fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}})
		}
		m := statsModule(t, binaryModule(functions...))
		if _, err := CompileModuleWith(m, CompileOptions{Profile: true, SourceMaps: true, Stats: &stats, DeferCodeMapping: true, Interruptible: true}); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, region := range stats.ProfileRegions {
			if region.Function != 0 || region.Kind != "cold-trap" {
				continue
			}
			found = true
			covered := uint64(0)
			for _, source := range stats.SourceRanges {
				start, end := max(source.Offset, region.Offset), min(source.Offset+source.Size, region.Offset+region.Size)
				if start >= end {
					continue
				}
				if source.Function != 0 || source.WasmOffset != uint32(m.Code[0].LocalDeclBytes+4) {
					t.Fatal("cold trap inherited later opcode", vector, source)
				}
				covered += end - start
			}
			if covered != region.Size {
				t.Fatal("cold trap source extent mismatch", vector, region)
			}
		}
		if !found {
			t.Fatal("division fixture lacks cold trap", vector)
		}
	}
}

func TestProductionProfileFailureBeforeStagingAndSnapshotOwnership(t *testing.T) {
	m := statsModule(t, binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 7, 0x0b}}))
	var stats ModuleStats
	opts := CompileOptions{Profile: true, SourceMaps: true, Stats: &stats, DeferCodeMapping: true}
	if _, err := CompileModuleWith(m, opts); err != nil {
		t.Fatal(err)
	}
	prior := stats
	regions, sources := slices.Clone(stats.ProfileRegions), slices.Clone(stats.SourceRanges)
	for _, bad := range []CompileOptions{{Profile: true, UnwindMaps: true}, {Profile: true, CollectInlineReport: true}, {SourceMaps: true}} {
		bad.Stats, bad.DeferCodeMapping = &stats, true
		staged := false
		bad.MemoryPressure = func() { staged = true }
		if image, err := CompileModuleWith(m, bad); image != nil || !errors.Is(err, ErrUnsupported) || staged || !reflect.DeepEqual(stats, prior) {
			t.Fatal("unsupported option published metadata", err)
		}
	}
	// Inject malformed compiler-owned layout: preparation must reject it before
	// even invoking the staging callback, and leave the prior sink untouched.
	c := &Compiled{profileModuleState: newProfileModule(true, true, 1), code: make([]byte, 4)}
	staged := false
	bad := opts
	bad.MemoryPressure = func() { staged = true }
	if image, err := stageProductionModule(c, bad); image != nil || err == nil || staged || !reflect.DeepEqual(stats, prior) {
		t.Fatal("invalid finalized layout reached staging", err)
	}
	// Reusing a destination and compiler worker must not mutate older snapshots.
	other := statsModule(t, binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 12, 0x0b}}, fixtureFunc{result: 0x7e, body: []byte{0x42, 31, 0x0b}}))
	if _, err := CompileModuleWith(other, opts); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prior.ProfileRegions, regions) || !reflect.DeepEqual(prior.SourceRanges, sources) {
		t.Fatal("new compile overwrote prior profile backing")
	}
	if _, err := CompileModuleWith(statsModule(t, wasmtest.Module()), opts); err != nil || !reflect.DeepEqual(stats, ModuleStats{}) {
		t.Fatal("empty module retained previous profile", err, stats)
	}
}

func TestProductionProfileDisabledCaptureAllocatesNothing(t *testing.T) {
	if got := testing.AllocsPerRun(10, func() {
		p := newProfileModule(false, false, MaxFunctions)
		if p.profile != nil {
			panic("disabled capture allocated")
		}
		if err := p.prepareProfile(0); err != nil {
			panic(err)
		}
	}); got != 0 {
		t.Fatal("nil profile capture allocated", got)
	}
}

func TestProductionProfileReleasesCompilerDirectories(t *testing.T) {
	m := statsModule(t, binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 7, 0x0b}}))
	opts := CompileOptions{production: true, publicStarts: true, Profile: true, SourceMaps: true, DeferCodeMapping: true}
	c, err := compileDecodedModule(m, scalarRegisters[:], opts)
	if err != nil {
		t.Fatal(err)
	}
	if c.profile == nil || len(c.profile.functions) != 1 || c.profile.shared.Memory() == 0 {
		t.Fatal("fixture did not retain compile-time source scratch")
	}
	before := c.profile.functions[0]
	for _, fn := range c.functions {
		if fn.mode.function != nil || fn.mode.profile != nil {
			t.Fatal("FunctionInfo retained source capture")
		}
	}
	var stats ModuleStats
	opts.Stats = &stats
	if _, err := stageProductionModule(c, opts); err != nil {
		t.Fatal(err)
	}
	if c.profile.functions != nil || c.profile.shared.Memory() != 0 {
		t.Fatal("staged image retained compiler directories")
	}
	regions, sources := slices.Clone(stats.ProfileRegions), slices.Clone(stats.SourceRanges)
	before.BodyRegions[0].Size = 0
	before.Sources[0].WasmOffset = ^uint32(0)
	if !reflect.DeepEqual(stats.ProfileRegions, regions) || !reflect.DeepEqual(stats.SourceRanges, sources) {
		t.Fatal("published snapshot aliases compiler layout scratch")
	}
}

func TestProductionProfileRetainedRangeBudgetIsTransactional(t *testing.T) {
	m := statsModule(t, binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 7, 0x0b}}))
	c, err := compileDecodedModule(m, scalarRegisters[:], CompileOptions{production: true, publicStarts: true, Profile: true, SourceMaps: true})
	if err != nil {
		t.Fatal(err)
	}
	layout := c.profile.functions[0]
	if len(layout.Sources) == 0 {
		t.Fatal("fixture needs a source range")
	}
	for _, sourceBound := range []bool{false, true} {
		p := newProfileModule(true, true, 1)
		if sourceBound {
			p.profile.budget.sources = maxProfileModuleRanges
		} else {
			p.profile.budget.regions = maxProfileModuleRanges
		}
		before := p.profile.budget
		mode := p.newFunctionProfile(&m.Code[0], 0)
		mode.function.layout, mode.function.shared = layout, true
		info := FunctionInfo{Entry: layout.Entry, CodeBytes: layout.CodeBytes, mode: emissionMode{profileMode: mode}}
		if err := p.retainProfile(&info); err == nil {
			t.Fatal("module range cap accepted", sourceBound)
		}
		if p.profile.budget != before || len(p.profile.functions) != 0 || info.mode.function != mode.function || &mode.function.layout.Sources[0] != &layout.Sources[0] {
			t.Fatal("failed charge retained/copy-transferred partial metadata", sourceBound)
		}
	}
}
