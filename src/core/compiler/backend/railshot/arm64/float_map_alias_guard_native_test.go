//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

func TestFloatMapAliasGuardPreservesStreamDistance(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, distance := range []uint32{0, 1, 4095, 4096, 4097, 65536, 262144, 0xffffff, 0x1000000, 0x80000000, 0xffffffff} {
			f := gpClassEmitter()
			f.policy.CompactNative = compact
			f.compareFloatMapSpan(X0, distance)
			f.a.Cset32(X0, condBE)
			f.a.Ret()
			code, entry, err := rt.MapCode(f.a.B)
			if err != nil {
				t.Fatal(err)
			}
			for _, span := range []uint32{0, 1, distance - 1, distance, distance + 1, 0x7fffffff, 0xffffffff} {
				want := uintptr(0)
				if span <= distance {
					want = 1
				}
				got := arm64spike.Call2(entry, uintptr(span), 0)
				if got != want {
					rt.Unmap(code)
					t.Fatalf("compact=%v span=%x distance=%x: got %d, want %d", compact, span, distance, got, want)
				}
			}
			rt.Unmap(code)
		}
	}
}
