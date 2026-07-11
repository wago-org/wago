//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// addLeafModule is a call-free single-result reg-ABI leaf f(a,b)=a+b. Both params
// are register-homed and it never spills, so its frame is never addressed — the
// frame adjust is elided (frameSize 0).
func addLeafModule(t *testing.T) *wasm.Module {
	return mod1(t, []wasm.ValType{i32, i32}, []wasm.ValType{i32},
		[]byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b})
}

func TestFrameElideExec(t *testing.T) {
	m := addLeafModule(t)
	for _, tc := range []struct{ a, b, want int32 }{{3, 4, 7}, {-1, 1, 0}, {1 << 30, 1 << 29, (1 << 30) + (1 << 29)}} {
		if got := runAmd64(t, m, tc.a, tc.b); got != tc.want {
			t.Errorf("add(%d,%d) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestFrameElideFires(t *testing.T) {
	if s := compileWithStats(t, addLeafModule(t), false).Funcs[0]; s.Peephole["frame-adjust-elide"] == 0 {
		t.Fatalf("frame-adjust-elide = 0, want >=1 (all: %v)", s.Peephole)
	}
	saved := smallFrameElideEnabled
	smallFrameElideEnabled = false
	defer func() { smallFrameElideEnabled = saved }()
	if s := compileWithStats(t, addLeafModule(t), false).Funcs[0]; s.Peephole["frame-adjust-elide"] != 0 {
		t.Fatalf("frame-adjust-elide still fired with elision disabled: %v", s.Peephole)
	}
}

// TestFrameElideKillSwitchEquivalent verifies the elision is behavior-neutral.
func TestFrameElideKillSwitchEquivalent(t *testing.T) {
	defer func(prev bool) { smallFrameElideEnabled = prev }(smallFrameElideEnabled)
	for _, tc := range []struct{ a, b int32 }{{3, 4}, {-5, 9}, {1 << 20, 7}} {
		smallFrameElideEnabled = true
		on := runAmd64(t, addLeafModule(t), tc.a, tc.b)
		smallFrameElideEnabled = false
		off := runAmd64(t, addLeafModule(t), tc.a, tc.b)
		if on != off {
			t.Fatalf("add(%d,%d): on=%d off=%d", tc.a, tc.b, on, off)
		}
	}
}
