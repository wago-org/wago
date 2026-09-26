package amd64

import (
	"bytes"
	"testing"
)

func TestLegacyMovdquEncoding(t *testing.T) {
	tests := []struct {
		name string
		emit func(*Asm)
		want []byte
	}{
		{"load", func(a *Asm) { a.MovdquLoadDisp(RAX, RDI, 0) }, []byte{0xf3, 0x0f, 0x6f, 0x07}},
		{"store", func(a *Asm) { a.MovdquStoreDisp(RDI, 0, RAX) }, []byte{0xf3, 0x0f, 0x7f, 0x07}},
		{"high-load", func(a *Asm) { a.MovdquLoadDisp(R9, R12, 16) }, []byte{0xf3, 0x45, 0x0f, 0x6f, 0x4c, 0x24, 0x10}},
		{"high-store", func(a *Asm) { a.MovdquStoreDisp(R12, 16, R9) }, []byte{0xf3, 0x45, 0x0f, 0x7f, 0x4c, 0x24, 0x10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Asm{}
			tt.emit(a)
			if !bytes.Equal(a.B, tt.want) {
				t.Fatalf("got %x want %x", a.B, tt.want)
			}
		})
	}
}
