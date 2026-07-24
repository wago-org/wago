//go:build arm64

package arm64

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestPatchCallRelocsRangeChecks(t *testing.T) {
	t.Run("forward", func(t *testing.T) {
		code := make([]byte, 8)
		binary.LittleEndian.PutUint32(code, 0x94000000) // BL placeholder
		err := patchCallRelocs(code, []int{0, 4}, []int{0, 4}, [][]callReloc{{{at: 0, target: 1}}, nil})
		if err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint32(code); got != 0x94000001 {
			t.Fatalf("patched forward BL = %#x, want %#x", got, uint32(0x94000001))
		}
	})
	t.Run("backward", func(t *testing.T) {
		code := make([]byte, 8)
		binary.LittleEndian.PutUint32(code[4:], 0x94000000)
		err := patchCallRelocs(code, []int{0, 4}, []int{0, 4}, [][]callReloc{nil, {{at: 0, target: 0}}})
		if err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint32(code[4:]); got != 0x97ffffff {
			t.Fatalf("patched backward BL = %#x, want %#x", got, uint32(0x97ffffff))
		}
	})
	t.Run("internal entry", func(t *testing.T) {
		code := make([]byte, 12)
		binary.LittleEndian.PutUint32(code, 0x94000000)
		err := patchCallRelocs(code, []int{0, 4}, []int{0, 8}, [][]callReloc{{{at: 0, target: 1, internal: true}}, nil})
		if err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint32(code); got != 0x94000002 {
			t.Fatalf("patched internal-entry BL = %#x, want %#x", got, uint32(0x94000002))
		}
	})
	t.Run("maximum forward displacement", func(t *testing.T) {
		code := make([]byte, 4)
		binary.LittleEndian.PutUint32(code, 0x94000000)
		target := (1 << 27) - 4
		err := patchCallRelocs(code, []int{0, target}, []int{0, target}, [][]callReloc{{{at: 0, target: 1}}, nil})
		if err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint32(code); got != 0x95ffffff {
			t.Fatalf("maximum-range BL = %#x, want %#x", got, uint32(0x95ffffff))
		}
	})
	t.Run("out of range", func(t *testing.T) {
		code := make([]byte, 4)
		binary.LittleEndian.PutUint32(code, 0x94000000)
		target := 1 << 27
		err := patchCallRelocs(code, []int{0, target}, []int{0, target}, [][]callReloc{{{at: 0, target: 1}}, nil})
		if err == nil || !strings.Contains(err.Error(), "exceeds BL range") {
			t.Fatalf("out-of-range relocation error = %v", err)
		}
	})
}
