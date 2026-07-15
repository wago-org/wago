package wago

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
)

func TestCompiledCodecV22VersionContract(t *testing.T) {
	blob, err := (&Compiled{}).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	if got := blob[4]; got != 22 {
		t.Fatalf("compiled codec version = %d, want 22", got)
	}

	v21 := append([]byte(nil), blob...)
	v21[4] = 21
	var got Compiled
	if err := got.UnmarshalBinary(v21); err == nil || !strings.Contains(err.Error(), "version 21 unsupported") {
		t.Fatalf("UnmarshalBinary v21 error = %v, want explicit incompatibility rejection", err)
	}
}

func TestCompiledCodecV21RoundTripsStructuralReferenceMetadata(t *testing.T) {
	input := structuralReferenceCodecFixture()
	blob, err := input.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary structural reference metadata: %v", err)
	}
	var got Compiled
	if err := got.UnmarshalBinary(blob); err != nil {
		t.Fatalf("UnmarshalBinary structural reference metadata: %v", err)
	}

	if len(got.importFuncSigs) != len(input.importFuncSigs) || len(got.Funcs) != len(input.Funcs) || !reflect.DeepEqual(got.Funcs[0].Params, input.Funcs[0].Params) || !reflect.DeepEqual(got.Funcs[0].Results, input.Funcs[0].Results) {
		t.Fatalf("reference signatures changed: imports=%#v funcs=%#v", got.importFuncSigs, got.Funcs)
	}
	params, results, err := got.SignatureDescriptor("refs")
	if err != nil || len(params) != 2 || len(results) != 2 || params[0].Ref.Heap.Abstract != AbstractHeapFunc || params[1].Ref.Heap.Abstract != AbstractHeapExtern {
		t.Fatalf("exact reference signatures = %v -> %v, %v", params, results, err)
	}
	if !reflect.DeepEqual(got.GlobalImports, input.GlobalImports) || !reflect.DeepEqual(got.Globals, input.Globals) || !reflect.DeepEqual(got.GlobalExports, input.GlobalExports) {
		t.Fatalf("reference globals changed: imports=%#v globals=%#v exports=%#v", got.GlobalImports, got.Globals, got.GlobalExports)
	}
	if got.TableType != input.TableType || got.tableImport != input.tableImport || got.tableImportMin != input.tableImportMin || got.tableImportMax != input.tableImportMax || got.tableImportHasMax != input.tableImportHasMax {
		t.Fatalf("table 0 changed: type=%s import=%q limits=%d/%d/%v", got.TableType, got.tableImport, got.tableImportMin, got.tableImportMax, got.tableImportHasMax)
	}
	if !reflect.DeepEqual(got.extraTables, input.extraTables) || !reflect.DeepEqual(got.tableExports, input.tableExports) || !got.hasTableExportMetadata {
		t.Fatalf("indexed table metadata changed: tables=%#v exports=%#v exact=%v", got.extraTables, got.tableExports, got.hasTableExportMetadata)
	}
	if !reflect.DeepEqual(got.Elems, input.Elems) || !reflect.DeepEqual(got.passiveElems, input.passiveElems) {
		t.Fatalf("typed element metadata changed: active=%#v state=%#v", got.Elems, got.passiveElems)
	}
}

func TestCompiledCodecV21ClearsReusedReceiverAndOmitsLiveState(t *testing.T) {
	reused := structuralReferenceCodecFixture()
	reused.dynamicImports = true
	reused.tableExports = map[string]int{"stale": 99}
	reused.extraTables = []tableDef{{ImportKey: "stale", Size: 99, Max: 99, Type: ValExternRef}}

	empty, err := (&Compiled{}).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary empty: %v", err)
	}
	if err := reused.UnmarshalBinary(empty); err != nil {
		t.Fatalf("UnmarshalBinary into reused receiver: %v", err)
	}
	if reused.dynamicImports || reused.tableExports != nil || reused.extraTables != nil || reused.HasTable || len(reused.Globals) != 0 || len(reused.passiveElems) != 0 {
		t.Fatalf("reused receiver retained stale state: %+v", reused)
	}
}

func TestCompiledCodecV21RejectsLiveAndMalformedReferenceMetadata(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*Compiled)
		want string
	}{
		{name: "externref token bits", mut: func(c *Compiled) { c.Globals[2].HasInitGlobal = false; c.Globals[2].Bits = 0x8877665544332211 }, want: "non-null externref"},
		{name: "funcref descriptor bits", mut: func(c *Compiled) { c.Globals[1].HasInitFunc = false; c.Globals[1].Bits = 0x1122334455667788 }, want: "non-structural funcref"},
		{name: "externref ref.func", mut: func(c *Compiled) {
			c.Globals[2].HasInitGlobal = false
			c.Globals[2].HasInitFunc = true
			c.Globals[2].InitFunc = 0
		}, want: "ref.func initializer has type externref"},
		{name: "non-null externref element", mut: func(c *Compiled) { c.Elems[1].Values[0] = RefInit{FuncIndex: 0} }, want: "non-null externref"},
		{name: "forged table export", mut: func(c *Compiled) { c.tableExports["bad"] = 99 }, want: "table export"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := structuralReferenceCodecFixture()
			tc.mut(c)
			if _, err := c.MarshalBinary(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("MarshalBinary error = %v, want %q", err, tc.want)
			}
		})
	}

	blob, err := structuralReferenceCodecFixture().MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	for _, cut := range []int{5, len(blob) / 2, len(blob) - 1} {
		var got Compiled
		if err := got.UnmarshalBinary(blob[:cut]); err == nil {
			t.Fatalf("UnmarshalBinary accepted truncated blob at %d/%d", cut, len(blob))
		}
	}
}

func TestCompiledCodecV21RequiredFeatureBitsAreExactAndFailClosed(t *testing.T) {
	fixture := structuralReferenceCodecFixture()
	loaded := roundTripCompiled(t, fixture)
	want := CoreFeatureMutableGlobal | CoreFeatureMultiValue | CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes
	if got := CoreFeatures(loaded.requiredFeatures); got != want {
		t.Fatalf("required features = %s, want %s", got, want)
	}
	if got := CoreFeatures(roundTripCompiled(t, &Compiled{}).requiredFeatures); got != 0 {
		t.Fatalf("scalar required features = %s, want none", got)
	}

	blob, err := structuralReferenceCodecFixture().MarshalBinary()
	if err != nil {
		t.Fatalf("marshal feature fixture: %v", err)
	}
	// The fixture has an empty memory-import string and GC descriptor list, so
	// the required-feature uint64 immediately precedes the zero GC count.
	binary.LittleEndian.PutUint64(blob[len(blob)-9:len(blob)-1], 0)
	var decoded Compiled
	if err := decoded.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unrecorded features") {
		t.Fatalf("missing feature bits error = %v, want fail-closed rejection", err)
	}

	blob, err = (&Compiled{}).MarshalBinary()
	if err != nil {
		t.Fatalf("marshal unknown-feature fixture: %v", err)
	}
	binary.LittleEndian.PutUint64(blob[len(blob)-9:len(blob)-1], uint64(CoreFeatureTailCall))
	if err := decoded.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
		t.Fatalf("unknown feature bits error = %v, want fail-closed rejection", err)
	}
}

func TestCompiledCodecV21CompileRecordsUsedFeatureFamilies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		module []byte
		want   CoreFeatures
	}{
		{name: "scalar", module: benchAddOneModule(), want: 0},
		{name: "sign extension", module: signExtModule(), want: CoreFeatureSignExtensionOps},
		{name: "multi value", module: multiValueControlCallModule(), want: CoreFeatureMultiValue},
		{name: "bulk memory", module: passiveDataModule(), want: CoreFeatureBulkMemoryOperations},
		{name: "reference types", module: nullableLocalFuncrefGlobalsModule(), want: CoreFeatureMutableGlobal | CoreFeatureReferenceTypes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileExplicitArtifact(t, tc.module)
			defer compiled.Close()
			if got := CoreFeatures(compiled.requiredFeatures); got != tc.want {
				t.Fatalf("compiled required features = %s, want %s", got, tc.want)
			}
			loaded := roundTripCompiled(t, compiled)
			if got := CoreFeatures(loaded.requiredFeatures); got != tc.want {
				t.Fatalf("loaded required features = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestCompiledCodecV21LoadedReferenceExecutionAndSnapshotBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		module []byte
		run    func(*testing.T, *Instance)
	}{
		{
			name:   "multiple funcref tables",
			module: benchTwoLocalTablesModuleWithExports(true),
			run: func(t *testing.T, in *Instance) {
				for _, export := range []string{"call0", "call1"} {
					got, err := in.Invoke(export)
					if err != nil || len(got) != 1 || AsI32(got[0]) != 7 {
						t.Fatalf("%s = %v, %v; want 7", export, got, err)
					}
				}
				for _, export := range []string{"table0", "table1"} {
					if _, err := in.ExportedTable(export); err != nil {
						t.Fatalf("ExportedTable(%s): %v", export, err)
					}
				}
			},
		},
		{
			name:   "externref table",
			module: benchExternrefTableModule(),
			run: func(t *testing.T, in *Instance) {
				got, err := in.Invoke("set_and_get", 0)
				if err != nil || len(got) != 1 || got[0] != 0 {
					t.Fatalf("set_and_get(null) = %v, %v; want null", got, err)
				}
			},
		},
		{
			name:   "reference globals",
			module: nullableLocalFuncrefGlobalsModule(),
			run: func(t *testing.T, in *Instance) {
				got, err := in.Invoke("set_and_get", 0)
				if err != nil || len(got) != 1 || got[0] != 0 {
					t.Fatalf("set_and_get(null) = %v, %v; want null", got, err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileExplicitArtifact(t, tc.module)
			defer compiled.Close()
			blob, err := compiled.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}
			loaded, err := Load(blob)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			defer loaded.Close()
			if _, err := Capture(loaded, SnapshotOptions{}); err == nil || (!strings.Contains(err.Error(), "tables") && !strings.Contains(err.Error(), "reference global metadata")) {
				t.Fatalf("Capture loaded reference module = %v, want live-state rejection", err)
			}
			in, err := Instantiate(loaded)
			if err != nil {
				t.Fatalf("Instantiate loaded: %v", err)
			}
			defer in.Close()
			tc.run(t, in)
		})
	}
}

func structuralReferenceCodecFixture() *Compiled {
	return &Compiled{
		Code:       []byte{0xc3},
		Entry:      []int{0},
		Funcs:      []FuncSig{{Params: []ValType{ValFuncRef, ValExternRef}, Results: []ValType{ValExternRef, ValFuncRef}}},
		FuncTypeID: []uint32{7},
		Exports:    map[string]int{"refs": 0},

		GlobalImports: []GlobalImportDef{{Module: "env", Name: "external", Type: ValExternRef}},
		Globals: []GlobalDef{
			{Type: ValExternRef},
			{Type: ValFuncRef, Mutable: true, HasInitFunc: true, InitFunc: 0},
			{Type: ValExternRef, HasInitGlobal: true, InitGlobal: 0},
		},
		GlobalExports: map[string]int{"fun": 1, "external-copy": 2},

		HasTable:          true,
		TableType:         ValFuncRef,
		tableImport:       "env.functions",
		tableImportMin:    1,
		tableImportMax:    2,
		tableImportHasMax: true,
		extraTables: []tableDef{
			{ImportKey: "env.externals", Size: 0, Max: 3, Type: ValExternRef, ImportHasMax: true},
			{Size: 1, Max: 2, Type: ValFuncRef, HasInitFunc: true, InitFunc: 0},
			{Size: 0, Max: 4, Type: ValExternRef},
		},
		tableExports:           map[string]int{"imported-functions": 0, "imported-externals": 1, "local-functions": 2, "local-externals": 3},
		hasTableExportMetadata: true,
		NeedsFuncRefDescs:      true,
		Elems: []ElemInit{
			{TableIndex: 2, RefType: ValFuncRef, Mode: ElemModeActive, Offset: OffsetInit{Base: 0}, Values: []RefInit{{Null: true}, {FuncIndex: 0}}},
			{TableIndex: 3, RefType: ValExternRef, Mode: ElemModeActive, Offset: OffsetInit{Base: 0}, Values: []RefInit{{Null: true}}},
		},
		passiveElems: []ElemInit{
			{RefType: ValFuncRef, Mode: ElemModePassive, Values: []RefInit{{FuncIndex: 0}, {Null: true}}},
			{RefType: ValExternRef, Mode: ElemModePassive, Values: []RefInit{{Null: true}}},
			{RefType: ValFuncRef, Mode: ElemModeDeclarative},
		},
	}
}
