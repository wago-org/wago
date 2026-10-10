//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestIntervalRegionsCanonicalizeBeforeCallsAndBulk(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		body := []byte{1, 32, 0x7f}
		for x := byte(1); x <= 32; x++ {
			body = append(body, 0x20, 0, 0x41)
			body = append(body, wasmtest.SLEB32(int32(x))...)
			body = append(body, 0x6a, 0x21, x)
		}
		// Both sides of the call read every local. The live sum also crosses the
		// bulk operation, exercising operand canonicalization before scratch use.
		body = append(body, 0x41, 0)
		for x := byte(1); x <= 32; x++ {
			body = append(body, 0x20, x, 0x6a)
		}
		if bulk {
			body = append(body, 0x41, 0, 0x41, 7, 0x41, 16, 0xfc, 11, 0)
		}
		body = append(body, 0x10, 1)
		for x := byte(1); x <= 32; x++ {
			body = append(body, 0x20, x, 0x6a)
		}
		body = append(body, 0x0b)
		callee := []byte{1, 32, 0x7f}
		for x := byte(1); x <= 32; x++ {
			callee = append(callee, 0x20, 0, 0x41, x, 0x6a, 0x21, x)
		}
		callee = append(callee, 0x41, 0)
		for x := byte(1); x <= 32; x++ {
			callee = append(callee, 0x20, x, 0x6a)
		}
		callee = append(callee, 0x41, 17, 0x6a, 0x0b)
		m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: body}, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: callee})
		if bulk {
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
		}
		for _, enabled := range []bool{false, true} {
			for _, guard := range []bool{false, true} {
				for _, input := range []uint64{0, 1, 0x7fffffff, 0xffffffff} {
					opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"inline": false, "interval-region-pins": true, "interval-call-regions": enabled}}
					var stats ModuleStats
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, input)
					want := uint64(uint32(33*(input*32+32*33/2) + 32*33/2 + 17))
					if err != nil || uint64(uint32(got)) != want {
						t.Fatalf("bulk=%v enabled=%v guard=%v input=%x got=%x want=%x err=%v", bulk, enabled, guard, input, got, want, err)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["interval-call-regions"] != 0) != enabled {
						t.Fatalf("missing admission bulk=%v enabled=%v stats=%v", bulk, enabled, stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}
