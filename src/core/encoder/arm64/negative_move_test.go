package arm64

import (
	"bytes"
	"testing"
)

func TestNegativeMove32Selection(t *testing.T) {
	for _, tc := range []struct {
		v    int32
		word uint32
	}{
		{-1, 0x12800000}, {-17, 0x12800200}, {-32768, 0x128fffe0}, {-65536, 0x129fffe0},
	} {
		a := Asm{AllowSingleNegativeMove32: true, DisableCompactMoveImmediate32: true, DisableLogicalMoveImmediate: true}
		a.MovImm32(X0, tc.v)
		if got := word(&a); got != tc.word {
			t.Fatalf("v=%d got=%08x want=%08x", tc.v, got, tc.word)
		}
	}
	for _, v := range []int32{0, 1, 65535, 65536, -65537, -2147483648} {
		off := Asm{DisableCompactMoveImmediate32: true, DisableLogicalMoveImmediate: true}
		off.MovImm32(X0, v)
		on := Asm{AllowSingleNegativeMove32: true, DisableCompactMoveImmediate32: true, DisableLogicalMoveImmediate: true}
		on.MovImm32(X0, v)
		if !bytes.Equal(off.B, on.B) {
			t.Fatalf("unexpected cover at %d", v)
		}
	}
}
