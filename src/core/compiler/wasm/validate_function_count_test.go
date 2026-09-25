package wasm

import "testing"

func TestValidateFunctionCodeCount(t *testing.T) {
	for _, tc := range []struct {
		name      string
		functions int
		bodies    int
	}{
		{name: "missing body", functions: 1},
		{name: "excess body", bodies: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Module{Types: []RecType{ft(nil, nil)}, FuncTypes: make([]TypeIdx, tc.functions), Code: make([]Func, tc.bodies)}
			expectValidateErr(t, m, ErrUnknownFunc)
		})
	}
}

func BenchmarkValidateFunctionCodeCount(b *testing.B) {
	m := modWithFunc(nil, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ValidateModule(m); err != nil {
			b.Fatal(err)
		}
	}
}
