package arm64

import (
	"fmt"
	"testing"
)

// Decode the address instructions independently of the encoder helpers.
func displacementAddress(t *testing.T, a *Asm, base Reg) int64 {
	t.Helper()
	var regs [32]int64
	regs[base] = 0x100000000
	for pc := 0; pc < a.Len(); pc += 4 {
		w := a.wordAt(pc)
		rd, rn, rm := w&31, (w>>5)&31, (w>>16)&31
		switch {
		case w&0x1f000000 == 0x11000000:
			imm := int64((w >> 10) & 0xfff)
			if w&(1<<22) != 0 {
				imm <<= 12
			}
			if w&(1<<30) != 0 {
				imm = -imm
			}
			regs[rd] = regs[rn] + imm
		case w&0x1f000000 == 0x0b000000:
			v := regs[rm]
			if w&(1<<30) != 0 {
				v = -v
			}
			baseValue := regs[rn]
			if rn == 31 && w&(1<<21) == 0 {
				baseValue = 0
			} // Shifted form uses ZR, not SP.
			if rm == 31 {
				v = 0
			}
			regs[rd] = baseValue + v
		case w&0x7f800000 == 0x52800000:
			regs[rd] = int64((w>>5)&0xffff) << (((w >> 21) & 3) * 16)
		case w&0x7f800000 == 0x12800000:
			regs[rd] = ^(int64((w>>5)&0xffff) << (((w >> 21) & 3) * 16))
		case w&0x7f800000 == 0x72800000:
			sh := ((w >> 21) & 3) * 16
			regs[rd] = regs[rd]&^(0xffff<<sh) | int64((w>>5)&0xffff)<<sh
		case w&0x3f20fc00 == 0x3c206800:
			index := regs[rm]
			if rm == 31 {
				index = 0
			}
			return regs[rn] + index - 0x100000000
		case w&0x3f000000 == 0x3d000000:
			return regs[rn] + int64((w>>10)&0xfff)*16 - 0x100000000
		case w&0x3f200c00 == 0x3c000000:
			off := int64(int32(w<<11) >> 23)
			return regs[rn] + off - 0x100000000
		default:
			t.Fatalf("unexpected instruction %#08x at %d", w, pc)
		}
	}
	return regs[X16] - 0x100000000
}

func TestQDisplacementAddress(t *testing.T) {
	offsets := []int32{0, 16, 0xff0, 0xff8, 0x1000, 0x1ff0, 0x4000, 0x40f0, 0x40f8,
		0xfff0, 0x10000, 0x10008, 0xfff000, 0x1000000, 0x12345678, 0x7fffffff,
		-1, -256, -257, -4095, -4096, -0x40f8, -0xfff000, -0x12345678, -0x80000000}
	for _, base := range []Reg{SP, X4, X16, X17} {
		for _, disp := range offsets {
			for _, store := range []bool{false, true} {
				t.Run(fmt.Sprintf("base%d/%x/store%t", base, disp, store), func(t *testing.T) {
					a := Asm{DisableLogicalMoveImmediate: true}
					if store {
						a.VMovdquStoreDisp(base, disp, X17)
					} else {
						a.VMovdquLoadDisp(X16, base, disp)
					}
					if got := displacementAddress(t, &a, base); got != int64(disp) {
						t.Fatalf("address displacement = %#x, want %#x; code %x", got, disp, a.B)
					}
					if disp >= 0 && disp&15 == 0 && disp <= 0xfff0 && a.Len() != 4 {
						t.Fatalf("scaled Q access uses %d bytes, want 4", a.Len())
					}
				})
			}
		}
	}
}

func TestQLargeDisplacementBoundary(t *testing.T) {
	var a Asm
	a.StrQ(SP, 0x40f8, X17)
	if got := displacementAddress(t, &a, SP); got != 0x40f8 || got == 0xf8 {
		t.Fatalf("0x40f8 became %#x; code %x", got, a.B)
	}
}

func TestQLargeDisplacementSequence(t *testing.T) {
	var a Asm
	a.StrQ(SP, 0x40f8, X17)
	// MOVZ X16,#0x40f8; STR Q17,[SP,X16].
	want := [...]uint32{0xd2881f10, 0x3cb06bf1}
	if a.Len() != len(want)*4 {
		t.Fatalf("code length = %d", a.Len())
	}
	for i, w := range want {
		if got := a.wordAt(i * 4); got != w {
			t.Errorf("word %d = %#08x, want %#08x", i, got, w)
		}
	}
}

func TestIndexedMinimumDisplacement(t *testing.T) {
	a := Asm{DisableLogicalMoveImmediate: true}
	a.addDispX16(-0x80000000)
	if got := displacementAddress(t, &a, X16); got != -0x80000000 {
		t.Fatalf("indexed displacement = %#x, want -0x80000000", got)
	}
}

func TestLeaSPLargeDisplacement(t *testing.T) {
	for _, disp := range []int32{0xff8, 0x1000, 0x40f8, -0x40f8, -0x80000000, 0x7fffffff} {
		for _, dst := range []Reg{X0, X16, X17} {
			a := Asm{DisableLogicalMoveImmediate: true}
			a.LeaSP(dst, disp)
			a.LdrQ(X0, dst, 0)
			if got := displacementAddress(t, &a, SP); got != int64(disp) {
				t.Errorf("LeaSP(%d, %#x) = %#x", dst, disp, got)
			}
		}
	}
}

func TestQDisplacementCodeSize(t *testing.T) {
	for _, tc := range []struct {
		disp  int32
		bytes int
	}{
		{16, 4}, {0x40f0, 4}, {0x40f8, 8}, {0x10000, 8}, {0xff8, 8}, {-1, 4}, {-0x1000, 8},
	} {
		for _, store := range []bool{false, true} {
			var a Asm
			if store {
				a.StrQ(SP, tc.disp, X17)
			} else {
				a.LdrQ(X17, SP, tc.disp)
			}
			if a.Len() != tc.bytes {
				t.Errorf("offset %#x store=%t: %d bytes, want %d", tc.disp, store, a.Len(), tc.bytes)
			}
		}
	}
}

func TestQDisplacementNoAllocations(t *testing.T) {
	var buf [64]byte
	a := Asm{B: buf[:0]}
	if got := testing.AllocsPerRun(100, func() {
		a.B = a.B[:0]
		a.StrQ(SP, 0x40f8, X17)
		a.LdrQ(X17, X16, -0x12345678)
	}); got != 0 {
		t.Fatalf("address fallback allocated %g times", got)
	}
}

func TestIndexedDisplacementAddresses(t *testing.T) {
	for _, disp := range []int32{-0x80000000, -0x40f8, 0x40f8, 0x12345678} {
		for _, tc := range []struct {
			name string
			emit func(*Asm, int32)
		}{
			{"GP load", func(a *Asm, d int32) { a.LoadIdx(X4, X0, X1, d, 8, false, true) }},
			{"GP store", func(a *Asm, d int32) { a.StoreIdx(X0, X1, X4, d, 8) }},
			{"FP load", func(a *Asm, d int32) { a.LdrFIdx(X17, X0, X1, d, true) }},
			{"FP store", func(a *Asm, d int32) { a.StrFIdx(X0, X1, X17, d, true) }},
			{"Q load", func(a *Asm, d int32) { a.LdrQIdx(X17, X0, X1, d) }},
			{"Q store", func(a *Asm, d int32) { a.StrQIdx(X0, X1, X17, d) }},
		} {
			t.Run(fmt.Sprintf("%s/%x", tc.name, disp), func(t *testing.T) {
				a := Asm{DisableLogicalMoveImmediate: true}
				tc.emit(&a, disp)
				a.B = a.B[:len(a.B)-4] // The final register-offset access has zero index.
				if got := displacementAddress(t, &a, X0); got != int64(disp) {
					t.Fatalf("address = %#x, want %#x", got, disp)
				}
			})
		}
	}
}
