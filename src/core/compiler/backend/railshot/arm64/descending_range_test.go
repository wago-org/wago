//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"
)

func TestDescendingRangeMatcher(t *testing.T) {
	// One of the retained sort/merge loops; offsets preserve the encoded branches.
	const start = 0x210
	words := []uint32{0xeb19029f, 0x54001008, 0xb8746b55, 0x6b1602bf, 0x54000129, 0x1100128b, 0xeb19017f, 0x54000f48, 0xb82b6b55, 0x51001294, 0x71000673, 0x54fffea1}
	makeCode := func() []byte {
		b := make([]byte, 0x418)
		for i, w := range words {
			binary.LittleEndian.PutUint32(b[start+i*4:], w)
		}
		return b
	}
	b := makeCode()
	p, ok := matchDescendingRange(b, start, nil)
	if !ok {
		t.Fatal("expected loop rejected")
	}
	if p.pointer != X20 || p.count != X19 || p.exit != 0x244 || p.trap != 0x414 {
		t.Fatalf("unexpected match: %+v", p)
	}
	for _, i := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11} {
		b = makeCode()
		binary.LittleEndian.PutUint32(b[start+i*4:], nopWord)
		if _, ok := matchDescendingRange(b, start, nil); ok {
			t.Fatalf("accepted missing instruction %d", i)
		}
	}
	b = makeCode()
	targets := make([]uint64, (len(b)/4+63)/64)
	branchTargetAdd(targets, start+12, len(b))
	if _, ok := matchDescendingRange(b, start, targets); ok {
		t.Fatal("accepted branch into loop")
	}
}
