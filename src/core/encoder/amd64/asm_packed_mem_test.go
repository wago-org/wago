//go:build amd64

package amd64

import (
	"bytes"
	"testing"
)

func TestVFPackedMemIdx(t *testing.T) {
	for _, tc := range []struct {
		op                    byte
		dst, src, base, index Reg
		disp                  int32
		wide                  bool
		want                  []byte
	}{
		{0x58, 2, 12, RBX, R13, 0, true, []byte{0xc4, 0xa1, 0x19, 0x58, 0x14, 0x2b}},
		{0x5c, 8, 1, R12, R9, 32, true, []byte{0xc4, 0x01, 0x71, 0x5c, 0x44, 0x0c, 0x20}},
		{0x59, 3, 9, R8, R10, -8, false, []byte{0xc4, 0x81, 0x30, 0x59, 0x5c, 0x10, 0xf8}},
	} {
		a := &Asm{}
		a.VFPackedMemIdx(tc.op, tc.dst, tc.src, tc.base, tc.index, tc.disp, tc.wide)
		if !bytes.Equal(a.B, tc.want) {
			t.Fatalf("got %x want %x", a.B, tc.want)
		}
	}
}
