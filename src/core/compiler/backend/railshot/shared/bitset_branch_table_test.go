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
