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
			// The decoded shared entry point selects the same bounded SWAR image.
			if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ImportBindings: hostImportBindings(test.imports)}); image == nil || err != nil || !bytes.Equal(image.Code, reference.code) || image.CodeImage != nil {
				t.Fatal("shared SIMD compiler route", err)
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

func TestSWARProductionSelectionUsesInstructionGrammar(t *testing.T) {
	for _, f := range []fixtureFunc{
		{result: 0x7e, locals: []byte{1, 0x81, 1, 0x7f}, body: []byte{0x42, 0xfd, 0, 0x0b}},
		{result: 0x7c, locals: []byte{1, 0x81, 1, 0x7f}, body: []byte{0x44, 0xfd, 0, 0, 0, 0, 0, 0, 0, 0x0b}},
	} {
		m, err := wasm.DecodeModule(binaryModule(f))
		if err != nil {
			t.Fatal(err)
		}
		selected, err := moduleUsesSWAR(m)
		if err != nil || selected {
			t.Fatal("SIMD prefix inside scalar immediate selected SWAR", err)
		}
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
		if err != nil || image == nil {
			t.Fatal("scalar129locals lost admission", err)
		}
	}
	source := binaryModule(fixtureFunc{body: append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x1a, 0x0b)})
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := moduleUsesSWAR(m)
	if err != nil || !selected {
		t.Fatal("SIMD body with scalar signature not selected", err)
	}
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal("SWAR source gate", err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
	if err != nil || image == nil {
		t.Fatal("scalar signature SWAR body", err)
	}
	if image, err := CompileProduction(source, ProductionOptions{DeferCodeMapping: true}); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("isolated byte compiler widened", err)
	}
}

func TestSWARProductionSelectionIncludesInlineVectorTypes(t *testing.T) {
	for _, body := range [][]byte{
		{0x02, 0x7b, 0x00, 0x0b, 0x1a, 0x0b},
		{0x03, 0x7b, 0x00, 0x0b, 0x1a, 0x0b},
		{0x41, 1, 0x04, 0x7b, 0x00, 0x05, 0x00, 0x0b, 0x1a, 0x0b},
		{0x00, 0x41, 1, 0x1c, 1, 0x7b, 0x1a, 0x0b},
	} {
		source := binaryModule(fixtureFunc{body: body})
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal("normally valid inline vector type", err)
		}
		selected, err := moduleUsesSWAR(m)
		if err != nil || !selected {
			t.Fatal("inline vector type not selected", body, err)
		}
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
		if err != nil || image == nil {
			t.Fatal("inline vector compiler route", body, err)
		}
	}
	m, err := wasm.DecodeModule(binaryModule(fixtureFunc{body: []byte{0x0b}}))
	if err != nil {
		t.Fatal(err)
	}
	m.Code[0].BodyBytes = append(uleb([]byte{0x1c}, ^uint32(0)), 0x7b, 0x0b)
	if selected, err := moduleUsesSWAR(m); selected || !errors.Is(err, ErrUnsupported) {
		t.Fatal("unbounded typed select reached classifier", err)
	}
}
