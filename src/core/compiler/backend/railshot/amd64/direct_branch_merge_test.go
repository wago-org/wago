//go:build linux && amd64

package amd64

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestDirectUnconditionalMerge(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64} {
		var valueType, add byte
		input, want := uint64(7), uint64(21)
		switch typ {
		case wasm.I32:
			valueType, add = 0x7f, 0x6a
		case wasm.I64:
			valueType, add = 0x7e, 0x7c
		case wasm.F32:
			valueType, add = 0x7d, 0x92
			input, want = uint64(math.Float32bits(1.25)), uint64(math.Float32bits(3.75))
		case wasm.F64:
			valueType, add = 0x7c, 0xa0
			input, want = math.Float64bits(1.25), math.Float64bits(3.75)
		}
		// Keep one base operand. The branch exits two blocks, discarding an inner
		// base operand and transporting a deferred arithmetic result to the outer join.
		body := []byte{0, 0x20, 0, 0x02, valueType, 0x20, 0, 0x02, valueType,
			0x20, 0, 0x20, 0, add, 0x0c, 1, 0x0b, 0x0b, add, 0x0b}
		m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
		for _, on := range []bool{false, true} {
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), CompactNative: true, Optimizations: map[string]bool{"reg-merge": true, "direct-int-branch-merge": on}})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if diagnosticsEnabled {
				if (stats.Funcs[0].Peephole["direct-int-branch-merge"] != 0) != (on && (typ == wasm.I32 || typ == wasm.I64)) {
					t.Fatalf("type=%v enabled=%t: missing expected admission", typ, on)
				}
			}
			if got := runCompiledAmd64u(t, cm, input); got != want {
				t.Fatalf("type=%v enabled=%t: got=%x want=%x", typ, on, got, want)
			}
		}
	}
}
