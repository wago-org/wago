//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestSWARProductionCompilerOwnershipAndEntries(t *testing.T) {
	table, _, _, _ := swarCombinedStateSource()
	host, _, _, _, _ := swarHostCombinedStateSource(256)
	for _, test := range []struct {
		name    string
		source  []byte
		policy  swarModulePolicy
		imports int
	}{
		{"calls", binaryModule(swarCallFixtures()...), swarModulePlain, 0},
		{"table", table, swarModuleTables, 0}, {"host", host, swarModuleHosts, 256},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, err := wasm.DecodeModule(test.source)
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := frontend.BuildGCTypeMetadata(m)
			if err != nil {
				t.Fatal(err)
			}
			reference, err := compileSWARModulePolicy(test.source, emissionMode{production: true, interruptible: true}, test.policy)
			if err != nil {
				t.Fatal(err)
			}
			for _, heap := range []bool{false, true} {
				for _, threshold := range []int{0, len(reference.code) + 1} {
					calls := 0
					opts := CompileOptions{DeferCodeMapping: heap, Interruptible: true, Workers: 8, SyncHostSlots: 64, ForceFallback: true, ImportBindings: hostImportBindings(test.imports), MemoryPressureAt: threshold, MemoryPressure: func() { calls++ }, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, Features: codegen.Features{V128: true}}}}
					image, err := compileSWARProductionModule(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					wantCalls := 1
					if threshold > len(reference.code) {
						wantCalls = 0
					}
					if calls != wantCalls || !bytes.Equal(image.Code, reference.code) || len(image.Entry) != len(reference.functions) || len(image.InternalEntry) != len(reference.functions) {
						t.Fatal("shared staging code/pressure/entries", calls, err)
					}
					for i, info := range reference.functions {
						if image.Entry[i] != info.Entry || image.Entry[i]&15 != 0 || image.InternalEntry[i] != info.Entry {
							t.Fatal("framed entry", i)
						}
					}
					if len(image.DirectPrepared) != 0 || len(image.DirectPreparedLight) != 0 || len(image.DirectPreparedBounded) != 0 || image.PreparedIsolatedTables || image.RequiredAMD64Features != 0 || image.RequiresBMI2 || image.RequiresAVX2 || image.RequiresAVX512 || image.RequiresBitCount != 0 {
						t.Fatal("unqualified entry/ISA metadata")
					}
					if heap {
						if image.CodeImage != nil {
							t.Fatal("heap bytes advertised RW ownership")
						}
					} else {
						if image.CodeImage == nil {
							t.Fatal("RW ownership missing")
						}
						mapping, base, err := image.CodeImage.Take()
						if err != nil {
							t.Fatal(err)
						}
						if base == 0 || !bytes.Equal(mapping[:len(image.Code)], reference.code) {
							t.Error("RW transfer changed code")
						}
						if err := coreruntime.Unmap(mapping); err != nil {
							t.Fatal(err)
						}
						if err := image.CodeImage.Close(); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			// Public decoded/source entry points retain the separately qualified scalar gate.
			if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ImportBindings: hostImportBindings(test.imports)}); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("public SIMD gate widened", err)
			}
		})
	}
}

func TestSWARProductionCompilerRejectsDependenciesBeforeStaging(t *testing.T) {
	source := binaryModule(fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}})
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, opts := range []CompileOptions{
		{ElideBoundsChecks: true}, {SyncHostCalls: true}, {ImportBindings: []ImportBinding{{Dynamic: true}}},
		{GCTypeSubtypingRefTest: true}, {GCStructHelpers: true}, {GCArrayHelpers: true},
		{Profile: true}, {SourceMaps: true}, {UnwindMaps: true}, {Stats: &ModuleStats{}}, {CollectInlineReport: true},
		{Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: []gc.TypeDesc{{Kind: gc.KindStruct}}}}},
		{Codegen: codegen.Options{Module: codegen.ModuleInfo{Features: codegen.Features{GC: true}}}},
		{Optimizations: map[string]bool{"reg-abi": true}}, {SyncHostSlots: -1}, {SyncHostSlots: 65536},
	} {
		pressure := 0
		opts.MemoryPressure = func() { pressure++ }
		image, err := compileSWARProductionModule(m, opts)
		if image != nil || err == nil || pressure != 0 {
			t.Fatalf("semantic option reached staging: %+v / %v", opts, err)
		}
	}
	if image, err := compileSWARProductionModule(nil, CompileOptions{}); image != nil || err == nil {
		t.Fatal("nil module admitted")
	}
	bad := binaryModule(fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}}, fixtureFunc{body: []byte{0x00, 0x0b}})
	m, err = wasm.DecodeModule(bad)
	if err != nil {
		t.Fatal(err)
	}
	m.Code[1].BodyBytes = []byte{0x20, 0, 0x0b} // invalid late body, within budget
	pressure := 0
	if image, err := compileSWARProductionModule(m, CompileOptions{MemoryPressure: func() { pressure++ }}); image != nil || err == nil || pressure != 0 {
		t.Fatal("invalid late body staged", err)
	}
	host, _, _, _, _ := swarHostCombinedStateSource(256)
	m, err = wasm.DecodeModule(host)
	if err != nil {
		t.Fatal(err)
	}
	if image, err := compileSWARProductionModule(m, CompileOptions{ImportBindings: hostImportBindings(256), MemoryPressure: func() { pressure++ }}); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
		t.Fatal("host bridge without interrupt context staged", err)
	}
}

func TestSWARProductionCompilerDescriptorPreflight(t *testing.T) {
	m, err := wasm.DecodeModule(binaryModule(fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{MaxFunctions, MaxFunctions + 1} {
		descs := make([]gc.TypeDesc, count)
		for i := range descs {
			descs[i] = gc.TypeDesc{ID: gc.TypeID(i), Kind: gc.KindFunc, Final: true}
		}
		pressure := 0
		image, err := compileSWARProductionModule(m, CompileOptions{DeferCodeMapping: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: descs}}, MemoryPressure: func() { pressure++ }})
		if count == MaxFunctions && (err != nil || image == nil || pressure != 1) || count > MaxFunctions && (image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0) {
			t.Fatal("descriptor count preflight", count, pressure, err)
		}
	}
	// This layout is invalid as well as unsupported. Policy must reject the
	// kind before general GC layout validation scans the provided fields.
	for _, typ := range []gc.TypeKind{gc.KindStruct, gc.KindArray} {
		pressure := 0
		image, err := compileSWARProductionModule(m, CompileOptions{Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: []gc.TypeDesc{{Kind: typ, Final: true, Fields: []gc.FieldDesc{{Kind: 255}}}}}}, MemoryPressure: func() { pressure++ }})
		if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
			t.Fatal("unsupported GC layout reached validation/staging", typ, err)
		}
	}
}
