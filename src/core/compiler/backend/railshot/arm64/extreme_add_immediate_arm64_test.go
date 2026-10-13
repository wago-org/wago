//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestI64MinimumArithmeticConstant(t *testing.T) {
	for _, op := range []byte{0x7c, 0x7d} {
		body := []byte{0, 0x20, 0, 0x42}
		body = append(body, wasmtest.SLEB64(-1<<63)...)
		body = append(body, op, 0x0b)
		m := mod1(t, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, body)
		for _, guard := range []bool{false, true} {
			for _, x := range []uint64{0, 1, ^uint64(0), 1 << 63} {
				opts := CompileOptions{ElideBoundsChecks: guard}
				got, err := runArm64WrapperWithOptions(t, m, opts, x)
				want := x ^ (uint64(1) << 63)
				if err != nil || got != want {
					t.Fatalf("op=%x guard=%v x=%x got=%x want=%x err=%v", op, guard, x, got, want, err)
				}
			}
		}
	}
}
