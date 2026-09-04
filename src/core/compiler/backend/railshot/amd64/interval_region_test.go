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

func TestIntervalRegionLastGetStorageOnlyForCandidates(t *testing.T) {
	saved := intervalRegionPinsEnabled
	defer func() { intervalRegionPinsEnabled = saved }()
	intervalRegionPinsEnabled = true

	m := intervalRegionModule(t)
	longBody := make([]byte, minIntervalRegionBody+1)
	for i := range longBody[:len(longBody)-4] {
		longBody[i] = 0x01 // nop
	}
	copy(longBody[len(longBody)-4:], []byte{0x20, 0x00, 0x1a, 0x0b}) // local.get 0; drop; end
	m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0], m.FuncTypes[0])
	m.Code = append(m.Code, wasm.Func{
		Locals:    wasm.Locals{Runs: []wasm.LocalRun{{Count: 100, Type: wasm.I32}}},
		BodyBytes: []byte{0x0b},
	}, wasm.Func{
		Locals:    wasm.Locals{Runs: []wasm.LocalRun{{Count: 100, Type: wasm.I32}}},
		BodyBytes: longBody,
	})
	hints, sidecar, _, err := computeModuleHints(m, 0, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(sidecar.view(hints[0]).localLastGet); got != 20 {
		t.Fatalf("candidate last-get storage = %d locals, want 20", got)
	}
	ineligible := sidecar.view(hints[1])
	if got, want := ineligible.nLocals, 100; got != want {
		t.Fatalf("ineligible local count = %d, want %d", got, want)
	}
	if got, want := len(ineligible.localScore), 64; got != want {
		t.Fatalf("ineligible retained scores = %d, want %d", got, want)
	}
	if got := ineligible.localLastGet; got != nil {
		t.Fatalf("ineligible function reserved %d last-get entries", len(got))
	}
	eligible := sidecar.view(hints[2])
	if got, want := len(eligible.localScore), 100; got != want {
		t.Fatalf("wide candidate retained scores = %d, want %d", got, want)
	}
	if got, want := len(eligible.localLastGet), 100; got != want {
		t.Fatalf("wide candidate last-get storage = %d, want %d", got, want)
	}
}
