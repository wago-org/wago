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
	"github.com/wago-org/wago/tests/support/wasmtest"
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
			reference, err := compileSWARProductionModule(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, SyncHostSlots: 64, ForceFallback: true, ImportBindings: hostImportBindings(test.imports), Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, Features: codegen.Features{V128: true}}}})
			if err != nil {
				t.Fatal(err)
			}
			for _, heap := range []bool{false, true} {
				for _, threshold := range []int{0, len(reference.Code) + 1} {
					calls := 0
					opts := CompileOptions{DeferCodeMapping: heap, Interruptible: true, Workers: 8, SyncHostSlots: 64, ForceFallback: true, ImportBindings: hostImportBindings(test.imports), MemoryPressureAt: threshold, MemoryPressure: func() { calls++ }, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, Features: codegen.Features{V128: true}}}}
					image, err := compileSWARProductionModule(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					wantCalls := 1
					if threshold > len(reference.Code) {
						wantCalls = 0
					}
					if calls != wantCalls || !bytes.Equal(image.Code, reference.Code) || len(image.Entry) != len(reference.Entry) || len(image.InternalEntry) != len(reference.Entry) {
						t.Fatal("shared staging code/pressure/entries", calls, err)
					}
					for i, entry := range reference.Entry {
						if image.Entry[i] != entry || image.Entry[i]&15 != 0 || image.InternalEntry[i] != entry {
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
						if base == 0 || !bytes.Equal(mapping[:len(image.Code)], reference.Code) {
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
			if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ImportBindings: hostImportBindings(test.imports)}); image == nil || err != nil || !bytes.Equal(image.Code, reference.Code) || image.CodeImage != nil {
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
		{SyncHostCalls: true}, {ImportBindings: []ImportBinding{{Dynamic: true}}},
		{GCTypeSubtypingRefTest: true}, {GCStructHelpers: true}, {GCArrayHelpers: true},
		{Profile: true}, {SourceMaps: true}, {UnwindMaps: true}, {Stats: &ModuleStats{}}, {CollectInlineReport: true},
		{Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: []gc.TypeDesc{{Kind: gc.KindStruct}}}}},
		{Codegen: codegen.Options{Module: codegen.ModuleInfo{Features: codegen.Features{GC: true}}}},
		{SyncHostSlots: -1}, {SyncHostSlots: 65536},
	} {
		if opts.Stats != nil && diagnosticsEnabled || opts.Profile && profileEnabled {
			continue // Finalized sizes are qualified independently of profiling.
		}
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
	for _, count := range []int{1, MaxFunctions, MaxFunctions + 1} {
		descs := make([]gc.TypeDesc, count)
		for i := range descs {
			descs[i] = gc.TypeDesc{ID: gc.TypeID(i), Kind: gc.KindFunc, Final: true}
		}
		pressure := 0
		image, err := compileSWARProductionModule(m, CompileOptions{DeferCodeMapping: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: descs}}, MemoryPressure: func() { pressure++ }})
		if count == 1 && (err != nil || image == nil || pressure != 1) || count > 1 && (image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0) {
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

func TestSWARProductionOwnedMemoryExportsRetainPrivatePolicy(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(5, []byte{1, 1, 1, 4}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("load", 0, 0), wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfd, 0, 0, 0, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if image, err := compileSWARDecodedModule(m, emissionMode{production: true, interruptible: true}, swarModulePlain); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("private memory export policy widened", err)
	}
	public, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
	if err != nil || public == nil || public.CodeImage != nil {
		t.Fatal("production memory export route", err)
	}
	// Removing metadata-only memory exports must leave every native byte intact.
	m.Exports = m.Exports[:1]
	reference, err := compileSWARDecodedModule(m, emissionMode{production: true, interruptible: true}, swarModulePlain)
	if err != nil || !bytes.Equal(reference.code, public.Code) {
		t.Fatal("owned memory export changed lowering", err)
	}
}

func TestSWARProductionOwnedGlobalExportsRetainPrivatePolicy(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, true, append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x0b)))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0), wasmtest.ExportEntry("g", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if image, err := compileSWARDecodedModule(m, emissionMode{production: true, interruptible: true}, swarModulePlain); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("private global export policy widened", err)
	}
	public, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
	if err != nil || public == nil || public.CodeImage != nil {
		t.Fatal("production global export route", err)
	}
	m.Exports = m.Exports[:1]
	reference, err := compileSWARDecodedModule(m, emissionMode{production: true, interruptible: true}, swarModulePlain)
	if err != nil || !bytes.Equal(reference.code, public.Code) {
		t.Fatal("owned global export changed lowering", err)
	}
}

func TestSWARProductionImportedMemoryCountsOnlyFunctionImports(t *testing.T) {
	entry := append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(2, wasmtest.Vec(entry)), wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("load", 0, 0), wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfd, 0, 0, 0, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if image, err := compileSWARDecodedModule(m, emissionMode{production: true, interruptible: true}, swarModuleHosts); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("private memory import policy widened", err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
	if err != nil || image == nil || image.CodeImage != nil || len(image.Entry) != 1 {
		t.Fatal("memory import entered function-index or host transport", err)
	}
	memory, ok := m.MemoryType(0)
	if !ok {
		t.Fatal("missing imported memory metadata")
	}
	m.Imports = nil
	m.Memories = []wasm.MemType{memory}
	reference, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
	if err != nil || !bytes.Equal(image.Code, reference.Code) {
		t.Fatal("memory import changed identical constraints lowering", err)
	}
}

func TestSWARProductionImportedGlobalsKeepSeparateIndexSpaces(t *testing.T) {
	vectorLiteral := append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128, wasm.I32, wasm.I64}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "v", wasm.V128, true), wasmtest.GlobalImportEntry("env", "i", wasm.I32, false))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I64, false, []byte{0x42, 7, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0), wasmtest.ExportEntry("local", 3, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x23, 1, 0x23, 2, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if image, err := compileSWARDecodedModule(m, emissionMode{production: true, interruptible: true}, swarModuleHosts); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("private global import policy widened", err)
	}
	image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
	if err != nil || image == nil || image.CodeImage != nil || len(image.Entry) != 1 {
		t.Fatal("global import entered function-index or host transport", err)
	}
	vector, _ := m.GlobalTypeByIndex(0)
	integer, _ := m.GlobalTypeByIndex(1)
	m.Imports = nil
	m.Globals = append([]wasm.Global{{Type: vector, Init: wasm.Expr{BodyBytes: vectorLiteral}}, {Type: integer, Init: wasm.Expr{BodyBytes: []byte{0x41, 0, 0x0b}}}}, m.Globals...)
	reference, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
	if err != nil || !bytes.Equal(image.Code, reference.Code) {
		t.Fatal("imported global prefix changed identical typed lowering", err)
	}
}

func TestSWARProductionUnusedVectorImportAndAggregateGlobalBound(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "unused", wasm.V128, false))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0x0b}))),
	)
	for _, deferred := range []bool{false, true} {
		if image, err := CompileProduction(source, ProductionOptions{DeferCodeMapping: deferred}); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("isolated scalar byte compiler admitted vector global import", err)
		}
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := moduleUsesSWAR(m)
	if err != nil || !selected {
		t.Fatal("unused vector import not selected", err)
	}
	im := m.Imports[0]
	m.Imports = make([]wasm.Import, 256)
	for i := range m.Imports {
		m.Imports[i] = im
	}
	if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true}); image == nil || err != nil {
		t.Fatal("aggregate global bound rejected its boundary", err)
	}
	m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32}, Init: wasm.Expr{BodyBytes: []byte{0x41, 0, 0x0b}}}}
	if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true}); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("aggregate imported/defined global bound bypassed", err)
	}
}

func TestSWARProductionCombinedStateImportsMatchOwnedMemory(t *testing.T) {
	globalImports := [][]byte{wasmtest.GlobalImportEntry("env", "v", wasm.V128, true), wasmtest.GlobalImportEntry("env", "i", wasm.I32, false)}
	memoryImport := append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4)
	for _, position := range []int{0, 1, 2} {
		imports := append([][]byte(nil), globalImports[:position]...)
		imports = append(imports, memoryImport)
		imports = append(imports, globalImports[position:]...)
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128, wasm.I32, wasm.I64, wasm.V128}))),
			wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 0}),
			wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I64, false, []byte{0x42, 7, 0x0b}))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0), wasmtest.ExportEntry("local", 3, 2), wasmtest.ExportEntry("memory", 2, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x23, 1, 0x23, 2, 0x20, 0, 0xfd, 0, 0, 0, 0x0b}))),
		)
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if image, err := compileSWARDecodedModule(m, emissionMode{production: true, interruptible: true}, swarModuleHosts); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("private combined import policy widened", err)
		}
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
		if err != nil || image == nil || image.CodeImage != nil || len(image.Entry) != 1 {
			t.Fatal("combined state imports changed defined function identity", position, err)
		}
		memory, ok := m.MemoryType(0)
		if !ok {
			t.Fatal("missing interleaved memory type")
		}
		m.Imports = append(append([]wasm.Import(nil), m.Imports[:position]...), m.Imports[position+1:]...)
		m.Memories = []wasm.MemType{memory}
		reference, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
		if err != nil || !bytes.Equal(image.Code, reference.Code) {
			t.Fatal("combined import changed identical global and memory constraints lowering", position, err)
		}
	}
}

func TestSWARProductionHostStateImportsUseActualImportEntries(t *testing.T) {
	global := wasmtest.GlobalImportEntry("env", "v", wasm.V128, true)
	memory := append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4)
	host := append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0)
	var expected []byte
	for _, imports := range [][][]byte{{host, global, memory}, {global, host, memory}, {global, memory, host}, {memory, host, global}, {host, memory, global}, {memory, global, host}} {
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.V128, wasm.I32, wasm.V128}))),
			wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 1}),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1), wasmtest.ExportEntry("global", 3, 0), wasmtest.ExportEntry("memory", 2, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x41, 7, 0x10, 0, 0x41, 3, 0xfd, 0, 0, 0, 0x0b}))),
		)
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if image, err := compileSWARDecodedModule(m, emissionMode{production: true, interruptible: true}, swarModuleHosts); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("private host/state import profile widened", err)
		}
		if image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, ImportBindings: hostImportBindings(1)}); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("host/state accepted without synchronous interrupt bridge", err)
		}
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ImportBindings: hostImportBindings(1)})
		if err != nil || image == nil || image.CodeImage != nil || len(image.Entry) != 1 {
			t.Fatal("interleaved host signature or defined identity", err)
		}
		if expected == nil {
			expected = append([]byte(nil), image.Code...)
		} else if !bytes.Equal(expected, image.Code) {
			t.Fatal("import order changed identical function/global/memory lowering")
		}
		resolvedMemory, ok := m.MemoryType(0)
		if !ok {
			t.Fatal("missing imported memory type")
		}
		resolvedGlobal, ok := m.GlobalTypeByIndex(0)
		if !ok {
			t.Fatal("missing imported global type")
		}
		for _, im := range m.Imports {
			if im.Type.Kind == wasm.ExternFunc {
				m.Imports = []wasm.Import{im}
				break
			}
		}
		m.Memories = []wasm.MemType{resolvedMemory}
		m.Globals = []wasm.Global{{Type: resolvedGlobal, Init: wasm.Expr{BodyBytes: append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x0b)}}}
		reference, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ImportBindings: hostImportBindings(1)})
		if err != nil || !bytes.Equal(image.Code, reference.Code) {
			t.Fatal("host with borrowed state differs from identical owned state constraints", err)
		}
	}
}

func TestSWARProductionCombinedImportBounds(t *testing.T) {
	imports := make([][]byte, 0, MaxProductionImports)
	for range 256 {
		imports = append(imports, wasmtest.GlobalImportEntry("env", "v", wasm.V128, false))
	}
	imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4))
	for range MaxHostFunctionImports {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0))
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.V128, wasm.I32, wasm.V128}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("last", 0, 256))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0xff, 1, 0x41, 7, 0x10, 0xff, 1, 0x41, 3, 0xfd, 0, 0, 0, 0x0b}))),
	)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal("combined raw source admission", err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, ImportBindings: hostImportBindings(MaxHostFunctionImports)}
	if image, err := CompileModuleWith(m, opts); err != nil || image == nil || image.CodeImage != nil || len(image.Entry) != 1 {
		t.Fatal("combined bounded imports rejected", err)
	}
	// Count rejection precedes validation and compilation, including private
	// decoded callers that do not pass through production binding admission.
	for _, kind := range []wasm.ExternKind{wasm.ExternFunc, wasm.ExternGlobal} {
		bad := *m
		bad.Imports = append([]wasm.Import(nil), m.Imports...)
		var extra wasm.Import
		for _, im := range m.Imports {
			if im.Type.Kind == kind {
				extra = im
				break
			}
		}
		// Remove the other kind to stay below the total-entry bound.
		for i, im := range bad.Imports {
			if im.Type.Kind != kind && im.Type.Kind != wasm.ExternMem {
				bad.Imports = append(bad.Imports[:i], bad.Imports[i+1:]...)
				break
			}
		}
		bad.Imports = append(bad.Imports, extra)
		if err := admitSWARDecodedBudget(&bad); !errors.Is(err, ErrUnsupported) {
			t.Fatal("per-kind import bound bypassed", kind, err)
		}
	}
	bad := *m
	bad.Imports = append(append([]wasm.Import(nil), m.Imports...), m.Imports[0])
	if err := admitSWARDecodedBudget(&bad); !errors.Is(err, ErrUnsupported) {
		t.Fatal("total import bound bypassed", err)
	}
}

func TestSWARProductionOwnedTableWithStateImports(t *testing.T) {
	global := wasmtest.GlobalImportEntry("env", "v", wasm.V128, true)
	memory := append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4)
	for _, entries := range [][][]byte{{global}, {global, memory}, {memory, global}} {
		sections := [][]byte{
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}), wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
			wasmtest.Section(2, wasmtest.Vec(entries...)), wasmtest.Section(3, []byte{2, 0, 1}),
			wasmtest.Section(4, []byte{1, 0x70, 1, 2, 4}),
		}
		if len(entries) == 1 {
			sections = append(sections, wasmtest.Section(5, []byte{1, 1, 1, 4}))
		}
		sections = append(sections,
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0), wasmtest.ExportEntry("global", 3, 0), wasmtest.ExportEntry("memory", 2, 0))),
			wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 1, 1})),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x11, 1, 0, 0x0b}), wasmtest.Code([]byte{0x23, 0, 0x0b}))))
		m, err := wasm.DecodeModule(wasmtest.Module(sections...))
		if err != nil {
			t.Fatal(err)
		}
		if image, err := compileSWARDecodedModule(m, emissionMode{production: true, interruptible: true}, swarModuleTables); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("private table pilot admitted state imports", err)
		}
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
		if err != nil || image == nil || image.CodeImage != nil || len(image.Entry) != 2 {
			t.Fatal("production state-only table policy", err)
		}
		resolvedMemory, _ := m.MemoryType(0)
		m.Imports = nil
		m.Memories = []wasm.MemType{resolvedMemory}
		m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.V128, Mutable: true}, Init: wasm.Expr{BodyBytes: append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x0b)}}}
		reference, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
		if err != nil || !bytes.Equal(image.Code, reference.Code) {
			t.Fatal("borrowed table state changed identical native lowering", err)
		}
		// Even direct decoded callers with the public state flags cannot combine
		// function imports with table policy and its defined-function index space.
		host := append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 2)
		hostModule, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}), wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(host)), wasmtest.Section(3, []byte{1, 1}), wasmtest.Section(4, []byte{1, 0x70, 0, 1}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x0b))))))
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(hostModule); err != nil {
			t.Fatal("scalar host/table negative is not valid Wasm", err)
		}
		if image, err := compileSWARDecodedModuleProfile(hostModule, emissionMode{production: true, interruptible: true}, swarModuleTables, swarStateProfile{exports: true, importedMemory: true, importedGlobals: true}); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("table policy admitted function import", err)
		}
	}
}
