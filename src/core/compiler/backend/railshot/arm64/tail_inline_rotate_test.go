//go:build (linux || darwin) && arm64

package arm64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestTailLoopInlineRotatingPair(t *testing.T) {
	if profileEnabled {
		t.Skip("tail-loop inlining excludes profiling")
	}
	old := tailLoopInlineEnabled
	tailLoopInlineEnabled = true
	defer func() { tailLoopInlineEnabled = old }()
	for _, wide := range []bool{false, true} {
		typ, block, eqz := wasm.I32, byte(0x7f), byte(0x45)
		if wide {
			typ, block, eqz = wasm.I64, 0x7e, 0x50
		}
		for _, op := range []byte{0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f, 0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78} {
			instruction := op
			if wide {
				instruction += 0x12
			}
			params, results := []wasm.ValType{typ, typ}, []wasm.ValType{typ}
			body := []byte{0, 0x20, 1, eqz, 0x04, block, 0x20, 0, 0x05, 0x20, 1, 0x20, 0, 0x20, 1, instruction, 0x12, 1, 0x0b, 0x0b}
			m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b}}, funcDef{params, results, body})
			// Choose terminating recurrences for every accepted operation. The
			// zero-input case also checks the unmodified initial return value.
			inputs := [][2]uint64{{17, 0}}
			switch op {
			case 0x6a:
				a := uint64(0xffffffff)
				if wide {
					a = ^uint64(0)
				}
				inputs = append(inputs, [2]uint64{a, 1})
			case 0x6b, 0x73:
				inputs = append(inputs, [2]uint64{17, 17})
			case 0x6c, 0x6d, 0x6e, 0x71, 0x74, 0x75, 0x76, 0x77, 0x78:
				inputs = append(inputs, [2]uint64{0, 7})
			case 0x6f, 0x70:
				inputs = append(inputs, [2]uint64{17, 5}, [2]uint64{5, 17}, [2]uint64{75025, 46368}, [2]uint64{0, 31}, [2]uint64{1, 1})
			case 0x72: // OR is non-terminating unless the initial divisor is zero.
			}
			for _, pair := range inputs {
				want := pair[1]
				if pair[1] == 0 {
					want = pair[0]
				} else if op == 0x6f || op == 0x70 {
					a, b := pair[0], pair[1]
					for b != 0 {
						a, b = b, a%b
					}
					want = a
				}
				var stats ModuleStats
				opts := CompileOptions{}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, pair[0], pair[1])
				if err != nil || got != want {
					t.Fatalf("wide=%t op=%x input=%v got=%d,%v want %d", wide, op, pair, got, err, want)
				}
				if diagnosticsEnabled && stats.Funcs[0].Peephole["inline-rotating-tail"] == 0 {
					t.Fatal("rotating transfer was not selected")
				}
			}
		}
	}
}

func TestTailLoopInlineRotatingDivisionTrapsAndSigns(t *testing.T) {
	if profileEnabled {
		t.Skip("tail-loop inlining excludes profiling")
	}
	old := tailLoopInlineEnabled
	tailLoopInlineEnabled = true
	defer func() { tailLoopInlineEnabled = old }()
	for _, wide := range []bool{false, true} {
		typ, block, eqz := wasm.I32, byte(0x7f), byte(0x45)
		min, minusOne := uint64(0x80000000), uint64(0xffffffff)
		if wide {
			typ, block, eqz, min, minusOne = wasm.I64, 0x7e, 0x50, 1<<63, ^uint64(0)
		}
		for _, op := range divOps(wide) {
			params, results := []wasm.ValType{typ, typ}, []wasm.ValType{typ}
			m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x10, 1, 0x0b}},
				funcDef{params, results, []byte{0, 0x20, 1, eqz, 0x04, block, 0x20, 0, 0x05, 0x20, 1, 0x20, 0, 0x20, 1, op.opcode, 0x12, 1, 0x0b, 0x0b}})
			inputs := [][2]uint64{{17, 5}, {5, 17}, {min, 0}, {min, minusOne}}
			if op.rem {
				inputs = append(inputs, [2]uint64{minusOne - 16, 5}, [2]uint64{17, minusOne - 4}, [2]uint64{7540113804746346429, 4660046610375530309})
			}
			for _, pair := range inputs {
				a, b := pair[0], pair[1]
				if !wide {
					a, b = uint64(uint32(a)), uint64(uint32(b))
				}
				overflow := false
				for steps := 0; b != 0; steps++ {
					if steps > 1000 {
						t.Fatalf("nonterminating test fixture: %s %v", op.name, pair)
					}
					if op.signed && !op.rem && a == min && b == minusOne {
						overflow = true
						break
					}
					a, b = b, refDivRem(op, wide, a, b)
				}
				for _, optimized := range []bool{false, true} {
					var stats ModuleStats
					opts := CompileOptions{Optimizations: map[string]bool{"borrowed-div-rem": optimized}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, pair[0], pair[1])
					if overflow {
						var trap *rt.TrapError
						if !errors.As(err, &trap) || trap.Code != rt.TrapDivOverflow {
							t.Fatalf("%s pair=%v optimized=%t error=%v want overflow", op.name, pair, optimized, err)
						}
					} else if err != nil || got != a {
						t.Fatalf("%s pair=%v optimized=%t got=%x,%v want %x", op.name, pair, optimized, got, err, a)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-rotating-tail"] > 0) != optimized {
						t.Fatal("rotation option fallback was not exercised")
					}
				}
			}
		}
	}
}
