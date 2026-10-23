//go:build arm64

package arm64

import "testing"

func TestBranchTargetSignedDisplacements(t *testing.T) {
	forms := []struct {
		name  string
		base  uint32
		bits  uint
		shift uint
	}{
		{"b", 0x14000000, 26, 0},
		{"bl", 0x94000000, 26, 0},
		{"b.cond", 0x5400000d, 19, 5},
		{"cbz-w", 0x3400001f, 19, 5},
		{"cbnz-x", 0xb500001f, 19, 5},
		{"tbz-w", 0x3600001f, 14, 5},
		{"tbnz-x", 0xb700001f, 14, 5},
	}
	const pc = 0x12340
	for _, form := range forms {
		t.Run(form.name, func(t *testing.T) {
			limit := 1 << (form.bits - 1)
			for _, delta := range []int{-limit, -limit + 1, -1, 0, 1, limit - 2, limit - 1} {
				word := form.base | (uint32(delta)&((1<<form.bits)-1))<<form.shift
				if target, ok := branchTarget(pc, word); !ok || target != pc+delta*4 {
					t.Fatalf("word %#08x: target %d/%t, want %d", word, target, ok, pc+delta*4)
				}
				if form.bits == 19 && imm19(word) != delta {
					t.Fatalf("word %#08x: imm19 %d, want %d", word, imm19(word), delta)
				}
				if form.bits == 26 {
					offset, ok := uncondBranchImm(word)
					if form.name == "b" && (!ok || offset != delta*4) || form.name == "bl" && ok {
						t.Fatalf("word %#08x: plain B offset %d/%t", word, offset, ok)
					}
				}
			}
		})
	}
	for _, word := range []uint32{0xd503201f, 0xd65f03c0, 0xd61f0000, 0xf9400000, 0xb9000000, 0xeb00001f} {
		if target, ok := branchTarget(pc, word); ok {
			t.Fatalf("non-direct-branch %#08x decoded as target %d", word, target)
		}
	}
}
