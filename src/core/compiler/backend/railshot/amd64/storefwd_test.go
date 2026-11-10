//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// storeFwdModule builds f(a i32, v i32) i32 that stores an owned computed value to
// mem[a] and immediately reloads it from the same local address+offset:
//
//	mem[a] = v + 5; return mem[a]
//
// The reload is the exact-width store-forwarding shape, so the compiler keeps the
// stored value in a register and forwards it instead of emitting a load.
func storeFwdModule(t *testing.T) *wasm.Module {
	return modMem(t, 1, []wasm.ValType{i32, i32}, []wasm.ValType{i32}, []byte{
		0x00,       // 0 locals
		0x20, 0x00, // local.get 0 (a) — store address
		0x20, 0x01, // local.get 1 (v)
		0x41, 0x05, // i32.const 5
		0x6a,             // i32.add        → v+5 (owned)
		0x36, 0x02, 0x00, // i32.store align=2 off=0   (mem[a] = v+5)
		0x20, 0x00, // local.get 0 (a)
		0x28, 0x02, 0x00, // i32.load align=2 off=0     (forwarded v+5)
		0x0b, // end
	})
}

func TestStoreForwardExec(t *testing.T) {
	m := storeFwdModule(t)
	for _, tc := range []struct{ a, v, want int32 }{
		{0, 42, 47}, {16, 100, 105}, {64, -5, 0},
	} {
		if got := runAmd64(t, m, tc.a, tc.v); got != tc.want {
			t.Errorf("f(%d,%d) = %d, want %d", tc.a, tc.v, got, tc.want)
		}
	}
}

func TestStoreForwardFires(t *testing.T) {
	m := storeFwdModule(t)
	s := compileWithStats(t, m, false).Funcs[0]
	if got := s.Peephole["linear-store-load-fwd"]; got != 1 {
		t.Fatalf("linear-store-load-fwd = %d, want 1 (all: %v)", got, s.Peephole)
	}
}

// TestStoreForwardKillSwitchEquivalent verifies the forwarding peephole is
// behavior-neutral: the same module produces the same results with it on and off.
func TestStoreForwardKillSwitchEquivalent(t *testing.T) {
	defer func(prev bool) { linearStoreForwardEnabled = prev }(linearStoreForwardEnabled)
	for _, tc := range []struct{ a, v int32 }{{0, 42}, {32, 7}, {8, -100}} {
		linearStoreForwardEnabled = true
		on := runAmd64(t, storeFwdModule(t), tc.a, tc.v)
		linearStoreForwardEnabled = false
		off := runAmd64(t, storeFwdModule(t), tc.a, tc.v)
		if on != off {
			t.Fatalf("f(%d,%d): on=%d off=%d", tc.a, tc.v, on, off)
		}
	}
}
