//go:build arm64

package arm64

import (
	"encoding/binary"
	"testing"
)

func TestReuseRepeatedNativeAddress(t *testing.T) {
	// add x16,x26,x8; add x16,x16,#65536; ldr w12,[x16]; repeat setup
	words := []uint32{0x8b080350, 0x91404210, 0xb940020c, 0x8b080350, 0x91404210, 0xb940420d}
	makeCode := func() []byte {
		b := make([]byte, len(words)*4)
		for i, w := range words {
			binary.LittleEndian.PutUint32(b[i*4:], w)
		}
		return b
	}
	t.Run("identical", func(t *testing.T) {
		b := makeCode()
		if got := reuseRepeatedNativeAddress(b, nil, nil); got != 1 {
			t.Fatalf("hits=%d", got)
		}
		if rdWord(b, 12) != nopWord || rdWord(b, 16) != nopWord || rdWord(b, 8) != words[2] || rdWord(b, 20) != words[5] {
			t.Fatal("changed the loads or retained redundant setup")
		}
	})
	t.Run("live index overwritten by load", func(t *testing.T) {
		b := makeCode()
		wrWord(b, 8, 0xb9400208)
		if reuseRepeatedNativeAddress(b, nil, nil) != 0 {
			t.Fatal("rewrote changed index")
		}
	})
	t.Run("branch enters interval", func(t *testing.T) {
		for _, target := range []int{4, 8, 12, 16} {
			b := makeCode()
			targets := make([]uint64, 1)
			branchTargetAdd(targets, target, len(b))
			if reuseRepeatedNativeAddress(b, targets, nil) != 0 {
				t.Fatalf("rewrote branch target %d", target)
			}
		}
	})
	t.Run("entry enters interval", func(t *testing.T) {
		b := makeCode()
		if reuseRepeatedNativeAddress(b, nil, []int{12}) != 0 {
			t.Fatal("rewrote entry target")
		}
	})
	t.Run("different displacement", func(t *testing.T) {
		b := makeCode()
		wrWord(b, 16, 0x91004210)
		if reuseRepeatedNativeAddress(b, nil, nil) != 0 {
			t.Fatal("rewrote distinct addresses")
		}
	})
}
