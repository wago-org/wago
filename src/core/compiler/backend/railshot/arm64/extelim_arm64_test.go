//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Extension-elimination coverage, ported from amd64/extelim_test.go. A redundant
// i64.extend_i32_u of a value already in clean zero-upper form (a 32-bit op zeroes
// the upper 32 bits of the X register on AArch64) is elided. The body feeds the
// extend to an outer i64.add as its RHS, so it condenses with no dest hint
// (result == src) — exactly the case the elision targets.
//
//	n + i64.extend_i32_u( (i32.wrap_i64 n) + 1 )
func TestExtendElimZeroExtendsArm64(t *testing.T) {
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
	for _, n := range []uint64{0, 1, 0x7fffffff, 0x80000000, 0xffffffff,
		0x1_0000_0000, 0x7fffffff_ffffffff, 0xffffffff_ffffffff, 0xdead_beef_cafe_babe} {
		want := n + uint64(uint32(n)+1) // extend_i32_u zero-extends the 32-bit sum
		if got := runArm64u(t, m, n); got != want {
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

// TestExtendWrapElimArm64 covers the complementary redundant-extend case: an
// i32.wrap_i64 that immediately re-narrows a just-widened value (or a
// sign/zero-extend feeding a wrap) collapses so no explicit extend survives. The
// value round-trips i64→i32→i64, and only the low 32 bits are observable.
//
//	i64.extend_i32_u( i32.wrap_i64( i64.extend_i32_s( i32.wrap_i64 n ) ) )
func TestExtendWrapElimArm64(t *testing.T) {
	body := []byte{
		0x00,       // 0 locals
		0x20, 0x00, // local.get 0     (i64 n)
		0xa7, // i32.wrap_i64
		0xac, // i64.extend_i32_s
		0xa7, // i32.wrap_i64    (re-narrows: the extend is dead)
		0xad, // i64.extend_i32_u
		0x0b, // end
	}
	m := mod1(t, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, body)
	for _, n := range []uint64{0, 1, 0x80000000, 0xffffffff, 0x1_0000_0000, 0xdead_beef_cafe_babe} {
		want := uint64(uint32(n)) // low 32 bits, zero-extended
		if got := runArm64u(t, m, n); got != want {
			t.Errorf("n=%#x: got %#x want %#x", n, got, want)
		}
	}
	var ms ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &ms}); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if ms.Funcs[0].Peephole["extend-wrap-elim"] == 0 {
		t.Errorf("extend-wrap-elim peep did not fire; peeps=%v", ms.Funcs[0].Peephole)
	}
}
