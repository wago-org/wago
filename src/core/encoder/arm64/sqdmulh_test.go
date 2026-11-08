package arm64

import (
	"encoding/binary"
	"testing"
)

func TestNeonSqdmulhSEncoding(t *testing.T) {
	var a Asm
	a.NeonSqdmulhS(X0, X1, X2)
	if len(a.B) != 4 || binary.LittleEndian.Uint32(a.B) != 0x4ea2b420 {
		t.Fatalf("SQDMULH.4S encoding %x", a.B)
	}
}
