//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestWideQuotientSinkPreservesDividendAndDirtyCarriers(t *testing.T) {
	old := wideQuotientSinkEnabled
	t.Cleanup(func() { wideQuotientSinkEnabled = old })
	for _, divisor := range []uint32{7, 19, 31, 97, 127} {
		for _, declared := range []bool{false, true} {
			source := byte(0)
			if declared {
				source = 1
			}
			// Both paths retain the dividend after assigning the quotient to a
			// separate local. Only the declared local has canonical upper bits.
			body := []byte{1, 2, 0x7f, 0x3f, 0, 0x1a, 0x20, 0, 0x21, 1, 0x03, 0x40, 0x20, source, 0x41}
			body = append(body, wasmtest.SLEB32(int32(divisor))...)
			body = append(body, 0x6e, 0x21, 2, 0x41, 0, 0x0d, 0, 0x0b, 0x20, source, 0x20, 2, 0x6a, 0x0b)
			m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
			for _, enabled := range []bool{false, true} {
				wideQuotientSinkEnabled = enabled
				for _, facts := range []bool{false, true} {
					for _, n := range []uint32{0, 1, divisor - 1, divisor, divisor + 1, 0x7fffffff, 0x80000000, 0xffffffff} {
						var stats ModuleStats
						opts := CompileOptions{Optimizations: map[string]bool{"value-facts": facts, "linear-integer-tree": false, "loop-int-const": false, "scoped-loop-int-const": false, "interval-region-pins": false}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, uint64(n)|0xdeadbeef00000000)
						if want := uint64(n + n/divisor); err != nil || got != want {
							t.Fatalf("d=%d declared=%v enabled=%v facts=%v n=%x got=%x want=%x err=%v", divisor, declared, enabled, facts, n, got, want, err)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["wide-quotient-sink"] > 0) != enabled {
							t.Fatalf("d=%d declared=%v enabled=%v stats=%v", divisor, declared, enabled, stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}
