//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
	"unsafe"
)

func linearTableBenchModule(n int) *wasm.Module {
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}}}, FuncTypes: []wasm.TypeIdx{{Index: 0}}, Code: []wasm.Func{{BodyBytes: []byte{0x0b}}}, Tables: make([]wasm.Table, n), Elements: make([]wasm.Elem, n), Exports: make([]wasm.Export, n)}
	for i := range n {
		m.Tables[i] = wasm.Table{Type: wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc)}}
		m.Elements[i] = wasm.Elem{Mode: wasm.ElemMode{Kind: wasm.ElemActive, Table: wasm.TableIdx(i)}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}}}
		m.Exports[i] = wasm.Export{Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}}
	}
	return m
}

var linearTableBenchSink []immutableTableHint

func BenchmarkImmutableTableAnalysisScaling(b *testing.B) {
	m := linearTableBenchModule(1024)
	policy := currentCodegenPolicy()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		linearTableBenchSink = computeImmutableTableHints(m, nil, policy)
	}
}
func TestImmutableTableAnalysisProofs(t *testing.T) {
	if unsafe.Sizeof(immutableTableHint{}) != 32 {
		t.Fatal("table hint storage grew")
	}
	for _, mode := range []string{"plain", "export", "initializer", "expressions", "foreign", "different"} {
		m := linearTableBenchModule(2)
		switch mode {
		case "export":
			m.Exports[0].Index = wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}
		case "initializer":
			init := wasm.Expr{BodyBytes: []byte{0xd0, 0x70, 0x0b}}
			m.Tables[0].Init = &init
		case "expressions":
			m.Elements[0].Kind = wasm.ElemKind{Kind: wasm.ElemFuncExprs, Exprs: []wasm.Expr{{BodyBytes: []byte{0xd2, 0, 0x0b}}}}
		case "foreign":
			m.Imports = []wasm.Import{{Type: wasm.NewFuncExternType(wasm.TypeIdx{Index: 0})}}
		case "different":
			m.Code = append(m.Code, wasm.Func{BodyBytes: []byte{0x0b}})
			m.FuncTypes = append(m.FuncTypes, wasm.TypeIdx{Index: 0})
			m.Elements[0].Kind.Funcs = []wasm.FuncIdx{0, 1}
		}
		got := computeImmutableTableHints(m, nil, currentCodegenPolicy())
		for i, h := range got {
			local := !moduleExportsTable(m, uint32(i)) && immutableLocalTableEntries(m, uint32(i))
			if !local {
				if h != (immutableTableHint{}) {
					t.Fatalf("%s table %d: %#v", mode, i, h)
				}
				continue
			}
			key, typed := immutableLocalTableType(m, uint32(i))
			target := immutableLocalTableTarget(m, uint32(i))
			if !h.local || h.typeKey != key || h.typed != typed || h.monomorphicTarget != target {
				t.Fatalf("%s table %d: %#v", mode, i, h)
			}
		}
	}
}
