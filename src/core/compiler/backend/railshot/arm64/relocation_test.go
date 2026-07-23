//go:build arm64

package arm64

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestPatchCallRelocsRangeChecks(t *testing.T) {
	t.Run("in range", func(t *testing.T) {
		code := make([]byte, 8)
		binary.LittleEndian.PutUint32(code, 0x94000000) // BL placeholder
		err := patchCallRelocs(code, []int{0, 4}, []int{0, 4}, [][]callReloc{{{at: 0, target: 1}}, nil})
		if err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint32(code); got != 0x94000001 {
			t.Fatalf("patched BL = %#x, want %#x", got, uint32(0x94000001))
		}
	})
	t.Run("out of range", func(t *testing.T) {
		code := make([]byte, 4)
		binary.LittleEndian.PutUint32(code, 0x94000000)
		err := patchCallRelocs(code, []int{0, 1 << 27}, []int{0, 1 << 27}, [][]callReloc{{{at: 0, target: 1}}, nil})
		if err == nil || !strings.Contains(err.Error(), "exceeds BL range") {
			t.Fatalf("out-of-range relocation error = %v", err)
		}
	})
}
