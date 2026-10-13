package arm64

import (
	"encoding/binary"
	"testing"
)

func TestFcselMatchesAssemblerEncoding(t *testing.T) {
	for _, test := range []struct {
		wide bool
		word uint32
	}{{false, 0x1e271ca3}, {true, 0x1e671ca3}} {
		a := &Asm{}
		a.Fcsel(Reg(3), Reg(5), Reg(7), CondNE, test.wide)
		if len(a.B) != 4 || binary.LittleEndian.Uint32(a.B) != test.word {
			t.Fatalf("wide=%t bytes=%x want=%x", test.wide, a.B, test.word)
		}
	}
}
