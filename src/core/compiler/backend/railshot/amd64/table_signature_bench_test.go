//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func tableSignatureBenchModule() *wasm.Module {
	params := make([]wasm.ValType, 32)
	for i := range params {
		params[i] = wasm.I32
	}
	return &wasm.Module{
		Types: []wasm.RecType{
			{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: params}}}},
			{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: append([]wasm.ValType(nil), params...)}}}},
		},
		FuncTypes: []wasm.TypeIdx{{Index: 0}, {Index: 1}}, Code: make([]wasm.Func, 2),
		Tables:   []wasm.Table{{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc)}}},
		Elements: []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemActive}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: make([]wasm.FuncIdx, 4096)}}},
	}
}

var tableSignatureBenchKey uint64

func BenchmarkImmutableTableRepeatedSignature(b *testing.B) {
	m := tableSignatureBenchModule()
	p := currentCodegenPolicy()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var ok bool
		tableSignatureBenchKey, ok = immutableLocalTableTypeWithPolicy(m, 0, p)
		if !ok {
			b.Fatal("not typed")
		}
	}
}
func TestImmutableTableEquivalentTypeIndexes(t *testing.T) {
	m := tableSignatureBenchModule()
	m.Elements[0].Kind.Funcs = []wasm.FuncIdx{0, 1, 0}
	key, ok := immutableLocalTableType(m, 0)
	want, _ := m.StructuralTypeKeyChecked(0)
	if !ok || key != want {
		t.Fatal("equivalent distinct types were rejected")
	}
	m.Types[1].SubTypes[0].Comp.Params[0] = wasm.I64
	_, ok = immutableLocalTableType(m, 0)
	if ok {
		t.Fatal("accepted different signature")
	}
}
