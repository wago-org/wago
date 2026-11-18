//go:build arm64

package arm64

import (
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"testing"
)

func TestExpensiveLoopConstPreservesTransientFloorArm64(t *testing.T) {
	old := expensiveLoopLeaseEnabled
	t.Cleanup(func() { expensiveLoopLeaseEnabled = old })
	const value = 0x123456789abcdef1
	for _, enabled := range []bool{false, true} {
		expensiveLoopLeaseEnabled = enabled
		for _, helper := range []bool{false, true} {
			for _, freeCount := range []int{4, 5, 6, 7} {
				f := fn{a: &a64.Asm{}, policy: currentCodegenPolicy()}
				candidates := []Reg{X14, X13, X12, X11, X10, X9, X8}
				free := maskOf(candidates[:freeCount]...)
				for _, r := range gpAlloc {
					if !free.has(r) {
						f.reserved = f.reserved.add(r)
					}
				}
				f.reserved = f.reserved.add(X25).add(X24).add(X23).add(X27)
				h := funcHintView{loopIntConstCount: 4, loopIntConstTypes: 0xaa, loopIntConst: [4]int64{value, value + 1, value + 2, value + 3}}
				if helper {
					h.flags |= hintUsesBulkMem
				}
				f.preloadLoopIntConsts(&h)
				remaining := 0
				for _, r := range candidates[:freeCount] {
					if !f.reserved.has(r) {
						remaining++
					}
				}
				if remaining < 4 {
					t.Fatalf("enabled=%v helper=%v free=%d left=%d", enabled, helper, freeCount, remaining)
				}
				want := 0
				if enabled && !helper {
					want = min(freeCount-4, 2)
				}
				if int(f.iconstN) != want {
					t.Fatalf("enabled=%v helper=%v free=%d leases=%d want=%d", enabled, helper, freeCount, f.iconstN, want)
				}
				for i := 0; i < int(f.iconstN); i++ {
					c := f.iconsts[i]
					if c.bits != value+int64(i) || c.typ != mtI64 || c.address || !free.has(c.reg) {
						t.Fatalf("invalid immutable lease: %+v", c)
					}
				}
			}
		}
	}
}
