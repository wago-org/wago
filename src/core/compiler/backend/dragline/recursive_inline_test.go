package dragline

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestBoundedRecursiveInlineExpandsOneGeneralLevel(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x41, 0x02, 0x48, 0x04, 0x7e,
			0x20, 0x00, 0xac,
			0x05,
			0x20, 0x00, 0x41, 0x01, 0x6b, 0x10, 0x00,
			0x20, 0x00, 0x41, 0x02, 0x6b, 0x10, 0x00, 0x7c,
			0x0b, 0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	inlined := boundedRecursiveInlineModule(m, 0, stack)
	if inlined == nil {
		t.Fatal("general bounded recursive candidate was rejected")
	}
	got, err := railssa.BuildStackFunc(inlined, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, instruction := range got.Instrs {
		if instruction.Kind == wasm.InstrCall {
			calls++
		}
	}
	if calls != 4 {
		t.Fatalf("remaining recursive calls = %d, want 4 after one bounded expansion", calls)
	}
	if len(got.Locals) != 2 || len(got.Instrs) <= len(stack.Instrs) {
		t.Fatalf("inlined shape locals=%d instructions=%d, original=%d", len(got.Locals), len(got.Instrs), len(stack.Instrs))
	}
}

func TestBoundedRecursiveInlineRejectsInvalidShape(t *testing.T) {
	if boundedRecursiveInlineModule(&wasm.Module{}, 0, &railssa.StackFunc{}) != nil {
		t.Fatal("invalid recursive shape was admitted")
	}
}
