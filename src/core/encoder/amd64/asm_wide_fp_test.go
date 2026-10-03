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
		{func(a *Asm) { a.YBroadcastSDLoadDisp(13, RSP, 0) }, []byte{0xc4, 0x62, 0x7d, 0x19, 0x2c, 0x24}},
		{func(a *Asm) { a.YBroadcastSDLoadDisp(9, R13, -128) }, []byte{0xc4, 0x42, 0x7d, 0x19, 0x4d, 0x80}},
		{func(a *Asm) { a.YBroadcastSDLoadIdx(8, R12, R9, 32) }, []byte{0xc4, 0x02, 0x7d, 0x19, 0x44, 0x0c, 0x20}},
		{func(a *Asm) { a.YBroadcastSDLoadIdx(3, R8, R10, -8) }, []byte{0xc4, 0x82, 0x7d, 0x19, 0x5c, 0x10, 0xf8}},
		{func(a *Asm) {
			if site := a.YBroadcastSDRipPlaceholder(8); site != 5 {
				t.Fatalf("RIP displacement site %d", site)
			}
		}, []byte{0xc4, 0x62, 0x7d, 0x19, 0x05, 0, 0, 0, 0}},
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
