//go:build linux && amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func forwardMergeModule(t *testing.T, after []byte) *wasm.Module {
	t.Helper()
	i32 := []wasm.ValType{wasm.I32}
	body := []byte{
		0x00,
		0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00, // dirty local0
		0x10, 0x01, // call: local0 becomes memory-only
		0x41, 0x00, 0x04, 0x40, // if false
		0x20, 0x00, 0x1a, // then edge reloads local0
		0x0b,
	}
	body = append(body, after...)
	body = append(body, 0x41, 0x07, 0x0b)
	return modFuncs(t,
		funcDef{params: i32, results: i32, body: body},
		funcDef{body: []byte{0x00, 0x0b}},
	)
}

func compileForwardMergeStats(t *testing.T, m *wasm.Module, on bool) CodegenStats {
	t.Helper()
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"inline": false, "merge-next-use": on}}); err != nil {
		t.Fatal(err)
	}
	return *stats.Funcs[0]
}

func TestForwardMergeNextUseSkipsDeadReloadAMD64(t *testing.T) {
	m := forwardMergeModule(t, nil)
	without := compileForwardMergeStats(t, m, false)
	with := compileForwardMergeStats(t, m, true)
	if without.LocalTraffic.ControlMergeReloads != 1 {
		t.Fatalf("disabled merge reloads = %d, want 1", without.LocalTraffic.ControlMergeReloads)
	}
	if with.LocalTraffic.ControlMergeReloads != 0 || with.Peephole["merge-dead-reload"] != 1 {
		t.Fatalf("enabled merge traffic = %+v peep=%v, want one dead reload removed", with.LocalTraffic, with.Peephole)
	}
	if with.CodeBytes >= without.CodeBytes {
		t.Fatalf("enabled code bytes = %d, want less than %d", with.CodeBytes, without.CodeBytes)
	}
	if got := runAmd64(t, m, 3); got != 7 {
		t.Fatalf("result = %d, want 7", got)
	}
}

func TestForwardMergeNextUseKeepsReadAndFuelFallbackAMD64(t *testing.T) {
	read := forwardMergeModule(t, []byte{0x20, 0x00, 0x1a})
	readStats := compileForwardMergeStats(t, read, true)
	if readStats.LocalTraffic.ControlMergeReloads != 1 || readStats.Peephole["merge-dead-reload"] != 0 {
		t.Fatalf("read near miss traffic = %+v peep=%v", readStats.LocalTraffic, readStats.Peephole)
	}

	after := make([]byte, maxMergeNextUseOps)
	for i := range after {
		after[i] = 0x01 // nop
	}
	after = append(after, 0x41, 0x09, 0x21, 0x00) // overwrite beyond the fuel cap
	fuel := forwardMergeModule(t, after)
	fuelStats := compileForwardMergeStats(t, fuel, true)
	if fuelStats.LocalTraffic.ControlMergeReloads != 1 || fuelStats.Peephole["merge-dead-reload"] != 0 {
		t.Fatalf("fuel fallback traffic = %+v peep=%v", fuelStats.LocalTraffic, fuelStats.Peephole)
	}
	compile := func() []byte {
		cm, err := CompileModuleWith(fuel, CompileOptions{Optimizations: map[string]bool{"inline": false, "merge-next-use": true}})
		if err != nil {
			t.Fatal(err)
		}
		return cm.Code
	}
	if a, b := compile(), compile(); !bytes.Equal(a, b) {
		t.Fatal("fuel fallback emitted nondeterministic code")
	}
}
