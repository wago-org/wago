package amd64

import (
	"bytes"
	"testing"
)

func TestShiftXEncoding(t *testing.T) {
	for _, tc := range []struct {
		digit           byte
		dst, src, count Reg
		wide            bool
		want            []byte
	}{
		{4, RAX, RBX, RDX, false, []byte{0xc4, 0xe2, 0x69, 0xf7, 0xc3}},
		{5, RAX, RBX, RDX, false, []byte{0xc4, 0xe2, 0x6b, 0xf7, 0xc3}},
		{7, RAX, RBX, RDX, false, []byte{0xc4, 0xe2, 0x6a, 0xf7, 0xc3}},
		{4, R9, R10, R11, true, []byte{0xc4, 0x42, 0xa1, 0xf7, 0xca}},
	} {
		var a Asm
		a.ShiftX(tc.digit, tc.dst, tc.src, tc.count, tc.wide)
		if !bytes.Equal(a.B, tc.want) || !a.UsesBMI2 {
			t.Fatalf("got %x want %x BMI2=%v", a.B, tc.want, a.UsesBMI2)
		}
	}
}
