//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestCallSafePinPoolExcludesCallerSavedRegistersAfterReservations(t *testing.T) {
	pool := []Reg{X19, X20, X21, X23, X24, X9, X10, X11, X15, X27}
	got := callSafePinPool(pool)
	want := [...]Reg{X19, X20, X21, X23, X24}
	if len(got) != len(want) {
		t.Fatalf("safe pool = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("safe pool = %v, want %v", got, want)
		}
	}
}

func TestLocalLoopPinThresholdChargesCallBoundaries(t *testing.T) {
	for _, tc := range []struct {
		reg       Reg
		callSites uint16
		want      uint32
	}{
		{reg: X15, callSites: 0, want: 0},
		{reg: X15, callSites: 6, want: 84},
		{reg: X8, callSites: 2, want: 200},
		{reg: X8, callSites: 6, want: 240},
		{reg: X27, callSites: 2, want: 200},
		{reg: X27, callSites: 6, want: 240},
	} {
		if got := localLoopPinThreshold(tc.reg, tc.callSites); got != tc.want {
			t.Errorf("threshold(%v, %d) = %d, want %d", tc.reg, tc.callSites, got, tc.want)
		}
	}
	if localLoopPinProfitable(X15, 6, 84) {
		t.Error("break-even X15 pin reported profitable")
	}
	if !localLoopPinProfitable(X15, 6, 85) {
		t.Error("profitable X15 pin rejected")
	}
}

func TestDirectLoopCallCanLeaseX8ForHotLocal(t *testing.T) {
	body := []byte{0x01, 0x10, 0x7f} // sixteen i32 locals
	for local := byte(0); local < 16; local++ {
		body = append(body, 0x41, local+1, 0x21, local) // local = local + 1
	}
	body = append(body, 0x02, 0x40, 0x03, 0x40) // block; loop
	for range 30 {
		for local := byte(0); local < 16; local++ {
			body = append(body, 0x20, local, 0x1a) // local.get; drop
		}
	}
	body = append(body, 0x10, 0x01, 0x1a, 0x0b, 0x0b, 0x20, 0x00) // call; drop; end loop/block; local 0
	for local := byte(1); local < 16; local++ {
		body = append(body, 0x20, local, 0x6a) // add the remaining locals
	}
	body = append(body, 0x0b)
	m := modFuncs(t,
		funcDef{results: []wasm.ValType{wasm.I32}, body: body},
		funcDef{results: []wasm.ValType{wasm.I32}, body: []byte{0x00, 0x41, 0x01, 0x0b}})

	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		opts := CompileOptions{Optimizations: map[string]bool{
			"inline":               false,
			"interval-region-pins": false,
			"x8-pin":               enabled,
		}}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		gotResult, err := runArm64WrapperWithOptions(t, m, opts)
		if err != nil || gotResult != 136 {
			t.Fatalf("enabled=%t: result = %d, err %v; want 136", enabled, gotResult, err)
		}
		if diagnosticsEnabled {
			got := stats.Funcs[0].Peephole["direct-call-x8-pin"]
			want := 0
			if enabled {
				want = 1
			}
			if got != want {
				t.Fatalf("enabled=%t: direct-call-x8-pin = %d, want %d", enabled, got, want)
			}
		}
	}
}
