package arm64

import (
	"encoding/binary"
	"testing"
)

func TestNeonMlaSAssemblerOracle(t *testing.T) {
	var a Asm
	a.NeonMlaS(Reg(0), Reg(1), Reg(2))
	a.NeonMlaS(Reg(31), Reg(30), Reg(29))
	// Independently assembled with clang -target arm64-apple-darwin:
	// mla v0.4s, v1.4s, v2.4s; mla v31.4s, v30.4s, v29.4s.
	want := []uint32{0x4ea29420, 0x4ebd97df}
	if len(a.B) != 4*len(want) {
		t.Fatalf("length=%d", len(a.B))
	}
	for i, word := range want {
		if got := binary.LittleEndian.Uint32(a.B[4*i:]); got != word {
			t.Fatalf("word%d=%08x want=%08x", i, got, word)
		}
	}
}
