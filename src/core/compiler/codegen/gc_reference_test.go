package codegen

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestIsCollectorReferenceType(t *testing.T) {
	for _, test := range []struct {
		name string
		typ  wasm.ValType
		want bool
	}{
		{"i64", wasm.I64, false},
		{"funcref", wasm.FuncRef, false},
		{"externref", wasm.ExternRef, false},
		{"anyref", wasm.AnyRef, true},
		{"eqref", wasm.EqRef, true},
		{"i31ref", wasm.I31Ref, true},
		{"structref", wasm.RefVal(wasm.AbsRef(wasm.HeapStruct)), true},
		{"arrayref", wasm.RefVal(wasm.AbsRef(wasm.HeapArray)), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := IsCollectorReferenceType(nil, test.typ); got != test.want {
				t.Fatalf("IsCollectorReferenceType(%s) = %v, want %v", test.typ, got, test.want)
			}
		})
	}
}
