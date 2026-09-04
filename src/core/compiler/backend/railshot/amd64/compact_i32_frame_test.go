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

func compactI32FrameOptions(enabled bool, stats *ModuleStats) CompileOptions {
	return CompileOptions{
		Stats: stats,
		Optimizations: map[string]bool{
			"compact-i32-frame": enabled,
			"frame-elide":       false,
		},
	}
}

func compileCompactI32FrameStats(t *testing.T, m *wasm.Module, enabled bool) *CodegenStats {
	t.Helper()
	stats := &ModuleStats{}
	cm, err := CompileModuleWith(m, compactI32FrameOptions(enabled, stats))
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		t.Cleanup(func() { _ = cm.CodeImage.Close() })
	}
	return stats.Funcs[0]
}

func TestCompactI32Frame(t *testing.T) {
	m := compactI32FrameModule(t)
	off := compileCompactI32FrameStats(t, m, false)
	got, _, err := runMemAmd64WithOptions(t, m, compactI32FrameOptions(false, nil), nil)
	if err != nil || got != 100 {
		t.Fatalf("unpacked result = %d, err=%v, want 100", got, err)
	}

	on := compileCompactI32FrameStats(t, m, true)
	got, _, err = runMemAmd64WithOptions(t, m, compactI32FrameOptions(true, nil), nil)
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
	rollback := compileCompactI32FrameStats(t, m, false)
	enabled := compileCompactI32FrameStats(t, m, true)
	if enabled.FrameBytes >= rollback.FrameBytes {
		t.Fatalf("packed control-flow frame = %d bytes, rollback = %d", enabled.FrameBytes, rollback.FrameBytes)
	}
	if enabled.Peephole["compact-i32-frame"] != 1 {
		t.Fatalf("compact-i32-frame hits = %d, want 1", enabled.Peephole["compact-i32-frame"])
	}
	got, _, err := runMemAmd64WithOptions(t, m, compactI32FrameOptions(true, nil), nil)
	if err != nil || got != 100 {
		t.Fatalf("packed control-flow result = %d, err=%v, want 100", got, err)
	}
}

func TestCompactI32FrameAcrossCall(t *testing.T) {
	callee := make([]byte, 0, 205)
	callee = append(callee, 0x00)
	for range 200 {
		callee = append(callee, 0x01) // nop; keep the call out of inline admission.
	}
	callee = append(callee, 0x20, 0x00, 0x0b)
	caller := []byte{0x01, 0x14, 0x7f,
		0x41, 0x2a, 0x21, 0x00,
		0x41, 0x7f, 0x21, 0x01, // adjacent packed local must not contaminate arg 0.
		0x20, 0x00, 0x10, 0x01,
		0x0b}
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: caller},
		funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: callee},
	)
	rollback := compileCompactI32FrameStats(t, m, false)
	enabled := compileCompactI32FrameStats(t, m, true)
	if enabled.FrameBytes >= rollback.FrameBytes {
		t.Fatalf("packed call frame = %d bytes, rollback = %d", enabled.FrameBytes, rollback.FrameBytes)
	}
	if enabled.Peephole["compact-i32-frame"] != 1 {
		t.Fatalf("compact-i32-frame hits = %d, want 1", enabled.Peephole["compact-i32-frame"])
	}
	got, _, err := runMemAmd64WithOptions(t, m, compactI32FrameOptions(true, nil), nil)
	if err != nil || got != 42 {
		t.Fatalf("packed call result = %d, err=%v, want 42", got, err)
	}
}
