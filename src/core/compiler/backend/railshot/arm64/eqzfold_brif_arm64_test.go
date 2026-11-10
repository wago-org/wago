//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Additional eqz-fusion coverage ported from amd64/eqzfold_test.go: the br_if
// consumer (the other fused branch path, distinct from if/else) and the nested-eqz
// fold-count assertion. The existing eqzfold_arm64_test.go already covers if/else
// exec, single-fold firing, and kill-switch equivalence.

// TestEqzFoldBrIfArm64 covers the br_if consumer:
//
//	func (param i32)(result i32): (local i32=0)
//	block: local.get 0; i32.const 5; i32.lt_s; i32.eqz; br_if 0 (to block end)
//	       i32.const 1; local.set 1        ;; only runs when NOT branched
//	end; local.get 1
//
// local1 == 1 iff we did NOT branch, i.e. iff !(x<5) is false, i.e. x<5.
func TestEqzFoldBrIfArm64(t *testing.T) {
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
	for arg, want := range map[int32]uint32{1: 1, 4: 1, 5: 0, 9: 0} {
		if got := uint32(runArm64Internal2(t, m, uintptr(uint32(arg)), 0)); got != want {
			t.Errorf("arg=%d: got %d, want %d", arg, got, want)
		}
	}

	// The eqz over the compare must fuse into the br_if (inverted condition), with
	// no runtime CSET materialized for the inner compare.
	s := compileWithStats(t, m, false).Funcs[0]
	if s.Peephole["eqz-fold"] != 1 {
		t.Errorf("eqz-fold = %d, want 1 (all: %v)", s.Peephole["eqz-fold"], s.Peephole)
	}
	if s.Peephole["cmp-branch-fuse"] != 1 {
		t.Errorf("cmp-branch-fuse = %d, want 1 (the br_if still fuses)", s.Peephole["cmp-branch-fuse"])
	}
	if s.Peephole["compare-setcc"] != 0 {
		t.Errorf("compare-setcc = %d, want 0 (fold must elide the CSET)", s.Peephole["compare-setcc"])
	}
}

// TestEqzFoldNestedFiresArm64 proves a nested eqz (double inversion) peels to two
// fold pairs and still emits no CSET for the inner compare.
func TestEqzFoldNestedFiresArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	m := mod1(t, i32, i32, buildEqzBranchArm64(0x48, 2)) // if (!!(x<5)) ...
	s := compileWithStats(t, m, false).Funcs[0]
	if s.Peephole["eqz-fold"] != 2 {
		t.Errorf("nested eqz-fold = %d, want 2 (all: %v)", s.Peephole["eqz-fold"], s.Peephole)
	}
	if s.Peephole["compare-setcc"] != 0 {
		t.Errorf("nested compare-setcc = %d, want 0", s.Peephole["compare-setcc"])
	}
}
