package amd64

import (
	"bytes"
	"testing"
)

// Golden VEX byte encodings, cross-checked against the AVX spec / a reference
// assembler (3-byte VEX form).
func TestVexEncoding(t *testing.T) {
	cases := []struct {
		name string
		emit func(a *Asm)
		want []byte
	}{
		{"vaddsd xmm0,xmm1,xmm2", func(a *Asm) { a.VFAdd(0, 1, 2, true) }, []byte{0xC4, 0xE1, 0x73, 0x58, 0xC2}},
		{"vmulsd xmm0,xmm1,xmm2", func(a *Asm) { a.VFMul(0, 1, 2, true) }, []byte{0xC4, 0xE1, 0x73, 0x59, 0xC2}},
		{"vsubss xmm0,xmm1,xmm2", func(a *Asm) { a.VFSub(0, 1, 2, false) }, []byte{0xC4, 0xE1, 0x72, 0x5C, 0xC2}},
		{"vaddsd xmm8,xmm1,xmm2", func(a *Asm) { a.VFAdd(8, 1, 2, true) }, []byte{0xC4, 0x61, 0x73, 0x58, 0xC2}},
		{"vaddsd xmm0,xmm1,xmm10", func(a *Asm) { a.VFAdd(0, 1, 10, true) }, []byte{0xC4, 0xC1, 0x73, 0x58, 0xC2}},
		{"vandpd xmm0,xmm1,xmm2", func(a *Asm) { a.VSseRRR(0b01, 0x54, 0, 1, 2) }, []byte{0xC4, 0xE1, 0x71, 0x54, 0xC2}},
		{"vxorps xmm3,xmm3,xmm5", func(a *Asm) { a.VSseRRR(0b00, 0x57, 3, 3, 5) }, []byte{0xC4, 0xE1, 0x60, 0x57, 0xDD}},
	}
	for _, c := range cases {
		a := &Asm{}
		c.emit(a)
		if !bytes.Equal(a.B, c.want) {
			t.Errorf("%s: got % x want % x", c.name, a.B, c.want)
		}
	}
}
