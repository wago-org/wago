package wago

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestPreparedScalarArgumentsSmallWidths(t *testing.T) {
	values := []uint64{0xffffffff12345678, 0x80000000ffffffff, 0x0123456789abcdef, 0xfedcba9876543210}
	for n := 0; n <= 4; n++ {
		for mask := uint8(0); mask < 16; mask++ {
			dst := bytes.Repeat([]byte{0xa5}, 40)
			copyPreparedScalarArgumentsSmall(dst, values[:n], mask)
			for slot := 0; slot < n; slot++ {
				want := values[slot]
				if mask&(1<<slot) == 0 {
					want = uint64(uint32(want))
				}
				if got := binary.LittleEndian.Uint64(dst[slot*8:]); got != want {
					t.Fatalf("n=%d mask=%x slot=%d got=%x want=%x", n, mask, slot, got, want)
				}
			}
			if !bytes.Equal(dst[n*8:], bytes.Repeat([]byte{0xa5}, 40-n*8)) {
				t.Fatalf("n=%d mask=%x wrote outside argument slots", n, mask)
			}
		}
	}
	copyPreparedScalarArgumentsSmall(nil, nil, 0)
}

func TestPreparedScalarArgumentsSmallDescendingBounds(t *testing.T) {
	for n := 1; n <= 4; n++ {
		// The highest slot must fail before any lower slot is written, matching
		// the previous descending captured-closure calls.
		dst := bytes.Repeat([]byte{0xa5}, n*8-1)
		before := append([]byte(nil), dst...)
		panicked := false
		func() {
			defer func() { panicked = recover() != nil }()
			copyPreparedScalarArgumentsSmall(dst, []uint64{1, 2, 3, 4}[:n], 15)
		}()
		if !panicked || !bytes.Equal(dst, before) {
			t.Fatalf("n=%d: panic=%v destination changed=%v", n, panicked, !bytes.Equal(dst, before))
		}
	}
}
