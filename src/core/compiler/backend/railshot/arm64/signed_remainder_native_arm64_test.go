//go:build (linux || darwin) && arm64

package arm64

import (
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
	"testing"
)

// Execute raw instructions independently of guest lowering. SDIV truncates
// its overflow result; MSUB still produces zero for signed minimum % -1.
func TestRawSignedRemainderNative(t *testing.T) {
	for _, wide := range []bool{false, true} {
		var a enc.Asm
		if wide {
			a.Sdiv64(enc.X2, enc.X0, enc.X1)
			a.Msub64(enc.X0, enc.X2, enc.X1, enc.X0)
		} else {
			a.Sdiv32(enc.X2, enc.X0, enc.X1)
			a.Msub32(enc.X0, enc.X2, enc.X1, enc.X0)
		}
		a.Ret()
		code, entry, err := rt.MapCode(a.B)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []uint64{0, 1, 17, ^uint64(0), uint64(1) << 31, (uint64(1) << 31) - 1, uint64(1) << 63, (uint64(1) << 63) - 1} {
			for _, d := range []uint64{1, 2, 3, 7, ^uint64(0), ^uint64(1), uint64(1) << 31, uint64(1) << 63} {
				var want uint64
				if wide {
					want = uint64(int64(n) % int64(d))
				} else {
					if uint32(d) == 0 {
						continue
					}
					want = uint64(uint32(int32(n) % int32(d)))
				}
				got := uint64(arm64spike.Call2(entry, uintptr(n), uintptr(d)))
				if !wide {
					got = uint64(uint32(got))
				}
				if got != want {
					rt.Unmap(code)
					t.Fatalf("wide=%v n=%x d=%x got=%x want=%x", wide, n, d, got, want)
				}
			}
		}
		rt.Unmap(code)
	}
}
