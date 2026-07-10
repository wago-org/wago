package wago

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestCompiledCodecV20VersionContract(t *testing.T) {
	blob, err := (&Compiled{}).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	if got := blob[4]; got != 20 {
		t.Fatalf("compiled codec version = %d, want 20", got)
	}

	v19 := append([]byte(nil), blob...)
	v19[4] = 19
	var got Compiled
	if err := got.UnmarshalBinary(v19); err == nil || !strings.Contains(err.Error(), "version 19 unsupported") {
		t.Fatalf("UnmarshalBinary v19 error = %v, want explicit incompatibility rejection", err)
	}
}

func TestCompiledCodecV20RoundTripsStructuralReferenceMetadata(t *testing.T) {
	input := structuralReferenceCodecFixture()
	blob, err := input.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary structural reference metadata: %v", err)
	}
	var got Compiled
	if err := got.UnmarshalBinary(blob); err != nil {
		t.Fatalf("UnmarshalBinary structural reference metadata: %v", err)
	}

	if !reflect.DeepEqual(got.importFuncSigs, input.importFuncSigs) || !reflect.DeepEqual(got.Funcs, input.Funcs) {
		t.Fatalf("reference signatures changed: imports=%#v funcs=%#v", got.importFuncSigs, got.Funcs)
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

func TestCompiledCodecV20ClearsReusedReceiverAndOmitsLiveState(t *testing.T) {
	input := structuralReferenceCodecFixture()
	input.wasmBytes = []byte("UNSERIALIZED-RUNTIME-IDENTITY")
	blob, err := input.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	if bytes.Contains(blob, input.wasmBytes) {
		t.Fatal("compiled blob contains non-structural runtime/link identity bytes")
	}

	reused := structuralReferenceCodecFixture()
	reused.wasmBytes = []byte("stale")
	reused.needsLink = true
	reused.syncHostImports = true
	reused.tableExports = map[string]int{"stale": 99}
	reused.extraTables = []tableDef{{ImportKey: "stale", Size: 99, Max: 99, Type: ValExternRef}}

	empty, err := (&Compiled{}).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary empty: %v", err)
	}
	if err := reused.UnmarshalBinary(empty); err != nil {
		t.Fatalf("UnmarshalBinary into reused receiver: %v", err)
	}
	if reused.wasmBytes != nil || reused.needsLink || reused.syncHostImports || reused.tableExports != nil || reused.extraTables != nil || reused.HasTable || len(reused.Globals) != 0 || len(reused.passiveElems) != 0 {
		t.Fatalf("reused receiver retained stale state: %+v", reused)
	}
}

func TestCompiledCodecV20RejectsLiveAndMalformedReferenceMetadata(t *testing.T) {
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
