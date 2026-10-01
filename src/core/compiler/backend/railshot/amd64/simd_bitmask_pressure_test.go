//go:build linux && amd64 && !tinygo

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestI16x8BitmaskRegisterPressure(t *testing.T) {
	for _, profile := range []struct {
		name     string
		features shared.AMD64Features
	}{
		{"sse2", 0},
		{"modern", shared.AMD64ModernBaseline},
	} {
		t.Run(profile.name, func(t *testing.T) {
			body := []byte{1, 1, 0x7f} // one i32 local
			for j := 0; j < 13; j++ {
				body = append(body, 0x20, 1, 0x20, 1, 0xa0) // keep f64.add results live
			}
			for j := 0; j < 13; j++ {
				body = append(body, 0x20, 0, 0x41, byte(j), 0x6a, 0x2b, 0, 0) // deferred f64.load
			}
			body = append(body, v128ConstBytes(i16x8Bytes(-1, -2, -3, -4, -5, -6, -7, -8))...)
			body = append(body, simdOp(132)...)
			body = append(body, 0x21, 2) // local.set 2
			for j := 0; j < 26; j++ {
				body = append(body, 0x1a)
			}
			body = append(body, 0x20, 2, 0x0b)
			m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.F64}, []wasm.ValType{wasm.I32}, body)
			// All accesses are in bounds. Eliding checks retains deferred loads,
			// whose eviction needs XMM scratch while the packed mask is live.
			opts := CompileOptions{ElideBoundsChecks: true, AMD64FeaturesSet: true, AMD64Features: profile.features}
			got, _, err := runMemAmd64WithOptions(t, m, opts, nil, 0, f64b(3))
			if err != nil || got != 0xff {
				t.Fatalf("result = %#x, %v; want 0xff", got, err)
			}
		})
	}
}

func TestI16x8BitmaskPreservesLocal(t *testing.T) {
	body := []byte{1, 1, 0x7b} // one v128 local
	body = append(body, v128ConstBytes(i16x8Bytes(-32768, 32767, -129, 128, 0, -1, 1, -128))...)
	body = append(body, 0x21, 0, 0x20, 0) // local.set 0; local.get 0
	body = append(body, simdOp(132)...)
	body = append(body, 0x20, 0) // read the original local after the pack
	body = append(body, simdOp(24)...)
	body = append(body, 0, 0x6a, 0x0b) // i16x8.extract_lane_s 0; i32.add
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
	if got := runAmd64(t, m); got != -32768+0xa5 {
		t.Fatalf("result = %d, want %d", got, -32768+0xa5)
	}
}
