//go:build amd64

package amd64

import (
	"bytes"
	"testing"
)

// Checked against LLVM llvm-mc with +avx,-avx2.
func TestWideFPAVXEncodings(t *testing.T) {
	for _, tc := range []struct {
		emit func(*Asm)
		want []byte
	}{
		{func(a *Asm) { a.YInsertF128(8, 9, 10, 1) }, []byte{0xc4, 0x43, 0x35, 0x18, 0xc2, 0x01}},
		{func(a *Asm) { a.YFPackedMemIdx(0x58, 2, 12, RBX, R13, 0, true) }, []byte{0xc4, 0xa1, 0x1d, 0x58, 0x14, 0x2b}},
		{func(a *Asm) { a.YFPackedMemIdx(0x5c, 8, 1, R12, R9, 32, true) }, []byte{0xc4, 0x01, 0x75, 0x5c, 0x44, 0x0c, 0x20}},
		{func(a *Asm) { a.YFPackedMemIdx(0x59, 3, 9, R8, R10, -8, true) }, []byte{0xc4, 0x81, 0x35, 0x59, 0x5c, 0x10, 0xf8}},
	} {
		a := &Asm{}
		tc.emit(a)
		if !bytes.Equal(a.B, tc.want) {
			t.Fatalf("got %x want %x", a.B, tc.want)
		}
	}
}
