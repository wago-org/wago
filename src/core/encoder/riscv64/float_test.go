package riscv64

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Words were obtained from LLVM 19's independently assembled RV64G fixture.
var floatEncodingCases = []struct {
	name string
	emit func(*Asm, Reg, FReg)
	want uint32
}{
	{"FaddS", func(a *Asm, d Reg, f FReg) { a.FaddS(f, F6, F7, RNE) }, 0x007302d3},
	{"FsubS", func(a *Asm, d Reg, f FReg) { a.FsubS(f, F6, F7, RNE) }, 0x087302d3},
	{"FmulS", func(a *Asm, d Reg, f FReg) { a.FmulS(f, F6, F7, RNE) }, 0x107302d3},
	{"FdivS", func(a *Asm, d Reg, f FReg) { a.FdivS(f, F6, F7, RNE) }, 0x187302d3},
	{"FsqrtS", func(a *Asm, d Reg, f FReg) { a.FsqrtS(f, F6, RNE) }, 0x580302d3},
	{"FsgnjS", func(a *Asm, d Reg, f FReg) { a.FsgnjS(f, F6, F7) }, 0x207302d3},
	{"FsgnjnS", func(a *Asm, d Reg, f FReg) { a.FsgnjnS(f, F6, F7) }, 0x207312d3},
	{"FsgnjxS", func(a *Asm, d Reg, f FReg) { a.FsgnjxS(f, F6, F7) }, 0x207322d3},
	{"FminS", func(a *Asm, d Reg, f FReg) { a.FminS(f, F6, F7) }, 0x287302d3},
	{"FmaxS", func(a *Asm, d Reg, f FReg) { a.FmaxS(f, F6, F7) }, 0x287312d3},
	{"FeqS", func(a *Asm, d Reg, f FReg) { a.FeqS(d, F6, F7) }, 0xa07322d3},
	{"FltS", func(a *Asm, d Reg, f FReg) { a.FltS(d, F6, F7) }, 0xa07312d3},
	{"FleS", func(a *Asm, d Reg, f FReg) { a.FleS(d, F6, F7) }, 0xa07302d3},
	{"FclassS", func(a *Asm, d Reg, f FReg) { a.FclassS(d, F6) }, 0xe00312d3},
	{"FcvtWS", func(a *Asm, d Reg, f FReg) { a.FcvtWS(d, F6, RTZ) }, 0xc00312d3},
	{"FcvtSW", func(a *Asm, d Reg, f FReg) { a.FcvtSW(f, X6, RNE) }, 0xd00302d3},
	{"FcvtWuS", func(a *Asm, d Reg, f FReg) { a.FcvtWuS(d, F6, RTZ) }, 0xc01312d3},
	{"FcvtSWu", func(a *Asm, d Reg, f FReg) { a.FcvtSWu(f, X6, RNE) }, 0xd01302d3},
	{"FcvtLS", func(a *Asm, d Reg, f FReg) { a.FcvtLS(d, F6, RTZ) }, 0xc02312d3},
	{"FcvtSL", func(a *Asm, d Reg, f FReg) { a.FcvtSL(f, X6, RNE) }, 0xd02302d3},
	{"FcvtLuS", func(a *Asm, d Reg, f FReg) { a.FcvtLuS(d, F6, RTZ) }, 0xc03312d3},
	{"FcvtSLu", func(a *Asm, d Reg, f FReg) { a.FcvtSLu(f, X6, RNE) }, 0xd03302d3},
	{"FaddD", func(a *Asm, d Reg, f FReg) { a.FaddD(f, F6, F7, RNE) }, 0x027302d3},
	{"FsubD", func(a *Asm, d Reg, f FReg) { a.FsubD(f, F6, F7, RNE) }, 0x0a7302d3},
	{"FmulD", func(a *Asm, d Reg, f FReg) { a.FmulD(f, F6, F7, RNE) }, 0x127302d3},
	{"FdivD", func(a *Asm, d Reg, f FReg) { a.FdivD(f, F6, F7, RNE) }, 0x1a7302d3},
	{"FsqrtD", func(a *Asm, d Reg, f FReg) { a.FsqrtD(f, F6, RNE) }, 0x5a0302d3},
	{"FsgnjD", func(a *Asm, d Reg, f FReg) { a.FsgnjD(f, F6, F7) }, 0x227302d3},
	{"FsgnjnD", func(a *Asm, d Reg, f FReg) { a.FsgnjnD(f, F6, F7) }, 0x227312d3},
	{"FsgnjxD", func(a *Asm, d Reg, f FReg) { a.FsgnjxD(f, F6, F7) }, 0x227322d3},
	{"FminD", func(a *Asm, d Reg, f FReg) { a.FminD(f, F6, F7) }, 0x2a7302d3},
	{"FmaxD", func(a *Asm, d Reg, f FReg) { a.FmaxD(f, F6, F7) }, 0x2a7312d3},
	{"FeqD", func(a *Asm, d Reg, f FReg) { a.FeqD(d, F6, F7) }, 0xa27322d3},
	{"FltD", func(a *Asm, d Reg, f FReg) { a.FltD(d, F6, F7) }, 0xa27312d3},
	{"FleD", func(a *Asm, d Reg, f FReg) { a.FleD(d, F6, F7) }, 0xa27302d3},
	{"FclassD", func(a *Asm, d Reg, f FReg) { a.FclassD(d, F6) }, 0xe20312d3},
	{"FcvtWD", func(a *Asm, d Reg, f FReg) { a.FcvtWD(d, F6, RTZ) }, 0xc20312d3},
	{"FcvtDW", func(a *Asm, d Reg, f FReg) { a.FcvtDW(f, X6, RNE) }, 0xd20302d3},
	{"FcvtWuD", func(a *Asm, d Reg, f FReg) { a.FcvtWuD(d, F6, RTZ) }, 0xc21312d3},
	{"FcvtDWu", func(a *Asm, d Reg, f FReg) { a.FcvtDWu(f, X6, RNE) }, 0xd21302d3},
	{"FcvtLD", func(a *Asm, d Reg, f FReg) { a.FcvtLD(d, F6, RTZ) }, 0xc22312d3},
	{"FcvtDL", func(a *Asm, d Reg, f FReg) { a.FcvtDL(f, X6, RNE) }, 0xd22302d3},
	{"FcvtLuD", func(a *Asm, d Reg, f FReg) { a.FcvtLuD(d, F6, RTZ) }, 0xc23312d3},
	{"FcvtDLu", func(a *Asm, d Reg, f FReg) { a.FcvtDLu(f, X6, RNE) }, 0xd23302d3},
	{"FmvXW", func(a *Asm, d Reg, f FReg) { a.FmvXW(d, F6) }, 0xe00302d3},
	{"FmvWX", func(a *Asm, d Reg, f FReg) { a.FmvWX(f, X6) }, 0xf00302d3},
	{"FmvXD", func(a *Asm, d Reg, f FReg) { a.FmvXD(d, F6) }, 0xe20302d3},
	{"FmvDX", func(a *Asm, d Reg, f FReg) { a.FmvDX(f, X6) }, 0xf20302d3},
	{"FcvtSD", func(a *Asm, d Reg, f FReg) { a.FcvtSD(f, F6, RNE) }, 0x401302d3},
	{"FcvtDS", func(a *Asm, d Reg, f FReg) { a.FcvtDS(f, F6, RNE) }, 0x420302d3},
	{"ReadFcsr", func(a *Asm, d Reg, f FReg) { a.ReadFcsr(d) }, 0x003022f3},
	{"WriteFcsr", func(a *Asm, d Reg, f FReg) { a.WriteFcsr(d) }, 0x00329073},
	{"ReadFflags", func(a *Asm, d Reg, f FReg) { a.ReadFflags(d) }, 0x001022f3},
	{"WriteFflags", func(a *Asm, d Reg, f FReg) { a.WriteFflags(d) }, 0x00129073},
}

func TestFloatEncodings(t *testing.T) {
	for _, c := range floatEncodingCases {
		t.Run(c.name, func(t *testing.T) {
			var a Asm
			c.emit(&a, X5, F5)
			if a.Err() != nil || len(a.B) != 4 {
				t.Fatalf("bytes=%x err=%v", a.B, a.Err())
			}
			if got := binary.LittleEndian.Uint32(a.B); got != c.want {
				t.Fatalf("got %#08x want %#08x", got, c.want)
			}
		})
	}
}

func TestFloatAssembler(t *testing.T) {
	want := assembledBytes(t, "float")
	var a Asm
	for _, c := range floatEncodingCases {
		c.emit(&a, X5, F5)
	}
	if a.Err() != nil || !bytes.Equal(a.B, want) {
		t.Fatalf("assembler comparison: err=%v\nencoder %x\nclang %x", a.Err(), a.B, want)
	}
}

func invalidFloatEmitters() []func(*Asm) {
	return []func(*Asm){
		func(a *Asm) { a.FaddS(32, F6, F7, RNE) },
		func(a *Asm) { a.FsubD(F5, 255, F7, RNE) },
		func(a *Asm) { a.FmulD(F5, F6, 32, RNE) },
		func(a *Asm) { a.FsqrtS(F5, F6, 5) },
		func(a *Asm) { a.FcvtSD(F5, F6, 6) },
		func(a *Asm) { a.FcvtDS(F5, F6, 255) },
		func(a *Asm) { a.FeqS(32, F6, F7) },
		func(a *Asm) { a.FclassD(X5, 32) },
		func(a *Asm) { a.FmvWX(F5, 32) },
		func(a *Asm) { a.FmvXD(32, F6) },
		func(a *Asm) { a.FcvtLuD(X5, F6, 5) },
		func(a *Asm) { a.FcvtSWu(32, X6, RNE) },
		func(a *Asm) { a.ReadFcsr(32) },
		func(a *Asm) { a.WriteFflags(255) },
	}
}

func TestFloatInvalidStickyAndAlignment(t *testing.T) {
	for _, bad := range invalidFloatEmitters() {
		var a Asm
		a.Nop()
		before := append([]byte(nil), a.B...)
		bad(&a)
		if a.Err() == nil || !bytes.Equal(a.B, before) {
			t.Fatalf("invalid float operand emitted %x err=%v", a.B, a.Err())
		}
		first := a.Err()
		for _, c := range floatEncodingCases {
			c.emit(&a, X5, F5)
		}
		if a.Err() != first || !bytes.Equal(a.B, before) {
			t.Fatal("float emission ignored sticky error")
		}
	}
	for _, c := range floatEncodingCases {
		a := Asm{B: []byte{1}}
		c.emit(&a, X5, F5)
		if a.Err() == nil || len(a.B) != 1 {
			t.Fatalf("%s ignored alignment", c.name)
		}
	}
}

func TestFloatRoundingModes(t *testing.T) {
	for mode := RoundingMode(0); mode < 8; mode++ {
		var a Asm
		a.FaddD(F31, F30, F29, mode)
		if mode == 5 || mode == 6 {
			if a.Err() == nil || len(a.B) != 0 {
				t.Fatalf("reserved mode %d admitted", mode)
			}
		} else if a.Err() != nil || len(a.B) != 4 || (binary.LittleEndian.Uint32(a.B)>>12)&7 != uint32(mode) {
			t.Fatalf("valid mode %d: bytes=%x err=%v", mode, a.B, a.Err())
		}
	}
}
