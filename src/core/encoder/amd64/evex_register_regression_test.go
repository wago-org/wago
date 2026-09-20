package amd64

import (
	"bytes"
	"testing"
)

func TestEVEXHighSourceRegisters(t *testing.T) {
	for _, tc := range []struct {
		name string
		emit func(*Asm)
		want []byte
	}{
		{"three operands", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, RCX, RDX, R11) }, []byte{0x62, 0xd1, 0x6d, 0x48, 0xfc, 0xcb}},
		{"two operands", func(a *Asm) { a.ZSIMDRR(1, 0, 0x51, false, RCX, R10) }, []byte{0x62, 0xd1, 0x7c, 0x48, 0x51, 0xca}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.emit(&a)
			if !bytes.Equal(a.B, tc.want) {
				t.Fatalf("got % x, want % x", a.B, tc.want)
			}
		})
	}
}
