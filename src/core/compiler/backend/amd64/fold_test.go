//go:build linux && amd64

package amd64

import (
	"strings"
	"testing"
)

// TestConstFoldExec checks that folded constant expressions produce the correct
// value, covering wrap-around, shift masking, signed vs unsigned, i64, and the
// div/rem trap edges. i64 cases compare to an expected i64 const so the result
// is an i32 0/1 that runI32 can read.
func TestConstFoldExec(t *testing.T) {
	cases := []struct {
		name string
		wat  string
		want int32
	}{
		{"add_wrap", `i32.const 0x7fffffff i32.const 1 i32.add`, -2147483648}, // 0x80000000
		{"sub_neg", `i32.const 5 i32.const 8 i32.sub`, -3},
		{"mul_wrap", `i32.const 0x10000 i32.const 0x10000 i32.mul`, 0}, // 2^32 -> 0
		{"and", `i32.const 0xF0 i32.const 0x3C i32.and`, 0x30},
		{"or", `i32.const 0xF0 i32.const 0x0F i32.or`, 0xFF},
		{"xor", `i32.const 0xFF i32.const 0x0F i32.xor`, 0xF0},
		{"shl", `i32.const 1 i32.const 4 i32.shl`, 16},
		{"shl_mask", `i32.const 1 i32.const 32 i32.shl`, 1}, // count masked to 0
		{"shr_s", `i32.const -8 i32.const 1 i32.shr_s`, -4},
		{"shr_u", `i32.const -8 i32.const 1 i32.shr_u`, 0x7FFFFFFC},
		{"rotl", `i32.const 0x80000001 i32.const 1 i32.rotl`, 3},
		{"rotr", `i32.const 3 i32.const 1 i32.rotr`, -2147483647}, // 0x80000001
		{"div_s", `i32.const -7 i32.const 2 i32.div_s`, -3},       // truncate toward 0
		{"div_u", `i32.const 7 i32.const 2 i32.div_u`, 3},
		{"rem_s", `i32.const -7 i32.const 2 i32.rem_s`, -1},
		{"rem_u", `i32.const 7 i32.const 3 i32.rem_u`, 1},
		{"rem_s_intmin", `i32.const -2147483648 i32.const -1 i32.rem_s`, 0}, // defined as 0, no trap
		// comparisons exercise the signed/unsigned split
		{"lt_s_true", `i32.const -1 i32.const 1 i32.lt_s`, 1},
		{"lt_u_false", `i32.const -1 i32.const 1 i32.lt_u`, 0}, // 0xFFFFFFFF > 1 unsigned
		{"eq", `i32.const 5 i32.const 5 i32.eq`, 1},
		{"ge_u", `i32.const 3 i32.const 3 i32.ge_u`, 1},
		{"eqz_zero", `i32.const 0 i32.eqz`, 1},
		{"eqz_nonzero", `i32.const 9 i32.eqz`, 0},
		// i64 (compared to an expected i64 const -> i32 bool)
		{"i64_add_wrap", `i64.const 0x7fffffffffffffff i64.const 1 i64.add i64.const -9223372036854775808 i64.eq`, 1},
		{"i64_shr_s", `i64.const -8 i64.const 1 i64.shr_s i64.const -4 i64.eq`, 1},
		{"i64_mul_wrap", `i64.const 0x100000000 i64.const 0x100000000 i64.mul i64.const 0 i64.eq`, 1},
		{"i64_lt_u", `i64.const -1 i64.const 1 i64.lt_u`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := watToModule(t, `(module (func (export "f") (result i32) `+tc.wat+`))`)
			if got := runI32(t, m); got != tc.want {
				t.Fatalf("%s: got %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

// TestConstFoldFires proves folding actually happened (not computed at runtime):
// a folded arithmetic expression emits no arithmetic instruction, and a folded
// divide emits no div — while a divide-by-zero is NOT folded (keeps its div so
// it still traps at runtime).
func TestConstFoldFires(t *testing.T) {
	mnem := func(wat string) string {
		m := watToModule(t, `(module (func (export "f") (result i32) `+wat+`))`)
		code, err := CompileFunction(m, 0)
		if err != nil {
			t.Fatal(err)
		}
		return disasm(t, code)
	}
	hasMnemonic := func(dis, m string) bool {
		for _, line := range strings.Split(dis, "\n") {
			for _, tok := range strings.Fields(line) {
				if tok == m {
					return true
				}
			}
		}
		return false
	}

	if d := mnem(`i32.const 2 i32.const 3 i32.add`); hasMnemonic(d, "add") || hasMnemonic(d, "imul") {
		t.Fatalf("expected 2+3 folded (no add); disasm:\n%s", d)
	}
	if d := mnem(`i32.const 6 i32.const 2 i32.div_u`); hasMnemonic(d, "div") {
		t.Fatalf("expected 6/2 folded (no div); disasm:\n%s", d)
	}
	if d := mnem(`i32.const 1 i32.const 0 i32.div_u`); !hasMnemonic(d, "div") {
		t.Fatalf("expected 1/0 NOT folded (keeps div to trap); disasm:\n%s", d)
	}
}
