//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math"
	"testing"
)

func TestFloatParityPendingLoads(t *testing.T) {
	for _, wide := range []bool{false, true} {
		ft, load, align, compare := wasm.F32, byte(0x2a), byte(2), byte(0x5b)
		if wide {
			ft, load, align, compare = wasm.F64, 0x2b, 3, 0x61
		}
		for _, ne := range []bool{false, true} {
			body := []byte{1, 1, 0x7f}
			for i := byte(1); i <= 40; i++ {
				body = append(body, 0x20, 2, 0x41, i, 0x6c, load, align, 0)
			}
			body = append(body, 0x20, 0, 0x20, 1, compare)
			if ne {
				body[len(body)-1]++
			}
			body = append(body, 0x21, 3)
			for range 40 {
				body = append(body, 0x1a)
			}
			body = append(body, 0x20, 3, 0x0b)
			m := modMem(t, 1, []wasm.ValType{ft, ft, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			cm, err := CompileModuleWith(m, CompileOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			vals := []uint64{0, 0x80000000, 0x7f812345, 0x7fc54321, 0x3f800000}
			if wide {
				vals = []uint64{0, 0x8000000000000000, 0x7ff0000000001234, 0x7ff8000000004321, 0x3ff0000000000000}
			}
			for _, a := range vals {
				for _, b := range vals {
					x, y := math.Float64frombits(a), math.Float64frombits(b)
					if !wide {
						x, y = float64(math.Float32frombits(uint32(a))), float64(math.Float32frombits(uint32(b)))
					}
					active := x == y
					if ne {
						active = !active
					}
					want := uint32(0)
					if active {
						want = 1
					}
					got := uint32(runCompiledAmd64u(t, cm, a, b, 4))
					if got != want {
						t.Fatalf("wide=%v ne=%v a=%x b=%x got=%d want=%d", wide, ne, a, b, got, want)
					}
				}
			}
		}
	}
}
