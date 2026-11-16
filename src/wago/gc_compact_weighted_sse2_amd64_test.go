//go:build linux && amd64 && (wago_gc_compact_weighted_sse2 || wago_gc_direct_weighted_sse2)

package wago

import "testing"

func TestCompactNativeWeightedSSE2Tails(t *testing.T) {
	pattern := []uint32{0, 0xffffffff, 0x80000001, 7, 0x7fffffff, 0x12345678, 0x87654321, 3, 0xfffffffd, 0x80000000, 19, 42, 0, 1, 2, 3}
	for _, weight := range []int32{0, 1, -7, 0x7fffffff, -0x80000000} {
		// Remaining count after the checked first iteration is 3/4/5/7/8/9.
		for _, bound := range []int32{4, 5, 6, 8, 9, 10} {
			in := weightedLoopInstance(t, compactWeightedModule(weight, bound, pattern...))
			for _, first := range []uint32{0, 1, uint32(bound) - 1} {
				index, sum := first, uint32(0xfffffff0)
				for {
					sum += (index + uint32(weight)) * pattern[index]
					index++
					if index >= uint32(bound) {
						break
					}
				}
				got, e := in.Invoke("run", 16, uint64(first), 0xfffffff0, 0)
				if e != nil || len(got) != 2 || got[0] != uint64(index) || got[1] != uint64(sum) {
					t.Fatalf("weight%d bound%d first%d got%v %v want%d,%x", weight, bound, first, got, e, index, sum)
				}
			}
		}
	}
}
