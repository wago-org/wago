//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// stFlags eqz-fusion (R1): `eqz` over a fusable compare feeding a branch fuses by
// inverting the branch condition rather than materializing the inner boolean
// (SETcc+MOVZX+TEST). These tests cover end-to-end correctness (both branch
// directions, nested eqz), that the fold actually fires and elides the SETcc, and
// that the kill switch (WAGO_NO_STFLAGS) is behavior-neutral.

// buildEqzBranch returns func (param i32)(result i32) with body:
//
//	local.get 0; i32.const 5; <cmp>; [eqz × n]; if (result i32) 10 else 20 end
//
// i.e. `if (<n eqz>(x <cmp> 5)) 10 else 20`.
func buildEqzBranch(cmp byte, nEqz int) []byte {
	body := []byte{0x00, 0x20, 0x00, 0x41, 0x05, cmp}
	for i := 0; i < nEqz; i++ {
		body = append(body, 0x45) // i32.eqz
	}
	body = append(body,
		0x04, 0x7f, // if (result i32)
		0x41, 0x0a, // i32.const 10
		0x05,       // else
		0x41, 0x14, // i32.const 20
		0x0b, // end (if)
		0x0b, // end (func)
	)
	return body
}

func TestEqzFoldExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	// x i32.lt_s 5 : true for x<5. eqz inverts. args span both sides of 5.
	cases := []struct {
		name string
		cmp  byte
		nEqz int
		// want[arg] result
		want map[int32]int32
	}{
		// if (x < 5) 10 else 20
		{"lt_s/0eqz", 0x48, 0, map[int32]int32{1: 10, 5: 20, 9: 20}},
		// if (!(x < 5)) 10 else 20  ==  if (x >= 5) 10 else 20
		{"lt_s/1eqz", 0x48, 1, map[int32]int32{1: 20, 5: 10, 9: 10}},
		// if (!!(x < 5)) 10 else 20  ==  if (x < 5) 10 else 20
		{"lt_s/2eqz", 0x48, 2, map[int32]int32{1: 10, 5: 20, 9: 20}},
		// if (!(x == 5)) 10 else 20
		{"eq/1eqz", 0x46, 1, map[int32]int32{5: 20, 4: 10}},
		// if (!(x >= 5)) 10 else 20 == if (x < 5)
		{"ge_s/1eqz", 0x4e, 1, map[int32]int32{1: 10, 5: 20}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := mod1(t, i32, i32, buildEqzBranch(c.cmp, c.nEqz))
			for arg, want := range c.want {
				if got := runAmd64(t, m, arg); got != want {
					t.Errorf("arg=%d: got %d, want %d", arg, got, want)
				}
			}
		})
	}
}

// TestEqzFoldBrIf covers the br_if consumer (the other fused branch path):
//
//	func (param i32)(result i32): (local i32=0)
//	block: local.get 0; i32.const 5; i32.lt_s; i32.eqz; br_if 0 (to block end)
//	       i32.const 1; local.set 1        ;; only runs when NOT branched
//	end; local.get 1
//
// So local1 == 1 iff we did NOT branch, i.e. iff !(x<5) is false, i.e. x<5.
func TestEqzFoldBrIf(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	body := []byte{
		0x01, 0x01, 0x7f, // 1 local: i32 (local 1)
		0x02, 0x40, // block (void)
		0x20, 0x00, 0x41, 0x05, 0x48, 0x45, // local.get 0; i32.const 5; i32.lt_s; i32.eqz
		0x0d, 0x00, // br_if 0  (branch when !(x<5))
		0x41, 0x01, 0x21, 0x01, // i32.const 1; local.set 1
		0x0b,       // end block
		0x20, 0x01, // local.get 1
		0x0b, // end func
	}
	m := mod1(t, i32, i32, body)
	for arg, want := range map[int32]int32{1: 1, 4: 1, 5: 0, 9: 0} {
		if got := runAmd64(t, m, arg); got != want {
			t.Errorf("arg=%d: got %d, want %d", arg, got, want)
		}
	}
}

// TestEqzFoldFires proves the fold fired (eqz-fold counter) and that no SETcc was
// emitted for the inner compare (compare-setcc == 0 — the whole point).
func TestEqzFoldFires(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := mod1(t, i32, i32, buildEqzBranch(0x48, 1)) // if (!(x<5)) ...
	on := compileWithStats(t, m, false).Funcs[0]
	if on.Peephole["eqz-fold"] != 1 {
		t.Errorf("eqz-fold = %d, want 1 (all: %v)", on.Peephole["eqz-fold"], on.Peephole)
	}
	if on.Peephole["cmp-branch-fuse"] != 1 {
		t.Errorf("cmp-branch-fuse = %d, want 1 (the branch still fuses)", on.Peephole["cmp-branch-fuse"])
	}
	if on.Peephole["compare-setcc"] != 0 {
		t.Errorf("compare-setcc = %d, want 0 (fold must elide the SETcc)", on.Peephole["compare-setcc"])
	}
	// Nested eqz peels to a single fold pair (double inversion), still no SETcc.
	m2 := mod1(t, i32, i32, buildEqzBranch(0x48, 2))
	s2 := compileWithStats(t, m2, false).Funcs[0]
	if s2.Peephole["eqz-fold"] != 2 {
		t.Errorf("nested eqz-fold = %d, want 2", s2.Peephole["eqz-fold"])
	}
	if s2.Peephole["compare-setcc"] != 0 {
		t.Errorf("nested compare-setcc = %d, want 0", s2.Peephole["compare-setcc"])
	}
}

// TestEqzFoldKillSwitchEquivalent verifies the fold is behavior-neutral: the same
// module produces the same results with the fold ON and OFF (WAGO_NO_STFLAGS).
func TestEqzFoldKillSwitchEquivalent(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	defer func(prev bool) { stFlagsEnabled = prev }(stFlagsEnabled)
	for _, cmp := range []byte{0x46, 0x48, 0x4e} { // eq, lt_s, ge_s
		for nEqz := 0; nEqz <= 3; nEqz++ {
			body := buildEqzBranch(cmp, nEqz)
			for _, arg := range []int32{1, 4, 5, 9} {
				stFlagsEnabled = true
				mOn := mod1(t, i32, i32, body)
				gotOn := runAmd64(t, mOn, arg)
				stFlagsEnabled = false
				mOff := mod1(t, i32, i32, body)
				gotOff := runAmd64(t, mOff, arg)
				if gotOn != gotOff {
					t.Errorf("cmp=%#x nEqz=%d arg=%d: on=%d off=%d", cmp, nEqz, arg, gotOn, gotOff)
				}
			}
		}
	}
}
