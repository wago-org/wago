package wasm

import "testing"

func TestValidateIndexedFunctionReferenceEquivalence(t *testing.T) {
	t.Run("duplicate function types", func(t *testing.T) {
		m := &Module{
			Types: []RecType{
				ft([]ValType{F32, F32}, []ValType{F32}),
				ft([]ValType{F32, F32}, []ValType{F32}),
				ft([]ValType{RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 0}), false))}, nil),
				ft([]ValType{RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 1}), false))}, nil),
			},
			FuncTypes: []TypeIdx{{Index: 2}, {Index: 3}},
			Code: []Func{
				{Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet}, {Kind: InstrCall, Index: 1}}}},
				{Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet}, {Kind: InstrCall, Index: 0}}}},
			},
		}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule duplicate types: %v", err)
		}
	})

	t.Run("separate recursive groups", func(t *testing.T) {
		self := func() RecType {
			return RecType{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: []ValType{
				I32, RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 0, Rec: true}), false)),
			}}}}}
		}
		m := &Module{
			Types: []RecType{
				self(),
				self(),
				ft([]ValType{RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 0}), false))}, nil),
				ft([]ValType{RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 1}), false))}, nil),
			},
			FuncTypes: []TypeIdx{{Index: 2}, {Index: 3}},
			Code: []Func{
				{Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet}, {Kind: InstrCall, Index: 1}}}},
				{Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet}, {Kind: InstrCall, Index: 0}}}},
			},
		}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule recursive types: %v", err)
		}
	})

	t.Run("different types remain distinct", func(t *testing.T) {
		m := &Module{
			Types: []RecType{
				ft([]ValType{I32}, nil),
				ft([]ValType{F32}, nil),
				ft([]ValType{RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 0}), false))}, nil),
				ft([]ValType{RefVal(Ref(false, IndexedHeap(TypeIdx{Index: 1}), false))}, nil),
			},
			FuncTypes: []TypeIdx{{Index: 2}, {Index: 3}},
			Code: []Func{
				{Body: Expr{Instrs: []Instruction{{Kind: InstrLocalGet}, {Kind: InstrCall, Index: 1}}}},
				{Body: Expr{}},
			},
		}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}
