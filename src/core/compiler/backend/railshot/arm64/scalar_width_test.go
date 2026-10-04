//go:build arm64 && !tinygo

package arm64

import (
	"encoding/binary"
	"testing"

	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestSharedScalarScaledAddEncodingWidth(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for shift := uint8(0); shift <= 3; shift++ {
			f := fn{a: &encoder.Asm{}}
			if !f.ScaledAdd(wide, uint8(X0), uint8(X1), uint8(X2), shift) {
				t.Fatal("shifted addition rejected")
			}
			if len(f.a.B) != 4 {
				t.Fatalf("got %d bytes, want one instruction", len(f.a.B))
			}
			word := binary.LittleEndian.Uint32(f.a.B)
			if (word>>31 != 0) != wide {
				t.Fatalf("wide=%v shift=%d: instruction %08x has wrong sf bit", wide, shift, word)
			}
		}
	}
}
