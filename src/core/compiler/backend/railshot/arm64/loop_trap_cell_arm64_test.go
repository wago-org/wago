//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestLoopTrapCellCachePreservesPollLayout(t *testing.T) {
	body := []byte{
		0x00,       // no declared locals
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x00, // local.get 0
		0x28, 0x02, 0x00, // i32.load align=4 offset=0
		0x1a,       // drop
		0x0c, 0x00, // br 0
		0x0b, 0x0b, 0x0b,
	}
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, nil, body)
	compile := func(enabled bool) (*ModuleStats, int) {
		t.Helper()
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{
			Interruptible: true,
			Optimizations: map[string]bool{"loop-trap-cell": enabled},
			Stats:         &stats,
		})
		if err != nil {
			t.Fatal(err)
		}
		return &stats, len(cm.Code)
	}
	on, onBytes := compile(true)
	off, offBytes := compile(false)
	if got := on.Funcs[0].Peephole["loop-trap-cell"]; got != 1 {
		t.Fatalf("enabled cache count = %d, want 1", got)
	}
	if got := off.Funcs[0].Peephole["loop-trap-cell"]; got != 0 {
		t.Fatalf("disabled cache count = %d, want 0", got)
	}
	if onBytes != offBytes {
		t.Fatalf("cached code = %d bytes, uncached = %d; poll layout must be stable", onBytes, offBytes)
	}
}

func TestLoopTrapCellRegisterSelectionPreservesPressureFloor(t *testing.T) {
	if got := selectLoopTrapCellReg(regNone, 0, 0); got != X27 {
		t.Fatalf("free selection = X%d, want X27", got)
	}
	if got := selectLoopTrapCellReg(X27, maskOf(X27), 0); got != X25 {
		t.Fatalf("explicit-bounds selection = X%d, want X25", got)
	}

	reserved := maskOf(X27)
	var pinned regMask
	for _, reg := range gpAlloc {
		if reg == X27 || reg == X25 {
			continue
		}
		pinned = pinned.add(reg)
		if pinned.count() > gpPinLimit(reserved.add(X25)) {
			break
		}
	}
	if got := selectLoopTrapCellReg(X27, reserved, pinned); got != regNone {
		t.Fatalf("pressure-floor selection = X%d, want none", got)
	}
}
