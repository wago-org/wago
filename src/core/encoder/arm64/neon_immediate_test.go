package arm64

import (
	"encoding/binary"
	"testing"
)

func TestNeonIntegerImmediateAssemblerOracle(t *testing.T) {
	// clang -c -target arm64-apple-darwin, independently disassembled:
	// movi/mvni v0/v1/v7/v16/v31.4s, #0/#1/#127/#128/#255.
	registers := []Reg{0, 1, 7, 16, 31}
	immediates := []uint8{0, 1, 127, 128, 255}
	want := []uint32{0x4f000400, 0x4f000421, 0x4f0307e7, 0x4f040410, 0x4f0707ff,
		0x6f000400, 0x6f000421, 0x6f0307e7, 0x6f040410, 0x6f0707ff}
	var a Asm
	for i, reg := range registers {
		a.NeonMoviS(reg, immediates[i])
	}
	for i, reg := range registers {
		a.NeonMvniS(reg, immediates[i])
	}
	if len(a.B) != len(want)*4 {
		t.Fatalf("length=%d", len(a.B))
	}
	for i, word := range want {
		if got := binary.LittleEndian.Uint32(a.B[4*i:]); got != word {
			t.Fatalf("word%d=%08x want=%08x", i, got, word)
		}
	}
}
