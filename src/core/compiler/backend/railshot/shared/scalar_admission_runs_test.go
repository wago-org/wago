package shared

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"reflect"
	"testing"
)

func TestScalarAdmissionRunDeclarations(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64, wasm.F32} {
		for _, count := range []uint32{0, 1, 255, 256, 257} {
			ft := &wasm.CompType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
			c := &wasm.Func{Locals: wasm.Locals{Runs: []wasm.LocalRun{{Count: count, Type: typ}}}, BodyBytes: []byte{0x20, 0, 0x0b}}
			types := []wasm.ValType{wasm.I32}
			for range count {
				types = append(types, typ)
			}
			got, want := AdmitScalarFunction(c, ft), AdmitScalar(c.BodyBytes, ft, types)
			if got.Eligible != want.Eligible || got.Eligible && !reflect.DeepEqual(got, want) {
				t.Fatalf("count=%d type=%v got=%+v want=%+v", count, typ, got, want)
			}
		}
	}
	// Mixed widths share one zero each, regardless of run ordering and repetition.
	ft := &wasm.CompType{Results: []wasm.ValType{wasm.I32}}
	c := &wasm.Func{Locals: wasm.Locals{Runs: []wasm.LocalRun{{Count: 2, Type: wasm.I64}, {Count: 3, Type: wasm.I32}, {Count: 1, Type: wasm.I64}}}, BodyBytes: []byte{0x20, 2, 0x0b}}
	got := AdmitScalarFunction(c, ft)
	want := AdmitScalar(c.BodyBytes, ft, []wasm.ValType{wasm.I64, wasm.I64, wasm.I32, wasm.I32, wasm.I32, wasm.I64})
	if !got.Eligible || got != want {
		t.Fatalf("mixed got=%+v want=%+v", got, want)
	}
}
