//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// More than eight parameters select the wrapper entry. Reading every parameter
// after the call fills the whole-function pin pool. A computed stack prefix must
// also survive while each distinct integer result is checked in reverse order.
func integerResultPressureModule(t testing.TB, count, paramCount int) (*wasm.Module, []uint64) {
	t.Helper()
	params := make([]wasm.ValType, paramCount)
	args := make([]uint64, paramCount)
	for i := range params {
		params[i] = wasm.I64
		args[i] = uint64(100 + i)
	}
	caller := []byte{0, 0x20, 0, 0x42, 1, 0x7c} // local.get 0; i64.const 1; i64.add
	caller = append(caller, 0x10, 1)            // call result producer
	callee := []byte{0}
	results := make([]wasm.ValType, count)
	for i := range results {
		results[i] = wasm.I64
		callee = append(callee, 0x42)
		callee = append(callee, wasmtest.SLEB64(int64(1000+i*17))...)
	}
	callee = append(callee, 0x0b)
	check := func(want uint64) {
		caller = append(caller, 0x42)
		caller = append(caller, wasmtest.SLEB64(int64(want))...)
		caller = append(caller, 0x52, 0x04, 0x40, 0x00, 0x0b) // i64.ne; if; unreachable; end
	}
	for i := count - 1; i >= 0; i-- {
		check(uint64(1000 + i*17))
	}
	check(args[0] + 1)
	for i, value := range args {
		caller = append(caller, 0x20, byte(i))
		check(value)
	}
	caller = append(caller, 0x42, 42, 0x0b)
	m := modFuncs(t,
		funcDef{params: params, results: []wasm.ValType{wasm.I64}, body: caller},
		funcDef{results: results, body: callee},
	)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m, args
}

func TestIntegerCallResultPressureARM64(t *testing.T) {
	for _, count := range []int{3, 4, 5, 6, 7, 8} {
		for _, params := range []int{1, 24} {
			for _, stackReg := range []bool{false, true} {
				t.Run(fmt.Sprintf("results=%d/params=%d/stack-reg=%t", count, params, stackReg), func(t *testing.T) {
					m, args := integerResultPressureModule(t, count, params)
					var stats ModuleStats
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{
						Stats: &stats, DeferCodeMapping: true,
						Optimizations: map[string]bool{"inline": false, "stack-reg": stackReg},
					}, args...)
					if err != nil || got != 42 {
						t.Fatalf("result = %d, %v; want 42", got, err)
					}
					if diagnosticsEnabled {
						s := stats.Funcs[0]
						t.Logf("pins=%d frame=%d spill-slots=%d calls=%v", s.PinnedLocals, s.FrameBytes, s.MaxSpillSlots, s.Calls)
						if params == 24 && s.PinnedLocals < 11 {
							t.Fatalf("fixture lost whole-function pin pressure: %d", s.PinnedLocals)
						}
					}
				})
			}
		}
	}
}
