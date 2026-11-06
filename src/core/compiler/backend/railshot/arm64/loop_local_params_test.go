//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestLoopLocalParametersPreserveInitialTrapOrder(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, divS, divU, eqz, sub := wasm.I32, byte(0x7f), byte(0x41), byte(0x6d), byte(0x6e), byte(0x45), byte(0x6b)
		minimum := uint64(0x80000000)
		if wide {
			typ, bt, k, divS, divU, eqz, sub = wasm.I64, 0x7e, 0x42, 0x7f, 0x80, 0x50, 0x7d
			minimum = 0x8000000000000000
		}
		body := []byte{0, 0x20, 0, 0x20, 1, divS, 0x20, 0, 0x20, 2, divU,
			0x03, 1, 0x21, 2, 0x21, 0, 0x20, 0, eqz, 0x04, bt, 0x20, 2,
			0x05, 0x20, 0, k, 1, sub, 0x20, 2, 0x0c, 1, 0x0b, 0x0b, 0x0b}
		m := modFuncs(t,
			funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, body},
			funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x0b}})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				var stats ModuleStats
				opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": false, "loop-local-params": enabled}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, 17, 5, 3)
				if err != nil || got != 5 {
					t.Fatalf("wide=%t enabled=%t workers=%d got=%d err=%v", wide, enabled, workers, got, err)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["loop-local-params"] != 0) != (enabled && !profileEnabled) {
					t.Fatal("did not exercise parameter transfer")
				}
				for _, tc := range []struct {
					input [3]uint64
					code  rt.TrapCode
				}{{[3]uint64{minimum, ^uint64(0), 0}, rt.TrapDivOverflow}, {[3]uint64{17, 5, 0}, rt.TrapDivZero}} {
					_, err := runArm64WrapperWithOptions(t, m, opts, tc.input[:]...)
					var trap *rt.TrapError
					if !errors.As(err, &trap) || trap.Code != tc.code {
						t.Fatalf("wide=%t enabled=%t workers=%d input=%x err=%v want trap=%v", wide, enabled, workers, tc.input, err, tc.code)
					}
				}
			}
		}
	}
}

func TestLoopLocalParametersMixedWidthsAndFourWayTransfer(t *testing.T) {
	params, results := []wasm.ValType{wasm.I32, wasm.I64, wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}
	body := []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x03, 0,
		0x21, 3, 0x21, 2, 0x21, 1, 0x21, 0,
		0x20, 0, 0x45, 0x04, 0x7e,
		0x20, 1, 0x42, 3, 0x7e, 0x20, 3, 0x42, 7, 0x7e, 0x7c, 0x20, 2, 0xad, 0x7c,
		0x05, 0x20, 0, 0x41, 1, 0x6b, 0x20, 3, 0x20, 2, 0x41, 1, 0x6a, 0x20, 1, 0x0c, 1,
		0x0b, 0x0b, 0x0b}
	m := modFuncs(t, funcDef{params, results, body}, funcDef{params, results, []byte{0, 0x20, 1, 0x0b}})
	for _, enabled := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for count := uint64(0); count < 10; count++ {
					a, c := uint64(0x8000000000000001), ^uint64(0)
					if count&1 != 0 {
						a, c = c, a
					}
					want := 3*a + 7*c + uint64(uint32(0xfffffff0+count))
					var stats ModuleStats
					opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"loop-local-params": enabled, "reg-abi": regABI, "value-facts": false}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, count|0xdeadbeef00000000, 0x8000000000000001, 0xcafebeeffffffff0, ^uint64(0))
					if err != nil || got != want {
						t.Fatalf("enabled=%t regABI=%t workers=%d count=%d got=%x err=%v want=%x", enabled, regABI, workers, count, got, err, want)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["loop-local-params"] != 0) != (enabled && !profileEnabled) {
						t.Fatal("four-parameter path selection mismatch")
					}
				}
			}
		}
	}
}

func TestLoopIntegerParametersTransferToLocals(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, eqz, sub, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x45), byte(0x6b), byte(0x6a)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, eqz, sub, add = wasm.I64, 0x7e, 0x42, 0x50, 0x7d, 0x7c
			fold = func(v uint64) uint64 { return v }
		}
		caller := []byte{1, 2, bt, 0x20, 1, 0x20, 2, 0x20, 3, 0x10, 1, 0x21, 5, 0x21, 4,
			0x20, 0, 0x04, bt, 0x20, 5, 0x05, 0x20, 4, 0x0b, 0x0b}
		// Type1 is both the helper signature and the loop parameter/result type;
		// type2 is the zero-parameter tuple if. Rotate two values on each edge.
		for _, variant := range []string{"direct", "declared-locals", "nop-binding", "nested-loop", "conditional-backedge", "table-backedge", "large-body", "prefix", "call"} {
			helper := []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x03, 1,
				0x21, 2, 0x21, 1, 0x21, 0,
				0x20, 0, eqz, 0x04, 2, 0x20, 1, 0x20, 2, 0x05,
				0x20, 0, k, 1, sub, 0x20, 2, 0x20, 1, 0x20, 2, add, 0x0c, 1,
				0x0b, 0x0b, 0x0b}

			insert := func(at int, extra ...byte) {
				helper = append(append(append([]byte{}, helper[:at]...), extra...), helper[at:]...)
			}
			switch variant {
			case "declared-locals":
				// Use declared homes for both rotating values, exercising the
				// canonical I32 assignment path as well as incoming parameters.
				for i := 9; i < len(helper); i++ {
					if helper[i] == 0x20 || helper[i] == 0x21 {
						i++
						if helper[i] == 1 || helper[i] == 2 {
							helper[i] += 2
						}
					}
				}
				helper = append([]byte{1, 2, bt}, helper[1:]...)
			case "nop-binding":
				insert(9, 0x01)
			case "nested-loop":
				insert(15, 0x03, 0x40, 0x01, 0x0b)
			case "large-body":
				extra := make([]byte, 130)
				for i := range extra {
					extra[i] = 0x01
				}
				insert(15, extra...)
			case "call":
				insert(15, 0x10, 3)
			case "conditional-backedge":
				helper = append(append(append([]byte{}, helper[:len(helper)-5]...), 0x41, 1, 0x0d, 1, 0x00), helper[len(helper)-3:]...)
			case "table-backedge":
				helper = append(append(append([]byte{}, helper[:len(helper)-5]...), 0x41, 0, 0x0e, 0, 1), helper[len(helper)-3:]...)
			case "prefix":
				insert(1, 0x41, 17)
				helper = append(append(append([]byte{}, helper[:len(helper)-1]...), 0x21, 1, 0x21, 2, 0x1a, 0x20, 2, 0x20, 1), 0x0b)
			}
			opaque := []byte{0}
			for range 20 {
				opaque = append(opaque, 0x01)
			}
			opaque = append(opaque, 0x0b)
			m := modFuncs(t,
				funcDef{[]wasm.ValType{wasm.I32, typ, typ, typ}, []wasm.ValType{typ}, caller},
				funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ, typ}, helper},
				funcDef{nil, []wasm.ValType{typ, typ}, []byte{0, k, 0, k, 0, 0x0b}}, funcDef{nil, nil, opaque})
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, count := range []uint64{0, 1, 2, 7, 12} {
						for _, input := range [][2]uint64{{3, 7}, {0xfffffff0, 0x80000001}, {^uint64(0), 0x8000000000000001}} {
							a, b := fold(input[0]), fold(input[1])
							for range count {
								a, b = b, fold(a+b)
							}
							for which, want := range []uint64{a, b} {
								var stats ModuleStats
								opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": false, "value-facts": variant == "declared-locals", "loop-local-params": enabled}}
								if diagnosticsEnabled {
									opts.Stats = &stats
								}
								got, err := runArm64WrapperWithOptions(t, m, opts, uint64(which), count, input[0], input[1])
								got = fold(got)
								if err != nil || got != want {
									t.Fatalf("wide=%t workers=%d count=%d input=%x result=%d got=%x err=%v want=%x", wide, workers, count, input, which, got, err, want)
								}
								if diagnosticsEnabled && (stats.Funcs[1].Peephole["loop-local-params"] != 0) != (enabled && !profileEnabled && (variant == "direct" || variant == "declared-locals" || variant == "prefix")) {
									t.Fatal("loop parameter path selection mismatch: " + variant)
								}
							}
						}
					}
				}
			}
		}
	}
}
