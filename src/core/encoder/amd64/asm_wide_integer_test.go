//go:build amd64

package amd64

import (
	"bytes"
	"testing"
)

// Encodings independently checked with GNU as and objdump on AMD64.
func TestWideIntegerSLPEncodings(t *testing.T) {
	for _, tc := range []struct {
		emit func(*Asm)
		want []byte
	}{
		{func(a *Asm) { a.YBroadcastDLoadDisp(13, RSP, 0) }, []byte{0xc4, 0x62, 0x7d, 0x58, 0x2c, 0x24}},
		{func(a *Asm) { a.YBroadcastDLoadIdx(8, R12, R9, 32) }, []byte{0xc4, 0x02, 0x7d, 0x58, 0x44, 0x0c, 0x20}},
		{func(a *Asm) { a.YPshufd(9, 10, 0xb1) }, []byte{0xc4, 0x41, 0x7d, 0x70, 0xca, 0xb1}},
		{func(a *Asm) { a.YExtractI128(11, 12, 1) }, []byte{0xc4, 0x43, 0x7d, 0x39, 0xe3, 0x01}},
		{func(a *Asm) { a.YExtractI128(0, 0, 1) }, []byte{0xc4, 0xe3, 0x7d, 0x39, 0xc0, 0x01}},
	} {
		var a Asm
		tc.emit(&a)
		if !bytes.Equal(a.B, tc.want) {
			t.Fatalf("got %x want %x", a.B, tc.want)
		}
	}
}
