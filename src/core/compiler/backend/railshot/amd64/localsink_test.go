//go:build linux && amd64

package amd64

import (
	"math/bits"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// In-place local-result sinking: `local.set/tee $x (op (local.get $x) …)` computes
// straight into x's pinned register with no pre-copy. These functions exercise the
// shift, unary, convert, and tee self-update shapes the sink now covers; each is a
// single call-free function so x is register-pinned. The optimization is
// behavior-neutral, so correctness (and kill-switch equivalence) is what's checked.

func selfShift(t *testing.T) *wasm.Module { // x <<= 3
	return mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32},
		[]byte{0x00, 0x20, 0x00, 0x41, 0x03, 0x74, 0x21, 0x00, 0x20, 0x00, 0x0b})
}
func selfClz(t *testing.T) *wasm.Module { // x = clz(x)
	return mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32},
		[]byte{0x00, 0x20, 0x00, 0x67, 0x21, 0x00, 0x20, 0x00, 0x0b})
}
func selfExtend8(t *testing.T) *wasm.Module { // x = extend8_s(x)
	return mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32},
		[]byte{0x00, 0x20, 0x00, 0xc0, 0x21, 0x00, 0x20, 0x00, 0x0b})
}
func selfTee(t *testing.T) *wasm.Module { // y=(x=x+1); return y + x
	return mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32},
		[]byte{0x00, 0x20, 0x00, 0x41, 0x01, 0x6a, 0x22, 0x00, 0x20, 0x00, 0x6a, 0x0b})
}

func TestLocalSinkExec(t *testing.T) {
	for _, x := range []int32{0, 1, 5, -1, 0xff, 0x80, 1 << 28, -1234} {
		if got, want := runAmd64(t, selfShift(t), x), x<<3; got != want {
			t.Errorf("x<<3 (x=%d) = %d, want %d", x, got, want)
		}
		if got, want := runAmd64(t, selfClz(t), x), int32(bits.LeadingZeros32(uint32(x))); got != want {
			t.Errorf("clz(x=%#x) = %d, want %d", x, got, want)
		}
		if got, want := runAmd64(t, selfExtend8(t), x), int32(int8(x)); got != want {
			t.Errorf("extend8_s(x=%#x) = %d, want %d", x, got, want)
		}
		if got, want := runAmd64(t, selfTee(t), x), 2*(x+1); got != want {
			t.Errorf("tee(x=%d) = %d, want %d", x, got, want)
		}
	}
}

// TestLocalSinkKillSwitchEquivalent verifies the unary/convert and tee sinks are
// behavior-neutral: same results with the sinks on and off.
func TestLocalSinkKillSwitchEquivalent(t *testing.T) {
	defer func(u, te bool) { unaryLocalSinkEnabled, teeLocalSinkEnabled = u, te }(unaryLocalSinkEnabled, teeLocalSinkEnabled)
	build := map[string]func(*testing.T) *wasm.Module{
		"clz": selfClz, "extend8": selfExtend8, "tee": selfTee,
	}
	for name, mk := range build {
		for _, x := range []int32{0, 1, 0xff, -1, 1 << 28} {
			unaryLocalSinkEnabled, teeLocalSinkEnabled = true, true
			on := runAmd64(t, mk(t), x)
			unaryLocalSinkEnabled, teeLocalSinkEnabled = false, false
			off := runAmd64(t, mk(t), x)
			if on != off {
				t.Fatalf("%s(x=%#x): on=%d off=%d", name, x, on, off)
			}
		}
	}
}
