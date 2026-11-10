//go:build linux && amd64

package amd64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

func TestPredicateShiftPreservesSource(t *testing.T) {
	saved, scalar := predicateShiftEnabled, sharedScalarEnabled
	sharedScalarEnabled = false
	defer func() { predicateShiftEnabled, sharedScalarEnabled = saved, scalar }()
	for shift, name := range []string{"shl", "shr_s", "shr_u", "rotl", "rotr"} {
		for _, alias := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/alias=%v", name, alias), func(t *testing.T) {
				// The predicate reads the same local as the shift. A local.set destination
				// must not write that local before the predicate reads its original value.
				body := []byte{0, 0x20, 0, 0x20, 0, 0x20, 1, 0x48, byte(0x74 + shift)}
				if alias {
					body = append(body, 0x21, 0, 0x20, 0)
				} else {
					body = append(body, 0x20, 0, 0x73)
				}
				body = append(body, 0x0b)
				m := mod1(t, []wasm.ValType{i32, i32}, []wasm.ValType{i32}, body)
				for _, on := range []bool{false, true} {
					predicateShiftEnabled = on
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["predicate-shift"] != 0) != on {
						t.Fatalf("on=%v hits=%v", on, stats.Funcs[0].Peephole)
					}
					for _, x := range []uint64{0, 1, 0x7fffffff, 0x80000000, 0xffffffff} {
						for _, y := range []uint64{0, 1, 0x7fffffff, 0x80000000, 0xffffffff} {
							count := uint64(0)
							if int32(x) < int32(y) {
								count = 1
							}
							want := shiftResult(32, shift, x, count)
							if !alias {
								want ^= x
							}
							if got := runCompiledAmd64u(t, cm, x, y); got != want {
								t.Fatalf("on=%v x=%x y=%x got=%x want=%x", on, x, y, got, want)
							}
						}
					}
				}
			})
		}
	}
}

func TestPredicateShiftTrapOrder(t *testing.T) {
	saved, scalar := predicateShiftEnabled, sharedScalarEnabled
	sharedScalarEnabled = false
	defer func() { predicateShiftEnabled, sharedScalarEnabled = saved, scalar }()
	// The shifted value divides first. The predicate divides second, even when
	// its boolean result would select the unchanged value.
	body := []byte{0, 0x20, 0, 0x20, 1, 0x6d, 0x20, 2, 0x20, 3, 0x6d, 0x45, 0x74, 0x0b}
	m := modMem(t, 1, []wasm.ValType{i32, i32, i32, i32}, []wasm.ValType{i32}, body)
	for _, on := range []bool{false, true} {
		predicateShiftEnabled = on
		for _, guard := range []bool{false, true} {
			for _, tc := range []struct {
				args [4]uint64
				want runtime.TrapCode
			}{
				{[4]uint64{0x80000000, 0xffffffff, 1, 0}, runtime.TrapDivOverflow},
				{[4]uint64{1, 0, 0x80000000, 0xffffffff}, runtime.TrapDivZero},
				{[4]uint64{7, 1, 1, 0}, runtime.TrapDivZero},
				{[4]uint64{7, 1, 0x80000000, 0xffffffff}, runtime.TrapDivOverflow},
			} {
				_, _, err := runMemAmd64WithOptions(t, m, CompileOptions{ElideBoundsChecks: guard}, nil, tc.args[:]...)
				var trap *runtime.TrapError
				if !errors.As(err, &trap) || trap.Code != tc.want {
					t.Fatalf("on=%v guard=%v args=%x trap=%v want=%v", on, guard, tc.args, err, tc.want)
				}
			}
		}
	}
}

func TestPredicateShiftFixedRegistersAndPressure(t *testing.T) {
	saved, scalar := predicateShiftEnabled, sharedScalarEnabled
	sharedScalarEnabled = false
	defer func() { predicateShiftEnabled, sharedScalarEnabled = saved, scalar }()
	const depth = 24
	for _, predicate := range []string{"div", "shift", "eqz", "inverse"} {
		t.Run(predicate, func(t *testing.T) {
			body := []byte{0}
			for level := 0; level < depth; level++ {
				body = append(body, 0x20, 0, 0x41, byte(level+1), 0x73)
			}
			body = append(body, 0x20, 1)
			if predicate != "eqz" {
				body = append(body, 0x20, 2)
				if predicate == "div" {
					body = append(body, 0x6e)
				} else {
					body = append(body, 0x74)
				}
				body = append(body, 0x20, 0, 0x49)
				if predicate == "inverse" {
					body = append(body, 0x45)
				}
			} else {
				body = append(body, 0x45)
			}
			body = append(body, 0x74)
			for level := depth - 2; level >= 0; level-- {
				body = append(body, 0x73)
			}
			body = append(body, 0x0b)
			m := mod1(t, []wasm.ValType{i32, i32, i32}, []wasm.ValType{i32}, body)
			for _, on := range []bool{false, true} {
				predicateShiftEnabled = on
				var stats ModuleStats
				cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["predicate-shift"] != 0) != on {
					t.Fatalf("on=%v hits=%v", on, stats.Funcs[0].Peephole)
				}
				for _, args := range [][3]uint64{{0x81234567, 0, 3}, {0x71234567, 0xffffffff, 3}, {0, 1, 33}, {0xffffffff, 0x80000000, 31}} {
					count := uint64(0)
					p := args[1]
					if predicate == "div" {
						p /= args[2]
					} else if predicate == "shift" || predicate == "inverse" {
						p = uint64(uint32(p) << (args[2] & 31))
					}
					if predicate == "eqz" {
						if p == 0 {
							count = 1
						}
					} else if (p < args[0]) != (predicate == "inverse") {
						count = 1
					}
					want := shiftResult(32, 0, args[0]^depth, count)
					for level := depth - 2; level >= 0; level-- {
						want ^= args[0] ^ uint64(level+1)
					}
					if got := runCompiledAmd64u(t, cm, args[:]...); got != want {
						t.Fatalf("on=%v args=%x got=%x want=%x", on, args, got, want)
					}
				}
			}
		})
	}
}
