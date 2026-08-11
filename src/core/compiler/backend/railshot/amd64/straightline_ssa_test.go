//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func straightLineSSATestModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := make([]byte, 1, 1400) // zero local-declaration groups
	for range 80 {
		// memory8[local[0]] = local[1]
		body = append(body, 0x20, 0x00, 0x20, 0x01, 0x3a, 0x00, 0x00)
		// local[1] = rotl(memory8_u[local[0]] * 3, 7)
		body = append(body, 0x20, 0x00, 0x2d, 0x00, 0x00, 0x41, 0x03, 0x6c, 0x41, 0x07, 0x77, 0x21, 0x01)
	}
	// memory[local[0]] = local[1]
	body = append(body, 0x20, 0x00, 0x20, 0x01, 0x36, 0x02, 0x00, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, nil, body)
}

func TestStraightLineSSAAdmissionAndFallback(t *testing.T) {
	old := straightLineSSAEnabled
	defer func() { straightLineSSAEnabled = old }()
	straightLineSSAEnabled = true
	m := straightLineSSATestModule(t)

	guard := compileWithStats(t, m, true)
	if got := guard.Funcs[0].Peephole["straightline-ssa-emitted"]; got != 1 {
		t.Fatalf("guard straightline-ssa-emitted = %d, want 1", got)
	}
	if got := guard.Funcs[0].Peephole["compact-i32-frame"]; got != 0 {
		t.Fatalf("guard compact-i32-frame = %d, want scheduler's 8-byte local slots", got)
	}
	explicit := compileWithStats(t, m, false)
	if got := explicit.Funcs[0].Peephole["straightline-ssa-emitted"]; got != 0 {
		t.Fatalf("explicit straightline-ssa-emitted = %d, want fallback", got)
	}

	straightLineSSAEnabled = false
	disabled := compileWithStats(t, m, true)
	if got := disabled.Funcs[0].Peephole["straightline-ssa-emitted"]; got != 0 {
		t.Fatalf("disabled straightline-ssa-emitted = %d, want fallback", got)
	}
	if got := disabled.Funcs[0].Peephole["compact-i32-frame"]; got != 1 {
		t.Fatalf("disabled compact-i32-frame = %d, want direct-path optimization", got)
	}
}
