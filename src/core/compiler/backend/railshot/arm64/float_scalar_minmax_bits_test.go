//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestFloatScalarMinMaxBits(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, first := wasm.F32, byte(0x96)
		values := []uint64{0, 0x80000000, 1, 0x80000001, 0x3f800000, 0xbf800000, 0x7f800000, 0xff800000, 0x7fc12345, 0x7f812345, 0xff812345}
		sign, exp, frac, quiet := uint64(0x80000000), uint64(0x7f800000), uint64(0x007fffff), uint64(0x00400000)
		if wide {
			typ, first = wasm.F64, 0xa4
			values = []uint64{0, 0x8000000000000000, 1, 0x8000000000000001, 0x3ff0000000000000, 0xbff0000000000000, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000001234, 0x7ff0000000001234, 0xfff0000000001234}
			sign, exp, frac, quiet = 1<<63, 0x7ff0000000000000, 0x000fffffffffffff, 1<<51
		}
		for _, max := range []bool{false, true} {
			op := first
			if max {
				op++
			}
			for _, sink := range []byte{0xff, 0, 1} {
				body := []byte{0, 0x20, 0, 0x20, 1, op}
				if sink != 0xff {
					body = append(body, 0x21, sink, 0x20, sink)
				}
				body = append(body, 0x0b)
				m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
				for _, a := range values {
					for _, b := range values {
						got, err := runArm64Wrapper(t, m, a, b)
						if err != nil {
							t.Fatal(err)
						}
						nan := func(x uint64) bool { return x&exp == exp && x&frac != 0 }
						if nan(a) || nan(b) {
							// Wasm permits any arithmetic NaN, but signaling inputs must be quieted.
							if !nan(got) || got&quiet == 0 {
								t.Fatalf("wide=%v max=%v sink=%d a=%x b=%x got=%x: not a quiet NaN", wide, max, sink, a, b, got)
							}
							continue
						}
						x, y := math.Float64frombits(a), math.Float64frombits(b)
						if !wide {
							x, y = float64(math.Float32frombits(uint32(a))), float64(math.Float32frombits(uint32(b)))
						}
						want := a
						if (max && y > x) || (!max && y < x) {
							want = b
						}
						if a&^sign == 0 && b&^sign == 0 {
							if max {
								want = a & b
							} else {
								want = a | b
							}
						}
						if got != want {
							t.Fatalf("wide=%v max=%v sink=%d a=%x b=%x got=%x want=%x", wide, max, sink, a, b, got, want)
						}
					}
				}
			}
		}
	}
}
