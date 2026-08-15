//go:build arm64

package arm64

import (
	"bytes"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func forwardMergeModuleARM64(t testing.TB, after []byte) *wasm.Module {
	t.Helper()
	i32x2 := []wasm.ValType{wasm.I32, wasm.I32}
	body := []byte{
		0x00,
		0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00, // dirty local0
		0x10, 0x01, // call: local0 becomes memory-only
		0x20, 0x01, 0x04, 0x40, // if param1
		0x20, 0x00, 0x1a, // then edge reloads local0
		0x0b,
	}
	body = append(body, after...)
	body = append(body, 0x41, 0x07, 0x0b)
	return modFuncs(t,
		funcDef{params: i32x2, results: []wasm.ValType{wasm.I32}, body: body},
		funcDef{body: []byte{0x01, 0x01, 0x7f, 0x0b}},
	)
}

func compileForwardMergeStatsARM64(t testing.TB, m *wasm.Module, on bool) CodegenStats {
	t.Helper()
	var stats ModuleStats
	if _, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{
		"abi-classes":    false,
		"inline":         false,
		"merge-next-use": on,
	}}); err != nil {
		t.Fatal(err)
	}
	return *stats.Funcs[0]
}

func TestForwardMergeNextUseSkipsDeadReloadARM64(t *testing.T) {
	m := forwardMergeModuleARM64(t, nil)
	without := compileForwardMergeStatsARM64(t, m, false)
	with := compileForwardMergeStatsARM64(t, m, true)
	if without.LocalTraffic.ControlMergeReloads != 1 {
		t.Fatalf("disabled merge reloads = %d, want 1", without.LocalTraffic.ControlMergeReloads)
	}
	if with.LocalTraffic.ControlMergeReloads != 0 || with.Peephole["merge-dead-reload"] != 1 {
		t.Fatalf("enabled merge traffic = %+v peep=%v, want one dead reload removed", with.LocalTraffic, with.Peephole)
	}
	if with.CodeBytes >= without.CodeBytes {
		t.Fatalf("enabled code bytes = %d, want less than %d", with.CodeBytes, without.CodeBytes)
	}
	got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{
		"abi-classes":    false,
		"inline":         false,
		"merge-next-use": true,
	}}, 3, 0)
	if err != nil || got != 7 {
		t.Fatalf("result = %d, %v; want 7", got, err)
	}
}

func TestForwardMergeNextUseSkipsDeadFloatReloadARM64(t *testing.T) {
	body := []byte{
		0x00,
		0x20, 0x00,
		0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x3f, // f64.const 1
		0xa0, 0x21, 0x00, // f64.add; local.set 0
		0x10, 0x01, // call: local0 becomes memory-only
		0x20, 0x01, 0x04, 0x40, // if param1
		0x20, 0x00, 0x1a, // then edge reloads local0
		0x0b,
		0x41, 0x07, 0x0b,
	}
	m := modFuncs(t,
		funcDef{params: []wasm.ValType{wasm.F64, wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body},
		funcDef{body: []byte{0x01, 0x01, 0x7f, 0x0b}},
	)
	without := compileForwardMergeStatsARM64(t, m, false)
	with := compileForwardMergeStatsARM64(t, m, true)
	if without.LocalTraffic.ControlMergeReloads != 1 || with.LocalTraffic.ControlMergeReloads != 0 || with.Peephole["merge-dead-reload"] != 1 {
		t.Fatalf("float merge traffic disabled=%+v enabled=%+v peep=%v", without.LocalTraffic, with.LocalTraffic, with.Peephole)
	}
	got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{
		"abi-classes":    false,
		"inline":         false,
		"merge-next-use": true,
	}}, math.Float64bits(3), 0)
	if err != nil || got != 7 {
		t.Fatalf("result = %d, %v; want 7", got, err)
	}
}

func TestForwardMergeNextUseKeepsReadAndFuelFallbackARM64(t *testing.T) {
	read := forwardMergeModuleARM64(t, []byte{0x20, 0x00, 0x1a})
	readStats := compileForwardMergeStatsARM64(t, read, true)
	if readStats.LocalTraffic.ControlMergeReloads != 1 || readStats.Peephole["merge-dead-reload"] != 0 {
		t.Fatalf("read near miss traffic = %+v peep=%v", readStats.LocalTraffic, readStats.Peephole)
	}

	after := make([]byte, maxMergeNextUseOps)
	for i := range after {
		after[i] = 0x01 // nop
	}
	after = append(after, 0x41, 0x09, 0x21, 0x00) // overwrite beyond the fuel cap
	fuel := forwardMergeModuleARM64(t, after)
	fuelStats := compileForwardMergeStatsARM64(t, fuel, true)
	if fuelStats.LocalTraffic.ControlMergeReloads != 1 || fuelStats.Peephole["merge-dead-reload"] != 0 {
		t.Fatalf("fuel fallback traffic = %+v peep=%v", fuelStats.LocalTraffic, fuelStats.Peephole)
	}
	compile := func() []byte {
		cm, err := CompileModuleWith(fuel, CompileOptions{Optimizations: map[string]bool{
			"abi-classes":    false,
			"inline":         false,
			"merge-next-use": true,
		}})
		if err != nil {
			t.Fatal(err)
		}
		return cm.Code
	}
	if a, b := compile(), compile(); !bytes.Equal(a, b) {
		t.Fatal("fuel fallback emitted nondeterministic code")
	}
}

func BenchmarkCompileForwardMergeNextUseARM64(b *testing.B) {
	m := forwardMergeModuleARM64(b, nil)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"eager", false}, {"dead", true}} {
		b.Run(tc.name, func(b *testing.B) {
			opts := CompileOptions{Optimizations: map[string]bool{
				"abi-classes":    false,
				"inline":         false,
				"merge-next-use": tc.on,
			}}
			b.ReportAllocs()
			for range b.N {
				if _, err := CompileModuleWith(m, opts); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
