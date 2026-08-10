//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func commuteSelfUpdateModule(t *testing.T) *wasm.Module {
	t.Helper()
	// Repeat x = (a + b) ^ x twice. The first candidate retains the conservative
	// spill form; the repeated site proves this is a dense self-update function and
	// accumulates directly in x's destination register.
	body := []byte{
		0x01, 0x06, 0x7f, // six extra locals keep this out of the tiny-leaf ABI
		0x20, 0x01,
		0x20, 0x02,
		0x6a,
		0x20, 0x00,
		0x73,
		0x21, 0x00,
		0x20, 0x01,
		0x20, 0x02,
		0x6a,
		0x20, 0x00,
		0x73,
		0x22, 0x00,
		0x0b,
	}
	return mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
}

func TestCommuteSelfUpdate(t *testing.T) {
	saved, savedTree, savedOrder := commuteSelfUpdateEnabled, associativeTreeEnabled, treeOrderEnabled
	defer func() {
		commuteSelfUpdateEnabled, associativeTreeEnabled, treeOrderEnabled = saved, savedTree, savedOrder
	}()
	// The whole-tree cover supersedes this local two-address rewrite. Disable it
	// here to exercise the fallback used while regional local registers are live.
	associativeTreeEnabled = false
	treeOrderEnabled = false
	m := commuteSelfUpdateModule(t)

	commuteSelfUpdateEnabled = false
	off := compileWithStats(t, m, false).Funcs[0]
	if got := runAmd64(t, m, 0x55, 10, 20); got != 0x55 {
		t.Fatalf("disabled result = %#x, want 0x55", got)
	}

	commuteSelfUpdateEnabled = true
	on := compileWithStats(t, m, false).Funcs[0]
	if got := runAmd64(t, m, 0x55, 10, 20); got != 0x55 {
		t.Fatalf("enabled result = %#x, want 0x55", got)
	}
	if got := on.Peephole["commute-self-update"]; got != 1 {
		t.Fatalf("commute-self-update = %d, want 1 (all: %v)", got, on.Peephole)
	}
	if on.Spills >= off.Spills {
		t.Fatalf("enabled spills = %d, disabled = %d", on.Spills, off.Spills)
	}
	if on.CodeBytes >= off.CodeBytes {
		t.Fatalf("enabled code = %d bytes, disabled = %d", on.CodeBytes, off.CodeBytes)
	}
}
