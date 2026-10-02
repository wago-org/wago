//go:build amd64 && wago_regalloccheck

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestRegallocCheckSynchronousHostCallClobber(t *testing.T) {
	for _, tc := range []struct {
		name    string
		preload bool
	}{{"without-cache", false}, {"preloaded-float", true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := fn{a: &encoder.Asm{}, s: newStack(), globalCellReg: regNone, memSizeReg: regNone}
			if tc.preload {
				if _, ok := f.preloadFloatConst(storage{kind: stConst, typ: mtF64, cval: 0x3ff0000000000000}); !ok {
					t.Fatal("immutable float cache was not preloaded")
				}
			}
			// There is no native allocation stub here. The unconditional host
			// trampoline must check a mistakenly admitted call-free cache itself.
			emit := func() {
				if err := f.callHostSync(0, &wasm.CompType{}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.preload {
				requireAllocationFailure(t, "immutable cache across physical call", emit)
			} else {
				emit()
			}
		})
	}
}
