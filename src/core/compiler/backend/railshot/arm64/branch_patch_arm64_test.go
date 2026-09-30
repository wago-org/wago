//go:build arm64

package arm64

import (
	"strings"
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

// These tests inspect compiler state and instruction bytes only. They do not
// map or execute generated code and need no large function or module input.
func TestMandatoryBranchPatchRangeArm64(t *testing.T) {
	for _, bits := range []uint{19, 26} {
		limit := 1 << (bits + 1)
		for _, delta := range []int{-limit - 4, -limit, limit - 4, limit} {
			f := fn{a: &a64.Asm{}}
			var site int
			if bits == 19 {
				site = f.a.Bcond(a64.CondNE)
				f.patchBranch19(site, site+delta)
			} else {
				site = f.a.Branch()
				f.patchBranch26(site, site+delta)
			}
			inRange := delta >= -limit && delta < limit
			if got := f.representationLimit == functionRepresentationOK; got != inRange {
				t.Fatalf("branch%d delta %d: accepted=%v, want %v", bits, delta, got, inRange)
			}
			if inRange {
				target, ok := branchTarget(site, rdWord(f.a.B, site))
				if !ok || target != site+delta {
					t.Fatalf("branch%d target=%d,%v, want %d", bits, target, ok, site+delta)
				}
			}
		}
	}
}

func TestBranchFailureRejectsDisabledFinalizerArm64(t *testing.T) {
	before := nativeFinalizerEnabled
	t.Cleanup(func() { nativeFinalizerEnabled = before })
	for _, enabled := range []bool{false, true} {
		nativeFinalizerEnabled = enabled
		f := fn{a: &a64.Asm{}}
		f.patchBranch19(f.a.Bcond(a64.CondEQ), 1<<20)
		if _, err := f.finalizeNativeCode(0); err == nil || !strings.Contains(err.Error(), "branch displacement") {
			t.Fatalf("finalizer enabled=%v: error=%v", enabled, err)
		}
	}
}

func TestBranchFailurePreservesFirstLimitArm64(t *testing.T) {
	f := fn{a: &a64.Asm{}, representationLimit: functionRepresentationFrameEnd}
	f.patchBranch19(f.a.Bcond(a64.CondEQ), 1<<20)
	if f.representationLimit != functionRepresentationFrameEnd {
		t.Fatal("branch failure replaced the first representation limit")
	}
}
