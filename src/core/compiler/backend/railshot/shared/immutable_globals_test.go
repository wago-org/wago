package shared

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestImmutableIntGlobals(t *testing.T) {
	m := &wasm.Module{
		Imports: []wasm.Import{{Type: wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.I64})}},
		Globals: []wasm.Global{
			{Type: wasm.GlobalType{Type: wasm.I32}, Init: wasm.Expr{BodyBytes: []byte{0x41, 0x7f, 0x0b}}},
			{Type: wasm.GlobalType{Type: wasm.I64}, Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI64Const, I64: 42}}}},
			{Type: wasm.GlobalType{Type: wasm.I32, Mutable: true}, Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const, I32: 7}}}},
			{Type: wasm.GlobalType{Type: wasm.I32}, Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const}, {Kind: wasm.InstrI32Const}}}},
			{Type: wasm.GlobalType{Type: wasm.F32}, Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrF32Const}}}},
			{Type: wasm.GlobalType{Type: wasm.FuncRef}, Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrRefNull}}}},
			{Type: wasm.GlobalType{Type: wasm.I64}, Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const}}}},
		},
	}
	got := ImmutableIntGlobals(m)
	if len(got) != 2 {
		t.Fatalf("constants = %+v, want 2", got)
	}
	if got[0].Index != 1 || got[0].Bits != -1 || !got[0].I32 {
		t.Fatalf("i32 constant = %+v", got[0])
	}
	if c, ok := FindImmutableIntGlobal(got, 2); !ok || c.Bits != 42 || c.I32 {
		t.Fatalf("i64 lookup = %+v, %v", c, ok)
	}
	if _, ok := FindImmutableIntGlobal(got, 0); ok {
		t.Fatal("imported global was folded")
	}
}
