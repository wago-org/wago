//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func intervalRegionModuleArm64(t *testing.T) *wasm.Module {
	body := []byte{0x01, 0x20, 0x7f}            // thirty-two i32 locals
	body = append(body, 0x41, 0x00, 0x21, 0x00) // sum = 0
	for x := byte(1); x < 32; x++ {
		body = append(body,
			0x41, x, 0x21, x, // local[x] = x
			0x20, 0x00, 0x20, x, 0x6a, 0x21, 0x00, // sum += local[x]
		)
	}
	body = append(body, 0x20, 0x00, 0x0b)
	return mod1(t, nil, []wasm.ValType{wasm.I32}, body)
}

func TestIntervalRegionDynamicReuseArm64(t *testing.T) {
	saved := intervalRegionPinsEnabled
	defer func() { intervalRegionPinsEnabled = saved }()
	m := intervalRegionModuleArm64(t)

	intervalRegionPinsEnabled = true
	on := compileWithStats(t, m, false).Funcs[0]
	if got := runArm64(t, m); got != 496 {
		t.Fatalf("enabled result = %d, want 496", got)
	}
	if on.Peephole["interval-region"] != 1 {
		t.Fatalf("interval-region = %d, want 1 (all: %v)", on.Peephole["interval-region"], on.Peephole)
	}
	if on.Peephole["interval-region-reactivate"] == 0 {
		t.Fatalf("dynamic regional cache did not reuse a register: %v", on.Peephole)
	}

	intervalRegionPinsEnabled = false
	off := compileWithStats(t, m, false).Funcs[0]
	if on.CodeBytes >= off.CodeBytes {
		t.Fatalf("regional code = %d bytes, whole-function code = %d; want smaller", on.CodeBytes, off.CodeBytes)
	}
	if got := runArm64(t, m); got != 496 {
		t.Fatalf("disabled result = %d, want 496", got)
	}
}

func TestEntryInitializedLocalSkipsZeroArm64(t *testing.T) {
	saved := entryInitElisionEnabled
	defer func() { entryInitElisionEnabled = saved }()
	body := []byte{
		0x01, 0x01, 0x7f, // one i32 local
		0x41, 0x07, 0x21, 0x00, // local[0] = 7
		0x20, 0x00, 0x0b, // return local[0]
	}
	m := mod1(t, nil, []wasm.ValType{wasm.I32}, body)
	entryInitElisionEnabled = true
	on := compileWithStats(t, m, false).Funcs[0]
	if got := on.Peephole["entry-init-elide"]; got != 1 {
		t.Fatalf("entry-init-elide = %d, want 1 (all: %v)", got, on.Peephole)
	}
	if got := runArm64(t, m); got != 7 {
		t.Fatalf("result = %d, want 7", got)
	}
	entryInitElisionEnabled = false
	off := compileWithStats(t, m, false).Funcs[0]
	if got := off.Peephole["entry-init-elide"]; got != 0 {
		t.Fatalf("disabled entry-init-elide = %d, want 0", got)
	}
	if got := runArm64(t, m); got != 7 {
		t.Fatalf("disabled result = %d, want 7", got)
	}
}
