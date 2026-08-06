package amd64

import (
	"bytes"
	"testing"
)

func TestLeaScaledDisplacements(t *testing.T) {
	cases := []struct {
		name string
		emit func(*Asm)
		want []byte
	}{
		{"no-displacement", func(a *Asm) { a.LeaScaledW(RAX, RBX, RCX, 2, 0, true) }, []byte{0x48, 0x8d, 0x04, 0x8b}},
		{"disp8", func(a *Asm) { a.LeaScaledW(RAX, RBX, RCX, 2, 7, true) }, []byte{0x48, 0x8d, 0x44, 0x8b, 0x07}},
		{"disp32", func(a *Asm) { a.LeaScaledW(RAX, RBX, RCX, 2, 0x12345678, true) }, []byte{0x48, 0x8d, 0x84, 0x8b, 0x78, 0x56, 0x34, 0x12}},
		{"rbp-zero-needs-disp8", func(a *Asm) { a.LeaScaledW(RAX, RBP, RCX, 2, 0, true) }, []byte{0x48, 0x8d, 0x44, 0x8d, 0x00}},
		{"extended-registers", func(a *Asm) { a.LeaScaledW(R8, R13, R12, 1, 0, true) }, []byte{0x4f, 0x8d, 0x44, 0x65, 0x00}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.emit(&a)
			if !bytes.Equal(a.B, tc.want) {
				t.Fatalf("got % x, want % x", a.B, tc.want)
			}
		})
	}
}
