package frontend

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

func TestLegacyDeclarativeDescriptorDemand(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*wasm.Module)
		want bool
	}{
		{name: "declaration only"},
		{name: "empty declaration", edit: func(m *wasm.Module) { m.Elements[0].Kind.Funcs = nil }},
		{name: "active payload", edit: func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemActive }, want: true},
		{name: "passive payload", edit: func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemPassive }, want: true},
		{name: "later passive payload", edit: func(m *wasm.Module) {
			e := m.Elements[0]
			e.Mode.Kind = wasm.ElemPassive
			m.Elements = append(m.Elements, e)
		}, want: true},
		{name: "earlier passive payload", edit: func(m *wasm.Module) {
			e := m.Elements[0]
			e.Mode.Kind = wasm.ElemPassive
			m.Elements = append([]wasm.Elem{e}, m.Elements...)
		}, want: true},
		{name: "body bytes", edit: func(m *wasm.Module) {
			m.Code = []wasm.Func{{BodyBytes: []byte{0xd2, 0x00, 0x1a, 0x0b}}}
		}, want: true},
		{name: "body instructions", edit: func(m *wasm.Module) {
			m.Code = []wasm.Func{{Body: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrRefFunc}}}}}
		}, want: true},
		{name: "global bytes", edit: func(m *wasm.Module) {
			m.Globals = []wasm.Global{{Init: wasm.Expr{BodyBytes: []byte{0xd2, 0x00, 0x0b}}}}
		}, want: true},
		{name: "global instructions", edit: func(m *wasm.Module) {
			m.Globals = []wasm.Global{{Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrRefFunc}}}}}
		}, want: true},
		{name: "expression declaration only", edit: func(m *wasm.Module) {
			m.Elements[0].Kind = wasm.ElemKind{Kind: wasm.ElemFuncExprs, Exprs: []wasm.Expr{{BodyBytes: []byte{0xd2, 0x00, 0x0b}}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := legacyDeclarativeDescriptorFixture(1)
			if tc.edit != nil {
				tc.edit(m)
			}
			facts, err := AnalyzeModuleFacts(m)
			if err != nil {
				t.Fatal(err)
			}
			if facts.UsesRefFunc != tc.want {
				t.Fatalf("materialized ref.func fact = %v, want %v", facts.UsesRefFunc, tc.want)
			}
			if got := RequiresFuncRefDescriptorsFromFacts(m, facts); got != tc.want {
				t.Fatalf("descriptor demand = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLegacyDeclarativePreservesTableDescriptorDemand(t *testing.T) {
	for _, imported := range []bool{false, true} {
		for _, ref := range []wasm.RefType{wasm.FuncRef.Ref(), wasm.ExternRef.Ref()} {
			m := legacyDeclarativeDescriptorFixture(1)
			tt := wasm.TableType{Ref: ref}
			if imported {
				m.Imports = []wasm.Import{{Type: wasm.NewTableExternType(tt)}}
			} else {
				m.Tables = []wasm.Table{{Type: tt}}
			}
			want := ref == wasm.FuncRef.Ref()
			if got := RequiresFuncRefDescriptors(m); got != want {
				t.Fatalf("imported=%v ref=%v descriptors=%v, want %v", imported, ref, got, want)
			}
		}
	}
}

func TestLegacyDeclarativeDescriptorQueryDoesNotAllocate(t *testing.T) {
	m := legacyDeclarativeDescriptorFixture(1024)
	facts, err := AnalyzeModuleFacts(m)
	if err != nil {
		t.Fatal(err)
	}
	if got := testing.AllocsPerRun(100, func() {
		if RequiresFuncRefDescriptorsFromFacts(m, facts) {
			t.Fatal("declaration-only module requires descriptors")
		}
	}); got != 0 {
		t.Fatalf("descriptor query allocations = %v, want 0", got)
	}
}

func BenchmarkLegacyDeclarativeDescriptorQuery(b *testing.B) {
	m := legacyDeclarativeDescriptorFixture(1024)
	facts, err := AnalyzeModuleFacts(m)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if RequiresFuncRefDescriptorsFromFacts(m, facts) {
			b.Fatal("declaration-only module requires descriptors")
		}
	}
	b.ReportMetric(float64((m.FuncCount()+1)*runtime.FuncRefDescBytes), "descriptor-bytes-avoided")
}

func legacyDeclarativeDescriptorFixture(functions int) *wasm.Module {
	return &wasm.Module{
		FuncTypes: make([]wasm.TypeIdx, functions),
		Elements: []wasm.Elem{{
			Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative},
			Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}},
		}},
	}
}
