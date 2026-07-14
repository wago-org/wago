package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestConstExpressionByteFormsAndInitializers(t *testing.T) {
	for _, tc := range []struct {
		body []byte
		want wasm.ValType
		bits uint64
	}{
		{[]byte{0x41, 0x7f, 0x0b}, wasm.I32, 0xffffffff},
		{[]byte{0x42, 0x7f, 0x0b}, wasm.I64, ^uint64(0)},
		{[]byte{0x43, 1, 2, 3, 4, 0x0b}, wasm.F32, 0x04030201},
		{[]byte{0x44, 1, 2, 3, 4, 5, 6, 7, 8, 0x0b}, wasm.F64, 0x0807060504030201},
		{[]byte{0xd0, 0x70, 0x0b}, wasm.FuncRef, 0},
		{[]byte{0xd0, 0x6f, 0x0b}, wasm.ExternRef, 0},
		{[]byte{0xd2, 0x03, 0x0b}, wasm.FuncRef, 0},
	} {
		res, err := evalConstExprBytes(tc.body, tc.want)
		if err != nil || res.bits != tc.bits {
			t.Fatalf("eval %x = %#v, %v", tc.body, res, err)
		}
	}
	v128 := append([]byte{0xfd, 0x0c}, append([]byte{1}, append(make([]byte, 15), 0x0b)...)...)
	if res, err := evalConstExprBytes(v128, wasm.V128); err != nil || res.v128[0] != 1 {
		t.Fatalf("v128 eval = %#v, %v", res, err)
	}
	for _, tc := range []struct {
		body []byte
		want wasm.ValType
	}{
		{[]byte{0xd0, 0x7f, 0x0b}, wasm.FuncRef},
		{[]byte{0xfd, 0x00, 0x0b}, wasm.V128},
		{[]byte{0x41, 0x00}, wasm.I32},
		{[]byte{0x41, 0x00, 0x00}, wasm.I32},
		{[]byte{0x41, 0x00, 0x0b, 0x00}, wasm.I32},
		{[]byte{0x41, 0x00, 0x0b}, wasm.I64},
	} {
		if _, err := evalConstExpr(tc.body, tc.want); err == nil {
			t.Fatalf("invalid const expression accepted: %x", tc.body)
		}
	}
	init := constExprInit{Bits: 7, GlobalIndex: 2, FuncIndex: 3}
	var g GlobalDef
	applyGlobalInit(&g, init)
	if g.Bits != 7 || !g.HasInitGlobal || g.InitGlobal != 2 || !g.HasInitFunc || g.InitFunc != 3 {
		t.Fatalf("global initializer = %#v", g)
	}
	var o OffsetInit
	applyOffsetInit(&o, init)
	if o.Base != 7 || !o.HasGlobal || o.Global != 2 {
		t.Fatalf("offset initializer = %#v", o)
	}
}

func TestConstExpressionModuleGlobalAndInstructionForms(t *testing.T) {
	m := &wasm.Module{Imports: []wasm.Import{{Type: wasm.ExternType{
		Kind:   wasm.ExternGlobal,
		Global: wasm.GlobalType{Type: wasm.I64},
	}}}}
	res, err := evalConstExprBytesWithModule([]byte{0x23, 0x00, 0x0b}, wasm.I64, m)
	if err != nil || res.GlobalIndex != 0 {
		t.Fatalf("imported global expression = %#v, %v", res, err)
	}
	for _, bad := range []*wasm.Module{
		nil,
		{Imports: []wasm.Import{{Type: wasm.ExternType{Kind: wasm.ExternGlobal, Global: wasm.GlobalType{Type: wasm.I64, Mutable: true}}}}},
		{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I64}}}},
	} {
		if _, err := evalConstExprBytesWithModule([]byte{0x23, 0x00, 0x0b}, wasm.I64, bad); err == nil {
			t.Fatal("invalid global.get constant expression accepted")
		}
	}
	for _, tc := range []struct {
		in   wasm.Instruction
		want wasm.ValType
		bits uint64
	}{
		{wasm.Instruction{Kind: wasm.InstrI32Const, I32: -2}, wasm.I32, 0xfffffffe},
		{wasm.Instruction{Kind: wasm.InstrI64Const, I64: -3}, wasm.I64, ^uint64(2)},
		{wasm.Instruction{Kind: wasm.InstrF32Const, F32Bits: 7}, wasm.F32, 7},
		{wasm.Instruction{Kind: wasm.InstrF64Const, F64Bits: 8}, wasm.F64, 8},
		{wasm.Instruction{Kind: wasm.InstrRefFunc, Index: 4}, wasm.FuncRef, 0},
		{wasm.Instruction{Kind: wasm.InstrV128Const}, wasm.V128, 0},
	} {
		got, err := evalConstExprWithModule(wasm.Expr{Instrs: []wasm.Instruction{tc.in}}, tc.want, m)
		if err != nil || got.bits != tc.bits {
			t.Fatalf("instruction expression %#v = %#v, %v", tc.in, got, err)
		}
	}
	got, err := evalConstExprWithModule(wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrGlobalGet}}}, wasm.I64, m)
	if err != nil || got.GlobalIndex != 0 {
		t.Fatalf("instruction global.get = %#v, %v", got, err)
	}
	for _, expr := range []wasm.Expr{{}, {Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const}, {Kind: wasm.InstrI32Const}}}, {Instrs: []wasm.Instruction{{Kind: wasm.InstrGlobalGet}}}} {
		if _, err := evalConstExprWithModule(expr, wasm.I32, nil); err == nil {
			t.Fatalf("invalid instruction expression accepted: %#v", expr)
		}
	}
}
