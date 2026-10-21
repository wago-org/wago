//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestVariableShiftDeferredPressure(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, alias := range []bool{false, true} {
			for _, depth := range []int{6, 32} {
				for kind := byte(0); kind < 3; kind++ {
					t.Run(fmt.Sprintf("wide%v/alias%v/depth%d/kind%d", wide, alias, depth, kind), func(t *testing.T) {
						typ, constant, add, mul, shift := wasm.I32, byte(0x41), byte(0x6a), byte(0x6c), byte(0x74)
						if wide {
							typ, constant, add, mul, shift = wasm.I64, 0x42, 0x7c, 0x7e, 0x86
						}
						body := []byte{1, 1, byte(0x7f), 0x3f, 0, 0x1a} // Use ordinary scalar lowering.
						if wide {
							body[2] = 0x7e
						}
						const live = 32
						for i := byte(1); i <= live; i++ {
							body = append(body, 0x20, 0, constant, i, add, 0x22, 2)
						}
						body = append(body, 0x20, 0, constant, 2, mul) // Deferred left operand.
						countLocal := byte(1)
						if alias {
							countLocal = 0
						}
						var tree func(int)
						tree = func(d int) {
							if d == 0 {
								body = append(body, 0x20, countLocal, constant, 1, add)
								return
							}
							tree(0)
							tree(d - 1)
							body = append(body, add)
						}
						tree(depth)
						body = append(body, 0x20, countLocal, add, shift+kind)
						for i := 0; i < live; i++ {
							body = append(body, add)
						}
						// Both source locals remain live after the shift, including the alias case.
						body = append(body, 0x20, 0, add, 0x20, 1, add, 0x0b)
						m := modMem(t, 1, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
						if err := wasm.ValidateModule(m); err != nil {
							t.Fatal(err)
						}
						var stats ModuleStats
						opts := CompileOptions{}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						for _, args := range [][2]uint64{{3, 5}, {0x80000001, 63}, {0x8000000000000001, 127}, {^uint64(0), ^uint64(0)}} {
							x, c := args[0], args[1]
							if alias {
								c = x
							}
							count := (uint64(depth+1)*(c+1) + c) & 63
							value := x * 2
							if !wide {
								count &= 31
								value = uint64(uint32(value))
							}
							want := value << count
							if kind == 1 {
								if wide {
									want = uint64(int64(value) >> count)
								} else {
									want = uint64(uint32(int32(value) >> count))
								}
							} else if kind == 2 {
								want = value >> count
							}
							want += live*x + live*(live+1)/2 + args[0] + args[1]
							got, err := runArm64WrapperWithOptions(t, m, opts, args[:]...)
							if !wide {
								got, want = uint64(uint32(got)), uint64(uint32(want))
							}
							if err != nil || got != want {
								t.Fatalf("args=%x got=%x want=%x err=%v", args, got, want, err)
							}
							if diagnosticsEnabled {
								s := stats.Funcs[0]
								if s.Peephole["variable-shift-three-operand"] != 1 || s.Spills == 0 {
									t.Fatalf("path/pressure not active: %+v", s)
								}
								t.Logf("code=%d spills=%d reloads=%d selected=%d", s.CodeBytes, s.Spills, s.Reloads, s.Peephole["variable-shift-three-operand"])
							}
						}
					})
				}
			}
		}
	}
}

func TestVariableShiftTrapOrder(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, div, load, shift, align := wasm.I32, byte(0x6d), byte(0x28), byte(0x74), byte(2)
		min, minusOne := uint64(1<<31), uint64(0xffffffff)
		if wide {
			typ, div, load, shift, align = wasm.I64, 0x7f, 0x29, 0x86, 3
			min, minusOne = 1<<63, ^uint64(0)
		}
		body := []byte{0, 0x20, 0, 0x20, 1, div, 0x20, 2, load, align, 0, shift, 0x0b}
		m := modMem(t, 1, []wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{typ}, body)
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct {
			n, d uint64
			want rt.TrapCode
		}{{1, 0, rt.TrapDivZero}, {min, minusOne, rt.TrapDivOverflow}, {12, 3, rt.TrapLinMemOutOfBounds}} {
			t.Run(fmt.Sprintf("wide%v/divisor%x", wide, test.d), func(t *testing.T) {
				// Explicit bounds checks use the ordinary wrapper and memory helper.
				_, err := runArm64Wrapper(t, m, test.n, test.d, 65536)
				var trap *rt.TrapError
				if !errors.As(err, &trap) || trap.Code != test.want {
					t.Fatalf("got %v want %v", err, test.want)
				}
			})
		}
	}
}
