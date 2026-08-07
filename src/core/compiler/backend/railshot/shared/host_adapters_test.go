package shared

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestHostAdapterSetIncludesEveryAddressableClass(t *testing.T) {
	start := wasm.FuncIdx(1)
	m := &wasm.Module{
		FuncTypes: make([]wasm.TypeIdx, 6),
		Code:      make([]wasm.Func, 6),
		Exports:   []wasm.Export{{Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}}},
		Start:     &start,
		Tables:    []wasm.Table{{Init: &wasm.Expr{BodyBytes: []byte{0xd2, 0x02, 0x0b}}}},
		Globals:   []wasm.Global{{Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrRefFunc, Index: 3}}}}},
		Elements: []wasm.Elem{
			{Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{4}}},
			{Kind: wasm.ElemKind{Kind: wasm.ElemFuncExprs, Exprs: []wasm.Expr{{BodyBytes: []byte{0xd2, 0x05, 0x0b}}}}},
		},
	}
	got, err := HostAdapterSet(m)
	if err != nil {
		t.Fatal(err)
	}
	if want := []bool{true, true, true, true, true, true}; !reflect.DeepEqual(got, want) {
		t.Fatalf("adapter set = %v, want %v", got, want)
	}
}

func TestHostAdapterSetLeavesDirectOnlyFunctionsClear(t *testing.T) {
	m := &wasm.Module{FuncTypes: make([]wasm.TypeIdx, 3), Code: make([]wasm.Func, 3), Exports: []wasm.Export{{Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}}}}
	got, err := HostAdapterSet(m)
	if err != nil {
		t.Fatal(err)
	}
	if want := []bool{true, false, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("adapter set = %v, want %v", got, want)
	}
}
