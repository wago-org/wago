package amd64

import (
	"bytes"
	"testing"
)

func TestAlignEncoding(t *testing.T) {
	for _, tc := range []struct {
		name string
		emit func(*Asm)
		want []byte
	}{
		{"vpalignr-low", func(a *Asm) { a.VPAlignr(0, 1, 2, 14) }, []byte{0xc4, 0xe3, 0x71, 0x0f, 0xc2, 14}},
		{"vpalignr-high", func(a *Asm) { a.VPAlignr(8, 9, 10, 15) }, []byte{0xc4, 0x43, 0x31, 0x0f, 0xc2, 15}},
		{"palignr-high", func(a *Asm) { a.SseMapRRI(0x66, 0x3a, 0x0f, 8, 9, 13) }, []byte{0x66, 0x45, 0x0f, 0x3a, 0x0f, 0xc1, 13}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Asm{}
			tc.emit(a)
			if got := a.B; !bytes.Equal(got, tc.want) {
				t.Fatalf("got %x, want %x", got, tc.want)
			}
		})
	}
}
