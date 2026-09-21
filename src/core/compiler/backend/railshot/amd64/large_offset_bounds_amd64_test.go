//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestLargeOffsetBoundsCertificate(t *testing.T) {
	for _, off := range []uint32{0x7fffffff, 0x80000000, 0xffffffff} {
		t.Run(fmt.Sprint(off), func(t *testing.T) {
			body := []byte{0, 0x20, 0, 0x2d, 0, 0, 0x20, 0, 0x2d, 0}
			body = append(body, wasmtest.ULEB(off)...)
			body = append(body, 0x0b)
			s := compileWithStats(t, modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}, body), false).Funcs[0]
			if s.BoundsChecks != 2 || s.BoundsChecksElidable != 0 {
				t.Fatalf("bounds=%d elidable=%d, want 2/0", s.BoundsChecks, s.BoundsChecksElidable)
			}
		})
	}
}
