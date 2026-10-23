package arm64

import (
	"bytes"
	"fmt"
	"testing"
)

func TestReservedBranch19Expansion(t *testing.T) {
	emitters := []struct {
		name   string
		emit   func(*Asm) int
		invert uint32
	}{
		{"cbz32", func(a *Asm) int { return a.Cbz32(X7) }, 1 << 24},
		{"cbnz32", func(a *Asm) int { return a.Cbnz32(X7) }, 1 << 24},
		{"cbz64", func(a *Asm) int { return a.Cbz64(X7) }, 1 << 24},
		{"cbnz64", func(a *Asm) int { return a.Cbnz64(X7) }, 1 << 24},
	}
	for cc := CondEQ; cc <= CondLE; cc++ {
		cc := cc
		emitters = append(emitters, struct {
			name   string
			emit   func(*Asm) int
			invert uint32
		}{fmt.Sprintf("cond%d", cc), func(a *Asm) int { return a.Bcond(cc) }, 1})
	}
	for _, emit := range emitters {
		for _, delta := range []int{-(1 << 27), -(1 << 27) + 4, -(1 << 20) - 4, -(1 << 20), (1 << 20) - 4, 1 << 20, 1 << 27, (1 << 27) + 4} {
			t.Run(fmt.Sprintf("%s/%d", emit.name, delta), func(t *testing.T) {
				a := &Asm{ReserveBranch19: true}
				a.Nop() // Test a nonzero instruction site.
				site := emit.emit(a)
				original := a.wordAt(site)
				before := append([]byte(nil), a.B...)
				wantOK := delta >= -(1<<27)+4 && delta < (1<<27)+4
				if got := a.PatchBranch19(site, site+delta); got != wantOK {
					t.Fatalf("accepted=%v, want %v", got, wantOK)
				}
				if !wantOK {
					if !bytes.Equal(before, a.B) {
						t.Fatal("failed patch changed code")
					}
					return
				}
				word := a.wordAt(site)
				if delta >= -(1<<20) && delta < 1<<20 {
					if word&^(0x7ffff<<5) != original || int(int32(word<<8)>>13)*4 != delta || a.wordAt(site+4) != 0xD503201F {
						t.Fatalf("bad short branch %08x", word)
					}
				} else {
					if word != (original^emit.invert)|2<<5 {
						t.Fatalf("inverted branch=%08x", word)
					}
					jump := a.wordAt(site + 4)
					target := site + 4 + int(int32(jump<<6)>>6)*4
					if jump&0xfc000000 != 0x14000000 || target != site+delta {
						t.Fatalf("long target=%d, want %d", target, site+delta)
					}
				}
			})
		}
	}
}

func TestReservedAlwaysBranch(t *testing.T) {
	for _, cc := range []Cond{CondAL, CondNV} {
		a := &Asm{ReserveBranch19: true}
		site := a.Bcond(cc)
		if !a.PatchBranch19(site, 1<<20) || a.wordAt(site) != 0x14040000 || a.wordAt(site+4) != 0xD503201F {
			t.Fatalf("always branch %d: %x", cc, a.B)
		}
	}
}

func TestBranch19ExpansionRequiresReservedNOP(t *testing.T) {
	for _, reserve := range []bool{false, true} {
		a := &Asm{ReserveBranch19: reserve}
		site := a.Cbz64(X0)
		if reserve {
			a.putWord(site+4, 0xd65f03c0)
		} else {
			a.Nop()
		}
		before := append([]byte(nil), a.B...)
		if a.PatchBranch19(site, 1<<20) || !bytes.Equal(before, a.B) {
			t.Fatal("expanded without a reserved NOP")
		}
	}
}
