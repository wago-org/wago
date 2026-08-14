//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func compactI32FrameModule(t *testing.T) *wasm.Module {
	// Twenty declared i32 locals force frame traffic. Set four, then return their
	// sum; the remaining locals exercise packed zero-initialization.
	body := []byte{0x01, 0x14, 0x7f,
		0x41, 0x0a, 0x21, 0x00,
		0x41, 0x14, 0x21, 0x01,
		0x41, 0x1e, 0x21, 0x02,
		0x41, 0x28, 0x21, 0x03,
		0x20, 0x00, 0x20, 0x01, 0x6a,
		0x20, 0x02, 0x6a, 0x20, 0x03, 0x6a,
		0x0b}
	return modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
}

func TestCompactI32Frame(t *testing.T) {
	defer func(compact, control, elide bool) {
		compactI32FrameEnabled = compact
		compactI32ControlFlowEnabled = control
		smallFrameElideEnabled = elide
	}(compactI32FrameEnabled, compactI32ControlFlowEnabled, smallFrameElideEnabled)
	smallFrameElideEnabled = false

	m := compactI32FrameModule(t)
	compactI32FrameEnabled = false
	off := compileWithStats(t, m, false).Funcs[0]
	got, _, err := runMemAmd64(t, m, nil)
	if err != nil || got != 100 {
		t.Fatalf("unpacked result = %d, err=%v, want 100", got, err)
	}

	compactI32FrameEnabled = true
	on := compileWithStats(t, m, false).Funcs[0]
	got, _, err = runMemAmd64(t, m, nil)
	if err != nil || got != 100 {
		t.Fatalf("packed result = %d, err=%v, want 100", got, err)
	}
	if on.FrameBytes >= off.FrameBytes {
		t.Fatalf("packed frame = %d bytes, unpacked = %d", on.FrameBytes, off.FrameBytes)
	}
	if on.Peephole["compact-i32-frame"] != 1 {
		t.Fatalf("compact-i32-frame hits = %d, want 1", on.Peephole["compact-i32-frame"])
	}
}

func TestCompactI32FrameThroughControlFlow(t *testing.T) {
	body := []byte{0x01, 0x14, 0x7f,
		0x41, 0x01, 0x04, 0x40, // if (true)
		0x41, 0x0a, 0x21, 0x00,
		0x41, 0x14, 0x21, 0x01,
		0x41, 0x1e, 0x21, 0x02,
		0x41, 0x28, 0x21, 0x03,
		0x0b,
		0x20, 0x00, 0x20, 0x01, 0x6a,
		0x20, 0x02, 0x6a, 0x20, 0x03, 0x6a,
		0x0b}
	m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
	beforeCompact, beforeControl := compactI32FrameEnabled, compactI32ControlFlowEnabled
	t.Cleanup(func() {
		compactI32FrameEnabled = beforeCompact
		compactI32ControlFlowEnabled = beforeControl
	})
	compactI32FrameEnabled = true
	compactI32ControlFlowEnabled = false
	rollback := compileWithStats(t, m, false).Funcs[0]
	compactI32ControlFlowEnabled = true
	enabled := compileWithStats(t, m, false).Funcs[0]
	if enabled.FrameBytes >= rollback.FrameBytes {
		t.Fatalf("packed control-flow frame = %d bytes, rollback = %d", enabled.FrameBytes, rollback.FrameBytes)
	}
	if enabled.Peephole["compact-i32-frame"] != 1 {
		t.Fatalf("compact-i32-frame hits = %d, want 1", enabled.Peephole["compact-i32-frame"])
	}
	got, _, err := runMemAmd64(t, m, nil)
	if err != nil || got != 100 {
		t.Fatalf("packed control-flow result = %d, err=%v, want 100", got, err)
	}
}
