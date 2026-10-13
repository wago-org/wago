package shared

import "testing"

func TestBitsetBranchTableAdmission(t *testing.T) {
	for i, labels := range [][]uint32{{0, 1, 0, 1, 0, 1, 0, 1}, {0, 1, 0, 1, 0, 2, 0, 1}, {0, 0, 0, 1, 1, 1, 1, 0}, {1, 1, 1, 1, 1, 1, 1, 1}, {0, 1, 0, 1}} {
		mask, target, ok := BitsetBranchTable(labels, 0)
		want := i == 0
		if ok != want {
			t.Fatalf("labels=%v ok=%v", labels, ok)
		}
		if ok && (mask != 0xaa || target != 1) {
			t.Fatalf("mask=%x target=%d", mask, target)
		}
	}
}

func TestBitsetBranchTableSparseFallback(t *testing.T) {
	for _, tc := range []struct {
		n    int
		mask uint64
		want bool
	}{{37, 0x1100000009, false}, {46, 0x21c00006001b, false}, {16, 0x2a, false}, {16, 0xaa, true}} {
		labels := make([]uint32, tc.n)
		for i := range labels {
			if tc.mask>>uint(i)&1 != 0 {
				labels[i] = 1
			}
		}
		_, _, got := BitsetBranchTable(labels, 0)
		if got != tc.want {
			t.Fatalf("n=%d mask=%x admission=%v want=%v", tc.n, tc.mask, got, tc.want)
		}
	}
}
