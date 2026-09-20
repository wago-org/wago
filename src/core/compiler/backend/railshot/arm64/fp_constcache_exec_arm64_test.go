//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestFloatConstCacheInlineBranchExecARM64(t *testing.T) {
	// The caller has no float literal, so its hint skips preload. Inlining
	// introduces 100 in each arm after the function becomes call-free.
	// Installing it in the then arm leaves the else arm uninitialized.
	callee := appendF64ConstForCacheTest([]byte{0x00, 0x20, 0x00, 0xbf}, 100)
	callee = append(callee, 0xa2, 0xbd, 0x0b) // f64.mul; i64.reinterpret_f64; end
	m := modFuncs(t,
		funcDef{
			params: []wasm.ValType{wasm.I32, wasm.I64}, results: []wasm.ValType{wasm.I64},
			body: []byte{0x00,
				0x20, 0x00, 0x04, 0x7e, // local.get 0; if (result i64)
				0x20, 0x01, 0x10, 0x01, // local.get 1; call 1
				0x05,                   // else
				0x20, 0x01, 0x10, 0x01, // local.get 1; call 1
				0x0b, 0x0b}, // end; end
		},
		funcDef{params: []wasm.ValType{wasm.I64}, results: []wasm.ValType{wasm.I64}, body: callee},
	)
	for _, branch := range []uint64{0, 1, 0} {
		stats := &ModuleStats{}
		got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Stats: stats, Optimizations: map[string]bool{"inline": true, "inline-callfree": true}}, branch, math.Float64bits(2))
		if err != nil {
			t.Fatal(err)
		}
		if stats.Funcs[0].Peephole["all-calls-inlined"] == 0 {
			t.Fatal("caller did not become call-free through inlining")
		}
		if want := math.Float64bits(200); got != want {
			t.Errorf("branch %d: result bits = %#x, want %#x", branch, got, want)
		}
	}
}
