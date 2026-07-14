package ir

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestMetadataAndReferenceHelpers(t *testing.T) {
	if !(Range{Start: 3}).Empty() || (Range{Len: 1}).Empty() {
		t.Fatal("Range.Empty did not distinguish empty and non-empty ranges")
	}

	if got := elemLen(wasm.Elem{Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0, 1}}}); got != 2 {
		t.Fatalf("function element length = %d, want 2", got)
	}
	if got := elemLen(wasm.Elem{Kind: wasm.ElemKind{Kind: wasm.ElemFuncExprs, Exprs: []wasm.Expr{{}, {}}}}); got != 2 {
		t.Fatalf("expression element length = %d, want 2", got)
	}
	if got := elemLen(wasm.Elem{Kind: wasm.ElemKind{Kind: wasm.ElemTypedExprs, Exprs: []wasm.Expr{{}}}}); got != 1 {
		t.Fatalf("typed expression element length = %d, want 1", got)
	}

	funcDef := &wasm.DefType{Rec: wasm.RecType{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc}}}}}
	structDef := &wasm.DefType{Rec: wasm.RecType{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompStruct}}}}}
	wasmModule := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{
		{Comp: wasm.CompType{Kind: wasm.CompFunc}},
		{Comp: wasm.CompType{Kind: wasm.CompStruct}},
	}}}}
	irModule := &Module{Types: []wasm.FuncType{{}, {}}, TypeIsFunc: []bool{true, false}}

	for _, tc := range []struct {
		name          string
		ref           wasm.RefType
		build, verify bool
	}{
		{"funcref", wasm.AbsRef(wasm.HeapFunc), true, true},
		{"nofunc", wasm.AbsRef(wasm.HeapNoFunc), true, true},
		{"externref", wasm.AbsRef(wasm.HeapExtern), false, false},
		{"indexed_function", wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false), true, true},
		{"indexed_nonfunction", wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false), false, false},
		{"indexed_recursive", wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0, Rec: true}), false), true, false},
		{"indexed_out_of_range", wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 9}), false), false, false},
		{"defined_function", wasm.Ref(true, wasm.HeapType{Kind: wasm.HeapDefType, Def: funcDef}, false), true, true},
		{"defined_nonfunction", wasm.Ref(true, wasm.HeapType{Kind: wasm.HeapDefType, Def: structDef}, false), false, false},
		{"defined_invalid", wasm.Ref(true, wasm.HeapType{Kind: wasm.HeapDefType, Def: &wasm.DefType{Index: 1}}, false), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFuncRefTableType(wasmModule, tc.ref); got != tc.build {
				t.Fatalf("build helper = %v, want %v", got, tc.build)
			}
			if got := irIsFuncRefTableType(irModule, tc.ref); got != tc.verify {
				t.Fatalf("verify helper = %v, want %v", got, tc.verify)
			}
		})
	}
	indexed := wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false)
	if !isFuncRefTableType(nil, indexed) || irIsFuncRefTableType(nil, indexed) {
		t.Fatal("nil module indexed-reference behavior changed")
	}

	canonical := &Module{Types: []wasm.FuncType{
		{Params: []wasm.ValType{wasm.I32}},
		{Params: []wasm.ValType{wasm.I64}},
		{Params: []wasm.ValType{wasm.I32}},
	}, TypeIsFunc: []bool{true, false, true}}
	if got := irCanonicalTypeID(canonical, 2); got != 0 {
		t.Fatalf("fallback canonical type ID = %d, want 0", got)
	}
	canonical.CanonicalTypeIDs = []uint32{7, 8, 9}
	if got := irCanonicalTypeID(canonical, 2); got != 9 {
		t.Fatalf("explicit canonical type ID = %d, want 9", got)
	}
}

func TestIRDiagnosticFormattingHelpers(t *testing.T) {
	if formatFuncSizeHint(nil) != 0 || formatFuncSizeHint(&Func{Blocks: make([]Block, 2), Insts: make([]Inst, 3), ValueIDs: make([]ValueID, 4), Edges: make([]Edge, 5)}) <= 64 {
		t.Fatal("format size hint changed")
	}
	for _, tc := range []struct {
		t    wasm.ValType
		aux  uint64
		want string
	}{{wasm.I32, uint64(^uint32(0)), "i32 -1"}, {wasm.I64, uint64(^uint64(0)), "i64 -1"}, {wasm.F32, 1, "f32 0x00000001"}, {wasm.F64, 1, "f64 0x0000000000000001"}, {wasm.ValType{}, 0xab, "0xab"}} {
		if got := constString(tc.t, tc.aux); got != tc.want {
			t.Fatalf("constString(%s, %#x) = %q", tc.t, tc.aux, got)
		}
	}
	if auxName(OpIBinary, uint8(IBinAdd)) != "add" || auxName(OpIBinary, 99) != "kind99" || memName(MemI32) != "i32" || memName(MemOp(99)) != "mem99" {
		t.Fatal("IR diagnostic names changed")
	}
}

func TestIRTypeAndMemoryHelperCoverage(t *testing.T) {
	if got, ok := memLoadResult(MemI64Load32U); !ok || got != wasm.I64 {
		t.Fatalf("i64 load result = %s, %v", got, ok)
	}
	if got, ok := memLoadResult(MemI32Store8); ok || got != (wasm.ValType{}) {
		t.Fatalf("store reported a load result = %s, %v", got, ok)
	}
	if got, ok := memStoreValue(MemI64Store32); !ok || got != wasm.I64 {
		t.Fatalf("i64 store value = %s, %v", got, ok)
	}
	if got, ok := memStoreValue(MemI32Load8S); ok || got != (wasm.ValType{}) {
		t.Fatalf("load reported a store value = %s, %v", got, ok)
	}
	if _, ok := memLoadResult(MemOp(99)); ok {
		t.Fatal("unknown memory operation reported a load result")
	}
	if _, ok := memStoreValue(MemOp(99)); ok {
		t.Fatal("unknown memory operation reported a store value")
	}
	if got := funcTypeFromComp(nil); len(got.Params) != 0 || len(got.Results) != 0 {
		t.Fatalf("nil function type = %#v", got)
	}
	comp := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I64}}
	if got := funcTypeFromComp(comp); len(got.Params) != 1 || len(got.Results) != 1 {
		t.Fatalf("function type = %#v", got)
	}
	if elemType(wasm.Elem{Kind: wasm.ElemKind{Kind: wasm.ElemFuncs}}) != wasm.FuncRef ||
		elemType(wasm.Elem{Kind: wasm.ElemKind{Kind: wasm.ElemTypedExprs, Ref: wasm.AbsRef(wasm.HeapExtern)}}) != wasm.ExternRef {
		t.Fatal("element types changed")
	}
	for op := byte(0x28); op <= 0x3e; op++ {
		kind, store := memOpcodeKind(op)
		memInfo(op)
		if kind == MemOp(0) && store {
			t.Fatalf("memory opcode %#x has invalid metadata", op)
		}
	}
	if kind, store := memOpcodeKind(0xff); kind != MemI64Store32 || !store || naturalMemAlign(MemOp(99)) != 0 {
		t.Fatal("fallback memory opcode handling changed")
	}
	f := &Func{Values: []Value{{Type: wasm.I32}}, ValueIDs: []ValueID{0}}
	if typeOf(f, 0) != wasm.I32 || typeOf(f, InvalidValue) != (wasm.ValType{}) || typeOf(f, 2) != (wasm.ValType{}) {
		t.Fatal("value type lookup changed")
	}
	if auxTypeFromResult(f, &Inst{Results: Range{Start: 0, Len: 1}}) != wasm.I32 || auxTypeFromResult(f, &Inst{}) != (wasm.ValType{}) {
		t.Fatal("result type lookup changed")
	}
	if valTypeCode(wasm.I64) != 0x7e || valTypeCode(wasm.ValType{}) != 0 || packValType(wasm.I32) != 0x7f {
		t.Fatal("value type packing changed")
	}
	if got := packKindType(9, wasm.F32); auxKind(got) != 9 || auxType(got) != wasm.F32 || auxValType(packValType(wasm.F64)) != wasm.F64 {
		t.Fatalf("packed kind/type = %#x", got)
	}
}
