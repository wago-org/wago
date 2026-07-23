package amd64

import (
	"bytes"
	"testing"
)

func TestSIBZeroDisplacementPreservesRBPAndR13Base(t *testing.T) {
	for _, tc := range []struct {
		name string
		base Reg
		want []byte
	}{
		{name: "rbp", base: RBP, want: []byte{0x89, 0x7c, 0x35, 0x00}},
		{name: "r13", base: R13, want: []byte{0x41, 0x89, 0x7c, 0x35, 0x00}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			a.StoreIdx(tc.base, RSI, RDI, 0, 4)
			if !bytes.Equal(a.B, tc.want) {
				t.Fatalf("StoreIdx bytes = % x, want % x", a.B, tc.want)
			}
		})
	}
}
