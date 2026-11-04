package wasm

import "testing"

func TestColdFlatStructuralKeyDoesNotAllocate(t *testing.T) {
	ft := CompType{Kind: CompFunc, Params: []ValType{I32, I64, F32, F64, I32, I64, F32, F64}, Results: []ValType{I32}}
	m := &Module{Types: []RecType{{SubTypes: []SubType{{Final: true, Comp: ft}}}}}
	if _, ok := m.TypeFunc(0); !ok {
		t.Fatal("missing function type")
	}
	want := StructuralFuncTypeKey(&ft)
	allocs := testing.AllocsPerRun(20, func() {
		m.structuralTypeCache = nil
		got, ok := m.StructuralTypeKeyChecked(0)
		if !ok || got != want {
			panic("flat identity changed")
		}
	})
	if allocs != 0 {
		t.Fatalf("one-use flat signature allocated %g times; want no cache allocation", allocs)
	}
}
