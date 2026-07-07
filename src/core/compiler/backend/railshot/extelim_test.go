package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestExtendElimZeroExtends checks that eliminating the redundant zero-extend of a
// clean i32 op preserves i64.extend_i32_u semantics (upper 32 bits zero, never
// sign-extended) and that the elimination actually fires. Body, one i64 param n:
//
//	n + i64.extend_i32_u( (i32.wrap_i64 n) + 1 )
//
// The extend feeds the outer i64.add as its RHS, so it is condensed with no dest
// hint (result == src) — the case where dropping the redundant mov is a real win.
func TestExtendElimZeroExtends(t *testing.T) {
	body := []byte{
		0x00,       // 0 locals
		0x20, 0x00, // local.get 0        (i64 n)
		0x20, 0x00, // local.get 0
		0xa7,       // i32.wrap_i64       (low32 n)
		0x41, 0x01, // i32.const 1
		0x6a, // i32.add            (low32(n)+1, 32-bit → clean upper)
		0xad, // i64.extend_i32_u   (redundant zero-extend → elided)
		0x7c, // i64.add
		0x0b, // end
	}
	m := mod1(t, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, body)
	call, done := divInvoker(t, m)
	defer done()
	for _, n := range []uint64{0, 1, 0x7fffffff, 0x80000000, 0xffffffff,
		0x1_0000_0000, 0x7fffffff_ffffffff, 0xffffffff_ffffffff, 0xdead_beef_cafe_babe} {
		want := n + uint64(uint32(n)+1) // extend_i32_u zero-extends the 32-bit sum
		if got := call(n); got != want {
			t.Errorf("n=%#x: got %#x want %#x", n, got, want)
		}
	}
	var ms ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if ms.Funcs[0].Peephole["ext-elim"] == 0 {
		t.Errorf("ext-elim peep did not fire; peeps=%v", ms.Funcs[0].Peephole)
	}
}
