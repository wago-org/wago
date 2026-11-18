//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestIntervalWritebackCostPressure(t *testing.T) {
	old := intervalWritebackCostEnabled
	t.Cleanup(func() { intervalWritebackCostEnabled = old })
	body := []byte{1, 48, 0x7f, 0x3f, 0, 0x1a}
	for x := 1; x <= 48; x++ {
		body = append(body, 0x20, 0, 0x41)
		body = append(body, wasmtest.SLEB32(int32(uint32(x)*0x9e3779b9))...)
		body = append(body, 0x73, 0x21, byte(x))
	}
	for round := 0; round < 4; round++ {
		for x := 1; x <= 48; x++ {
			body = append(body, 0x20, byte(x), 0x20, byte((x+6)%48+1), 0x6a, 0x21, byte(x))
		}
	}
	body = append(body, 0x41, 0)
	for x := 1; x <= 48; x++ {
		body = append(body, 0x20, byte(x), 0x73)
	}
	body = append(body, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	for _, enabled := range []bool{false, true} {
		intervalWritebackCostEnabled = enabled
		for _, compact := range []bool{false, true} {
			for _, input := range []uint32{0, 1, 0x80000000, 0xaabbccdd, 0xffffffff} {
				var locals [49]uint32
				for x := 1; x <= 48; x++ {
					locals[x] = input ^ (uint32(x) * 0x9e3779b9)
				}
				for round := 0; round < 4; round++ {
					for x := 1; x <= 48; x++ {
						locals[x] += locals[(x+6)%48+1]
					}
				}
				var want uint32
				for x := 1; x <= 48; x++ {
					want ^= locals[x]
				}
				var stats ModuleStats
				opts := CompileOptions{CompactNative: compact}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, uint64(input))
				if err != nil || uint32(got) != want {
					t.Fatalf("enabled%v compact%v input%x got%x want%x err%v", enabled, compact, input, got, want, err)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Residency.Evictions == 0 || stats.Funcs[0].Residency.ActivationLoads == 0) {
					t.Fatalf("test did not exercise regional eviction and reload: %+v; %v", stats.Funcs[0].Residency, stats.Funcs[0].Peephole)
				}
			}
		}
	}
}
