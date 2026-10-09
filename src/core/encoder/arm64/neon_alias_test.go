package arm64

import (
	"bytes"
	"fmt"
	"testing"
)

func requireNeonAliasPanic(t *testing.T, emit func(*Asm), want string) {
	t.Helper()
	var a Asm
	a.Nop()
	before := append([]byte(nil), a.B...)
	defer func() {
		if got := recover(); got != want {
			t.Errorf("panic = %v, want %q", got, want)
		}
		if !bytes.Equal(a.B, before) {
			t.Error("rejected operand emitted instructions")
		}
	}()
	emit(&a)
}

func TestNeonPshufSEncodedOverlap(t *testing.T) {
	for _, physical := range []int{0, 2, 31} {
		for dstBand := 0; dstBand < 8; dstBand++ {
			for srcBand := 0; srcBand < 8; srcBand++ {
				dst, src := Reg(physical+32*dstBand), Reg(physical+32*srcBand)
				t.Run(fmt.Sprintf("dst=%d/src=%d", dst, src), func(t *testing.T) {
					requireNeonAliasPanic(t, func(a *Asm) { a.NeonPshufS(dst, src, 0x1b) }, "NeonPshufS needs a separate source; use NeonPshufSWithScratch")
				})
			}
		}
	}
}

func TestNeonPshufSScratchEncodedOverlap(t *testing.T) {
	for operandBand := 0; operandBand < 8; operandBand++ {
		for scratchBand := 0; scratchBand < 8; scratchBand++ {
			for _, operand := range []Reg{2, 3} {
				dst, src := Reg(2+32*operandBand), Reg(3+32*operandBand)
				scratch := operand + Reg(32*scratchBand)
				t.Run(fmt.Sprintf("dst=%d/src=%d/scratch=%d", dst, src, scratch), func(t *testing.T) {
					requireNeonAliasPanic(t, func(a *Asm) { a.NeonPshufSWithScratch(dst, src, scratch, 0x1b) }, "NeonPshufS scratch overlaps an operand")
				})
			}
		}
	}
}

func TestNeonPshufSEncodedAliases(t *testing.T) {
	for band := 0; band < 8; band++ {
		t.Run(fmt.Sprintf("band=%d", band), func(t *testing.T) {
			for control := 0; control < 256; control++ {
				imm := byte(control)
				var plain, aliased, inPlace, aliasedInPlace Asm
				plain.NeonPshufS(3, 2, imm)
				aliased.NeonPshufS(Reg(3+32*band), Reg(2+32*band), imm)
				inPlace.NeonPshufSWithScratch(2, 2, 3, imm)
				aliasedInPlace.NeonPshufSWithScratch(Reg(2+32*band), 2, Reg(3+32*band), imm)
				if !bytes.Equal(plain.B, aliased.B) || !bytes.Equal(inPlace.B, aliasedInPlace.B) {
					t.Fatalf("control=%#x: aliases changed canonical shuffle encoding", imm)
				}
			}
			for _, imm := range []byte{0, 0x55, 0xaa, 0xff, 0xe4} {
				var plain, aliased Asm
				plain.NeonPshufS(2, 2, imm)
				aliased.NeonPshufS(Reg(2+32*band), 2, imm)
				if !bytes.Equal(plain.B, aliased.B) {
					t.Fatalf("control=%#x: aliases changed safe in-place encoding", imm)
				}
			}
		})
	}
}

func TestNeonMovemaskBEncodedScratch(t *testing.T) {
	for band := 0; band < 8; band++ {
		for _, physical := range []Reg{X16, X17} {
			dst := physical + Reg(32*band)
			t.Run(fmt.Sprintf("dst=%d", dst), func(t *testing.T) {
				requireNeonAliasPanic(t, func(a *Asm) { a.NeonMovemaskB(dst, 2) }, "NeonMovemaskB destination overlaps scratch")
			})
		}
	}
}

func TestNeonMovemaskBEncodedAliases(t *testing.T) {
	for band := 0; band < 8; band++ {
		for physical := 0; physical < 32; physical++ {
			if physical == 16 || physical == 17 {
				continue
			}
			// V16/V17 are a different register bank from the GP scratches.
			for _, vector := range []int{2, 16, 17, 31} {
				var plain, aliased Asm
				plain.NeonMovemaskB(Reg(physical), Reg(vector))
				aliased.NeonMovemaskB(Reg(physical+32*band), Reg(vector+32*band))
				if !bytes.Equal(plain.B, aliased.B) {
					t.Fatalf("dst=%d/src=%d/band=%d: aliases changed movemask encoding", physical, vector, band)
				}
			}
		}
	}
}
