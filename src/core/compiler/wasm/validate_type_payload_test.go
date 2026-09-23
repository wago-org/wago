package wasm

import "testing"

func TestValidateRejectsInvalidValueTypePayloads(t *testing.T) {
	for _, tc := range []struct {
		name      string
		typeValue ValType
	}{
		{name: "zero numeric", typeValue: ValType{}},
		{name: "unknown numeric", typeValue: newValType(ValNum, NumType(0xff))},
		{name: "vector with numeric payload", typeValue: newValType(ValVec, NumI32)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Module{Types: []RecType{ft([]ValType{tc.typeValue}, nil)}}
			expectValidateErr(t, m, ErrUnknownType)
		})
	}
}

func TestValidateRejectsInvalidAbstractHeapType(t *testing.T) {
	m := &Module{Tables: []Table{{Type: TableType{Ref: AbsRef(AbsHeapType(0xff))}}}}
	expectValidateErr(t, m, ErrUnknownType)
}

func BenchmarkValidateTypePayloads(b *testing.B) {
	m := &Module{Types: []RecType{ft([]ValType{I32, V128, FuncRef}, nil)}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ValidateModule(m); err != nil {
			b.Fatal(err)
		}
	}
}
