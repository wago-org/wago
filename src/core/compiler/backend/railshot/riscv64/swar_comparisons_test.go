//go:build linux && !tinygo

package riscv64

import (
	"testing"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func TestSWARIntegerVectorComparisonAdmission(t *testing.T) {
	for _, interval := range [][2]uint32{{35, 64}, {214, 219}} {
		for kind := interval[0]; kind <= interval[1]; kind++ {
			fn, ft := swarValidatedFunction(t, swarArithmeticFixture(kind))
			if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
				t.Fatal("valid integer vector comparison", kind, err)
			}
			for _, body := range [][]byte{
				uleb([]byte{0xfd}, kind),
				append(uleb([]byte{0x20, 0, 0xfd}, kind), 0x0b),
				append(uleb([]byte{0x20, 0, 0x41, 0, 0xfd}, kind), 0x0b),
				append(uleb([]byte{0x41, 0, 0x20, 1, 0xfd}, kind), 0x0b),
			} {
				bad := *fn
				bad.BodyBytes = body
				if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
					t.Fatal("invalid comparison carriers emitted", kind, err)
				}
			}
		}
	}
	for _, kind := range []uint32{256} {
		fn, ft := swarValidatedFunction(t, swarArithmeticFixture(kind))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("pending vector floating min/max emitted", kind, err)
		}
	}
}

func TestSWARIntegerComparisonHelpersRejectBeforeBytes(t *testing.T) {
	for _, test := range []struct {
		width    uint8
		relation swarComparison
	}{{0, swarEQ}, {24, swarEQ}, {65, swarEQ}, {255, swarEQ}, {8, 10}, {32, 255},
		{64, swarLTU}, {64, swarGTU}, {64, swarLEU}, {64, swarGEU}} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 1)
		before := len(f.a.B)
		f.swarCompare(test.width, test.relation)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid comparison partially emitted", test, f.err)
		}
		f.swarCompare(8, swarEQ)
		if len(f.a.B) != before {
			t.Fatal("sticky comparison error appended bytes")
		}
	}
}
