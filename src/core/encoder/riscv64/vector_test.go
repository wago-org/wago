package riscv64

import "testing"

func TestVectorEncodingGoldens(t *testing.T) {
	tests := []struct {
		name string
		emit func(*Asm)
		want uint32
	}{
		{"vsetivli-e8", func(a *Asm) { a.Vsetivli(Zero, 16, VE8) }, 0xcc087057},
		{"vsetivli-e64", func(a *Asm) { a.Vsetivli(T0, 2, VE64) }, 0xcd8172d7},
		{"vle8", func(a *Asm) { a.Vle(1, A0, VE8) }, 0x02050087},
		{"vse8", func(a *Asm) { a.Vse(2, A1, VE8) }, 0x02058127},
		{"vadd-vv", func(a *Asm) { a.VaddVV(1, 2, 3) }, 0x022180d7},
		{"vsub-vv", func(a *Asm) { a.VsubVV(4, 5, 6) }, 0x0a530257},
		{"vmv-vv", func(a *Asm) { a.VmvVV(7, 8) }, 0x5e0403d7},
		{"vfadd-vv", func(a *Asm) { a.VfaddVV(1, 2, 3) }, 0x022190d7},
		{"vfsqrt-v", func(a *Asm) { a.VfsqrtV(1, 2) }, 0x4e2010d7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.emit(&a)
			if got := word(&a); got != tc.want {
				t.Fatalf("got %#08x, want %#08x", got, tc.want)
			}
		})
	}
}

func TestVectorEncodingRejectsInvalidConfiguration(t *testing.T) {
	var a Asm
	if a.Vsetivli(Zero, 32, VE8) || len(a.B) != 0 {
		t.Fatal("Vsetivli accepted AVL 32")
	}
	if a.Vsetivli(Zero, 16, VSEW(4)) || len(a.B) != 0 {
		t.Fatal("Vsetivli accepted invalid SEW")
	}
	if a.Vle(1, A0, VSEW(4)) || a.Vse(1, A0, VSEW(4)) || len(a.B) != 0 {
		t.Fatal("vector memory operation accepted invalid SEW")
	}
}
