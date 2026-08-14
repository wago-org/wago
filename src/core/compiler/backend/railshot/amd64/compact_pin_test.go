//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestPreferPinRegAMD64(t *testing.T) {
	pool := []Reg{R12, R13, R14, R15, R9, R10, R11, RBP}
	preferPinReg(pool, RBP)
	want := []Reg{RBP, R12, R13, R14, R15, R9, R10, R11}
	for i := range want {
		if pool[i] != want[i] {
			t.Fatalf("pool[%d] = %v, want %v (all: %v)", i, pool[i], want[i], pool)
		}
	}
}

func TestSizePrefersLowRegisterForHotLeafLocalAMD64(t *testing.T) {
	before := compactLowPinEnabled
	t.Cleanup(func() { compactLowPinEnabled = before })
	i32 := []wasm.ValType{wasm.I32}
	body := []byte{0x00, 0x20, 0x00}
	for range 40 {
		body = append(body, 0x20, 0x00, 0x6a)
	}
	body = append(body, 0x0b)
	m := mod1(t, i32, i32, body)
	size := OptimizeSize
	compile := func(enabled bool) (int, *ModuleStats) {
		compactLowPinEnabled = enabled
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Objective: &size, Stats: &stats, Workers: 1})
		if err != nil {
			t.Fatal(err)
		}
		return len(cm.Code), &stats
	}
	rollback, _ := compile(false)
	compact, stats := compile(true)
	if got := stats.Funcs[0].Peephole["compact-low-local-pin"]; got != 1 {
		t.Fatalf("compact low pin count = %d, want 1", got)
	}
	if compact >= rollback {
		t.Fatalf("compact code = %d bytes, rollback = %d; want shrink", compact, rollback)
	}
}
