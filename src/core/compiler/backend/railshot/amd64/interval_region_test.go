//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func intervalRegionModule(t *testing.T) *wasm.Module {
	body := []byte{0x01, 0x14, 0x7f} // twenty i32 locals
	for x := byte(0); x < 20; x++ {
		body = append(body, 0x41, x+1, 0x21, x) // local[x] = x+1
	}
	body = append(body, 0x20, 0x00)
	for x := byte(1); x < 20; x++ {
		body = append(body, 0x20, x, 0x6a)
	}
	body = append(body, 0x0b)
	return mod1(t, nil, []wasm.ValType{wasm.I32}, body)
}

func TestIntervalRegionDynamicReuse(t *testing.T) {
	saved := intervalRegionPinsEnabled
	defer func() { intervalRegionPinsEnabled = saved }()
	m := intervalRegionModule(t)

	intervalRegionPinsEnabled = true
	on := compileWithStats(t, m, false).Funcs[0]
	if got := runAmd64(t, m); got != 210 {
		t.Fatalf("enabled result = %d, want 210", got)
	}
	if on.Peephole["interval-region"] != 1 {
		t.Fatalf("interval-region = %d, want 1 (all: %v)", on.Peephole["interval-region"], on.Peephole)
	}
	if on.Peephole["interval-region-reactivate"] == 0 {
		t.Fatalf("dynamic regional cache did not reuse a register: %v", on.Peephole)
	}
	if on.Peephole["tree-order"] != 0 {
		t.Fatalf("tree ordering must stay disabled while regional registers are active: %v", on.Peephole)
	}

	intervalRegionPinsEnabled = false
	if got := runAmd64(t, m); got != 210 {
		t.Fatalf("disabled result = %d, want 210", got)
	}
}
