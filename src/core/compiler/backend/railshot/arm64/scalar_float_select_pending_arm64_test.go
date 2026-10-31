//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestScalarFloatSelectPendingLoads(t *testing.T) {
	for _, wide := range []bool{false, true} {
		ft, it, local, load, align, reinterpret, xor := wasm.F32, wasm.I32, byte(0x7d), byte(0x2a), byte(2), byte(0xbc), byte(0x73)
		if wide {
			ft, it, local, load, align, reinterpret, xor = wasm.F64, wasm.I64, 0x7c, 0x2b, 3, 0xbd, 0x85
		}
		body := []byte{1, 1, local}
		for i := byte(1); i <= 40; i++ {
			body = append(body, 0x20, 2, 0x41, i, 0x6c, load, align, 0)
		}
		body = append(body, 0x20, 0, 0x20, 1, 0x20, 2, 0x45, 0x1b, 0x21, 3)
		for range 40 {
			body = append(body, 0x1a)
		}
		// Observe both borrowed locals after selection as well as the selected bits.
		body = append(body, 0x20, 3, reinterpret, 0x20, 0, reinterpret, xor, 0x20, 1, reinterpret, xor, 0x0b)
		m := modMem(t, 1, []wasm.ValType{ft, ft, wasm.I32}, []wasm.ValType{it}, body)
		a, b := uint64(0x7f812345), uint64(0x80000000)
		if wide {
			a, b = 0x7ff0000000001234, 0x8000000000000000
		}
		for _, base := range []uint64{0, 4} {
			want := a
			if base == 0 {
				want = b
			}
			got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, a, b, base)
			if !wide {
				got = uint64(uint32(got))
			}
			if err != nil || got != want {
				t.Fatalf("wide=%v base=%d got=%x want=%x err=%v", wide, base, got, want, err)
			}
		}
	}
}
