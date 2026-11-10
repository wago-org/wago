//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// countdownModule sums n + (n-1) + ... + 1 with a value-less loop back-edge
// `local.get n; br_if 0` (the non-fused opBr path). The edge carries no operand
// values, so it lowers to the foldable `Jcc over; JMP loop; over:` idiom.
func countdownModule(t *testing.T) *wasm.Module {
	return mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32}, []byte{
		0x01, 0x01, 0x7f, // 1 local: sum (index 1)
		0x03, 0x40, // loop void
		0x20, 0x01, 0x20, 0x00, 0x6a, 0x21, 0x01, // sum += n
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // n -= 1
		0x20, 0x00, 0x0d, 0x00, // local.get n; br_if 0  (value-less loop back-edge)
		0x0b,       // end loop
		0x20, 0x01, // local.get sum
		0x0b, // end func
	})
}

// blockExitModule returns 20 when x<5 else 10, via a fused `<compare> br_if` that
// exits a void block (the brIfFused path); the block edge carries no values.
func blockExitModule(t *testing.T) *wasm.Module {
	return mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32}, []byte{
		0x00,       // no locals
		0x02, 0x40, // block void
		0x20, 0x00, 0x41, 0x05, 0x48, // local.get x; i32.const 5; i32.lt_s
		0x0d, 0x00, // br_if 0  (exit block when x<5) — fused compare, value-less
		0x41, 0x0a, 0x0f, // i32.const 10; return   (x>=5)
		0x0b,       // end block
		0x41, 0x14, // i32.const 20  (x<5 lands here)
		0x0b, // end func
	})
}

func triangular(n int32) int32 { return n * (n + 1) / 2 }

func TestBranchFoldExec(t *testing.T) {
	cd := countdownModule(t)
	for _, n := range []int32{1, 3, 5, 10, 100} {
		if got := runAmd64(t, cd, n); got != triangular(n) {
			t.Errorf("countdown(%d) = %d, want %d", n, got, triangular(n))
		}
	}
	bx := blockExitModule(t)
	for _, tc := range []struct{ x, want int32 }{{3, 20}, {4, 20}, {5, 10}, {9, 10}} {
		if got := runAmd64(t, bx, tc.x); got != tc.want {
			t.Errorf("blockExit(%d) = %d, want %d", tc.x, got, tc.want)
		}
	}
}

func TestBranchFoldFires(t *testing.T) {
	for name, m := range map[string]*wasm.Module{"countdown": countdownModule(t), "blockExit": blockExitModule(t)} {
		s := compileWithStats(t, m, false).Funcs[0]
		if s.Peephole["br-pair-fold"] == 0 {
			t.Errorf("%s: br-pair-fold = 0, want >=1 (all: %v)", name, s.Peephole)
		}
	}
}

// TestBranchFoldKillSwitchEquivalent verifies the fold is behavior-neutral.
func TestBranchFoldKillSwitchEquivalent(t *testing.T) {
	defer func(prev bool) { branchFoldEnabled = prev }(branchFoldEnabled)
	for _, n := range []int32{1, 7, 50} {
		branchFoldEnabled = true
		on := runAmd64(t, countdownModule(t), n)
		branchFoldEnabled = false
		off := runAmd64(t, countdownModule(t), n)
		if on != off {
			t.Fatalf("countdown(%d): on=%d off=%d", n, on, off)
		}
	}
}
