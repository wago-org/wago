package amd64

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// Golden bytes were assembled by GNU as 2.44 in Intel syntax.
func TestEVEXRegisterBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		emit func(*Asm)
		hex  string
	}{
		{"vsqrtps zmm7, zmm7", func(a *Asm) { a.ZSIMDRR(1, 0, 0x51, false, Reg(7), Reg(7)) }, "62f17c4851ff"},
		{"vpaddb zmm7, zmm7, zmm7", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(7), Reg(7), Reg(7)) }, "62f14548fcff"},
		{"vpaddb zmm7, zmm8, zmm7", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(7), Reg(8), Reg(7)) }, "62f13d48fcff"},
		{"vpaddb zmm7, zmm15, zmm7", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(7), Reg(15), Reg(7)) }, "62f10548fcff"},
		{"vsqrtps zmm7, zmm8", func(a *Asm) { a.ZSIMDRR(1, 0, 0x51, false, Reg(7), Reg(8)) }, "62d17c4851f8"},
		{"vpaddb zmm7, zmm7, zmm8", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(7), Reg(7), Reg(8)) }, "62d14548fcf8"},
		{"vpaddb zmm7, zmm8, zmm8", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(7), Reg(8), Reg(8)) }, "62d13d48fcf8"},
		{"vpaddb zmm7, zmm15, zmm8", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(7), Reg(15), Reg(8)) }, "62d10548fcf8"},
		{"vsqrtps zmm7, zmm15", func(a *Asm) { a.ZSIMDRR(1, 0, 0x51, false, Reg(7), Reg(15)) }, "62d17c4851ff"},
		{"vpaddb zmm7, zmm7, zmm15", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(7), Reg(7), Reg(15)) }, "62d14548fcff"},
		{"vpaddb zmm7, zmm8, zmm15", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(7), Reg(8), Reg(15)) }, "62d13d48fcff"},
		{"vpaddb zmm7, zmm15, zmm15", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(7), Reg(15), Reg(15)) }, "62d10548fcff"},
		{"vsqrtps zmm8, zmm7", func(a *Asm) { a.ZSIMDRR(1, 0, 0x51, false, Reg(8), Reg(7)) }, "62717c4851c7"},
		{"vpaddb zmm8, zmm7, zmm7", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(8), Reg(7), Reg(7)) }, "62714548fcc7"},
		{"vpaddb zmm8, zmm8, zmm7", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(8), Reg(8), Reg(7)) }, "62713d48fcc7"},
		{"vpaddb zmm8, zmm15, zmm7", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(8), Reg(15), Reg(7)) }, "62710548fcc7"},
		{"vsqrtps zmm8, zmm8", func(a *Asm) { a.ZSIMDRR(1, 0, 0x51, false, Reg(8), Reg(8)) }, "62517c4851c0"},
		{"vpaddb zmm8, zmm7, zmm8", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(8), Reg(7), Reg(8)) }, "62514548fcc0"},
		{"vpaddb zmm8, zmm8, zmm8", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(8), Reg(8), Reg(8)) }, "62513d48fcc0"},
		{"vpaddb zmm8, zmm15, zmm8", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(8), Reg(15), Reg(8)) }, "62510548fcc0"},
		{"vsqrtps zmm8, zmm15", func(a *Asm) { a.ZSIMDRR(1, 0, 0x51, false, Reg(8), Reg(15)) }, "62517c4851c7"},
		{"vpaddb zmm8, zmm7, zmm15", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(8), Reg(7), Reg(15)) }, "62514548fcc7"},
		{"vpaddb zmm8, zmm8, zmm15", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(8), Reg(8), Reg(15)) }, "62513d48fcc7"},
		{"vpaddb zmm8, zmm15, zmm15", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(8), Reg(15), Reg(15)) }, "62510548fcc7"},
		{"vsqrtps zmm15, zmm7", func(a *Asm) { a.ZSIMDRR(1, 0, 0x51, false, Reg(15), Reg(7)) }, "62717c4851ff"},
		{"vpaddb zmm15, zmm7, zmm7", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(15), Reg(7), Reg(7)) }, "62714548fcff"},
		{"vpaddb zmm15, zmm8, zmm7", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(15), Reg(8), Reg(7)) }, "62713d48fcff"},
		{"vpaddb zmm15, zmm15, zmm7", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(15), Reg(15), Reg(7)) }, "62710548fcff"},
		{"vsqrtps zmm15, zmm8", func(a *Asm) { a.ZSIMDRR(1, 0, 0x51, false, Reg(15), Reg(8)) }, "62517c4851f8"},
		{"vpaddb zmm15, zmm7, zmm8", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(15), Reg(7), Reg(8)) }, "62514548fcf8"},
		{"vpaddb zmm15, zmm8, zmm8", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(15), Reg(8), Reg(8)) }, "62513d48fcf8"},
		{"vpaddb zmm15, zmm15, zmm8", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(15), Reg(15), Reg(8)) }, "62510548fcf8"},
		{"vsqrtps zmm15, zmm15", func(a *Asm) { a.ZSIMDRR(1, 0, 0x51, false, Reg(15), Reg(15)) }, "62517c4851ff"},
		{"vpaddb zmm15, zmm7, zmm15", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(15), Reg(7), Reg(15)) }, "62514548fcff"},
		{"vpaddb zmm15, zmm8, zmm15", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(15), Reg(8), Reg(15)) }, "62513d48fcff"},
		{"vpaddb zmm15, zmm15, zmm15", func(a *Asm) { a.ZSIMDRRR(1, 1, 0xfc, false, Reg(15), Reg(15), Reg(15)) }, "62510548fcff"},
		{"vmovdqu64 zmm15, ZMMWORD PTR [r12+r13+64]", func(a *Asm) { a.ZMovdqu64LoadIdx(R15, R12, R13, 64) }, "6211fe486f7c2c01"},
		{"vmovdqu64 ZMMWORD PTR [r13+r14+128], zmm8", func(a *Asm) { a.ZMovdqu64StoreIdx(R13, R14, R8, 128) }, "6211fe487f443502"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, err := hex.DecodeString(tc.hex)
			if err != nil {
				t.Fatal(err)
			}
			var a Asm
			tc.emit(&a)
			if !bytes.Equal(a.B, want) {
				t.Fatalf("got % x, want % x", a.B, want)
			}
		})
	}
}
