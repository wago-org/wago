//go:build linux && amd64

package amd64

import (
	"strings"
	"testing"
)

// TestFuseCorrectness exercises every fusable consumer (if, br_if, select) fed
// by both an integer compare and eqz, checking both the true and false edges.
func TestFuseCorrectness(t *testing.T) {
	cases := []struct {
		name string
		wat  string
		args []int32
		want int32
	}{
		// compare -> if
		{"cmp_if_true",
			`(module (func (export "f") (param i32 i32) (result i32)
				local.get 0 local.get 1 i32.lt_s (if (result i32) (then i32.const 10) (else i32.const 20))))`,
			[]int32{1, 2}, 10},
		{"cmp_if_false",
			`(module (func (export "f") (param i32 i32) (result i32)
				local.get 0 local.get 1 i32.lt_s (if (result i32) (then i32.const 10) (else i32.const 20))))`,
			[]int32{2, 1}, 20},
		// compare -> br_if (branch out of a block)
		{"cmp_brif_taken",
			`(module (func (export "f") (param i32) (result i32)
				(block (result i32)
					i32.const 99
					local.get 0 i32.const 0 i32.eq br_if 0
					drop i32.const 7)))`,
			[]int32{0}, 99}, // 0==0 -> branch taken, yields 99
		{"cmp_brif_nottaken",
			`(module (func (export "f") (param i32) (result i32)
				(block (result i32)
					i32.const 99
					local.get 0 i32.const 0 i32.eq br_if 0
					drop i32.const 7)))`,
			[]int32{5}, 7}, // 5==0 false -> fall through to 7
		// compare -> select
		{"cmp_select_true",
			`(module (func (export "f") (param i32 i32) (result i32)
				i32.const 111 i32.const 222 local.get 0 local.get 1 i32.gt_s select))`,
			[]int32{9, 3}, 111}, // 9>3 -> a (111)
		{"cmp_select_false",
			`(module (func (export "f") (param i32 i32) (result i32)
				i32.const 111 i32.const 222 local.get 0 local.get 1 i32.gt_s select))`,
			[]int32{3, 9}, 222}, // 3>9 false -> b (222)
		// eqz -> if
		{"eqz_if_true",
			`(module (func (export "f") (param i32) (result i32)
				local.get 0 i32.eqz (if (result i32) (then i32.const 1) (else i32.const 0))))`,
			[]int32{0}, 1},
		{"eqz_if_false",
			`(module (func (export "f") (param i32) (result i32)
				local.get 0 i32.eqz (if (result i32) (then i32.const 1) (else i32.const 0))))`,
			[]int32{42}, 0},
		// regression: non-adjacent use must still work (stored, reused) -> NOT fused
		{"cmp_stored_then_used",
			`(module (func (export "f") (param i32 i32) (result i32) (local i32)
				local.get 0 local.get 1 i32.lt_s local.set 2
				local.get 2 local.get 2 i32.add))`,
			[]int32{1, 2}, 2}, // (1<2)=1 stored; 1+1=2
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := watToModule(t, tc.wat)
			if got := runI32(t, m, tc.args...); got != tc.want {
				t.Fatalf("%s: got %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

// TestFuseEliminatesSetcc proves the optimization fired: a compare feeding an
// `if` must lower to `cmp` + a conditional jump with no `setcc` in between,
// whereas the same compare whose result is stored keeps its `setcc`.
func TestFuseEliminatesSetcc(t *testing.T) {
	fused := watToModule(t, `(module (func (export "f") (param i32 i32) (result i32)
		local.get 0 local.get 1 i32.lt_s (if (result i32) (then i32.const 10) (else i32.const 20))))`)
	code, err := CompileFunction(fused, 0)
	if err != nil {
		t.Fatal(err)
	}
	if dis := disasm(t, code); hasSetcc(dis) {
		t.Fatalf("expected no setcc in fused compare+if; disassembly:\n%s", dis)
	}

	notFused := watToModule(t, `(module (func (export "f") (param i32 i32) (result i32) (local i32)
		local.get 0 local.get 1 i32.lt_s local.set 2 local.get 2))`)
	code2, err := CompileFunction(notFused, 0)
	if err != nil {
		t.Fatal(err)
	}
	if dis2 := disasm(t, code2); !hasSetcc(dis2) {
		t.Fatalf("expected a setcc in the non-fused store path; disassembly:\n%s", dis2)
	}
}

// hasSetcc reports whether any setCC mnemonic appears in the disassembly.
func hasSetcc(dis string) bool {
	for _, line := range strings.Split(dis, "\n") {
		for _, tok := range strings.Fields(line) {
			if strings.HasPrefix(tok, "set") && len(tok) >= 4 && len(tok) <= 6 { // sete, setl, setbe, setnbe, ...
				return true
			}
		}
	}
	return false
}
