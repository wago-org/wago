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

func TestLegacySSEOpcodeMaps(t *testing.T) {
	for _, tc := range []struct {
		name              string
		prefix, opmap, op byte
		dst, src          Reg
		imm               *byte
		want              []byte
	}{
		{"paddd", 0x66, 0, 0xfe, 9, 10, nil, []byte{0x66, 0x45, 0x0f, 0xfe, 0xca}},
		{"pshufb", 0x66, 0x38, 0, 9, 10, nil, []byte{0x66, 0x45, 0x0f, 0x38, 0, 0xca}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := Asm{}
			a.SseMapRR(tc.prefix, tc.opmap, tc.op, tc.dst, tc.src)
			if !bytes.Equal(a.B, tc.want) {
				t.Fatalf("got %x want %x", a.B, tc.want)
			}
		})
	}
	a := Asm{}
	a.SseMapRRI(0x66, 0, 0x72, 6, 9, 7)
	if want := []byte{0x66, 0x41, 0x0f, 0x72, 0xf1, 7}; !bytes.Equal(a.B, want) {
		t.Fatalf("pslld got %x want %x", a.B, want)
	}
}

func TestSharedSSEInstructionForms(t *testing.T) {
	// Checked with GNU as/objdump, forcing {vex3} where a two-byte VEX
	// encoding is also legal. Wago deliberately keeps the three-byte form.
	for _, tc := range []struct {
		name string
		emit func(*Asm)
		want []byte
	}{
		{"vpaddd", func(a *Asm) { a.VexMapRRR(0, 1, 0xfe, 8, 1, 2) }, []byte{0xc4, 0x61, 0x71, 0xfe, 0xc2}},
		{"vpshufb", func(a *Asm) { a.VexMapRRR(0x38, 1, 0, 8, 1, 2) }, []byte{0xc4, 0x62, 0x71, 0, 0xc2}},
		{"vpsrld", func(a *Asm) { a.VexShiftImm(0x72, 2, 9, 10, 7) }, []byte{0xc4, 0xc1, 0x31, 0x72, 0xd2, 7}},
		{"vsqrtpd", func(a *Asm) { a.VexMapRR(0, 1, 0x51, 9, 10) }, []byte{0xc4, 0x41, 0x79, 0x51, 0xca}},
		{"vpabsb", func(a *Asm) { a.VexMapRR(0x38, 1, 0x1c, 9, 10) }, []byte{0xc4, 0x42, 0x79, 0x1c, 0xca}},
		{"vpaddd memory", func(a *Asm) { a.VSseMemDisp(1, 0xfe, 8, 1, 12, 32) }, []byte{0xc4, 0x41, 0x71, 0xfe, 0x44, 0x24, 32}},
		{"movdqu memory", func(a *Asm) { a.MovdquDisp(0x6f, 9, 12, 16) }, []byte{0xf3, 0x45, 0x0f, 0x6f, 0x4c, 0x24, 16}},
		{"vmovdqu memory", func(a *Asm) { a.VMovdquDisp(0x6f, 9, 12, 32) }, []byte{0xc4, 0x41, 0x7a, 0x6f, 0x4c, 0x24, 32}},
		{"vmovdqu indexed store", func(a *Asm) { a.VMovdquIdx(0x7f, 10, 8, 9, 127) }, []byte{0xc4, 0x01, 0x7a, 0x7f, 0x54, 0x08, 127}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Asm{}
			tc.emit(a)
			if !bytes.Equal(a.B, tc.want) {
				t.Fatalf("got %x want %x", a.B, tc.want)
			}
		})
	}
}
