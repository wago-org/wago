//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestGuardedDivRemUsesImmediateNonzeroPath(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, eqz, first := wasm.I32, byte(0x7f), byte(0x45), byte(0x6d)
		mask, minimum := uint64(0xffffffff), uint64(0x80000000)
		if wide {
			typ, bt, eqz, first = wasm.I64, 0x7e, 0x50, 0x7f
			mask = ^uint64(0)
			minimum = 0x8000000000000000
		}
		for operation := byte(0); operation < 4; operation++ {
			for _, destination := range []byte{0, 1, 3} {
				body := []byte{1, 1, bt, 0x02, 0x40, 0x03, 0x40, 0x20, 1, eqz, 0x0d, 1, 0x20, 0, 0x20, 1, first + operation, 0x21, destination, 0x0c, 1, 0x0b, 0x0b, 0x20, destination, 0x0b}
				params, result := []wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{typ}
				m := modFuncs(t, funcDef{params, result, body}, funcDef{params, result, []byte{0, 0x20, 0, 0x0b}})
				for _, enabled := range []bool{false, true} {
					for _, workers := range []int{1, 4} {
						for _, input := range [][2]uint64{{17, 5}, {minimum, mask}, {mask, 7}, {17, 0}} {
							a, b := input[0]&mask, input[1]&mask
							want := a
							if b == 0 {
								switch destination {
								case 1:
									want = b
								case 3:
									want = 0
								}
							} else if operation%2 == 1 {
								if operation == 1 {
									want = a / b
								} else {
									want = a % b
								}
							} else {
								x, y := int64(a), int64(b)
								if !wide {
									x, y = int64(int32(a)), int64(int32(b))
								}
								if operation == 0 {
									want = uint64(x/y) & mask
								} else {
									want = uint64(x%y) & mask
								}
							}
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"guarded-div-rem": enabled}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							aArg, bArg := a, b
							if !wide {
								aArg |= 0xdeadbeef00000000
								bArg |= 0xcafebabe00000000
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, aArg, bArg, 1)
							if operation == 0 && a == minimum && b == mask {
								var trap *rt.TrapError
								if !errors.As(err, &trap) || trap.Code != rt.TrapDivOverflow {
									t.Fatalf("overflow missing: %v", err)
								}
								continue
							}
							if err != nil || got&mask != want {
								t.Fatalf("wide=%t op=%d destination=%d enabled=%t workers=%d args=%x got=%x err=%v want=%x", wide, operation, destination, enabled, workers, input, got, err, want)
							}
							if diagnosticsEnabled && (stats.Funcs[0].Peephole["guarded-div-rem"] != 0) != (enabled && !profileEnabled) {
								t.Fatalf("path mismatch wide=%t op=%d dst=%d enabled=%t stats=%v", wide, operation, destination, enabled, stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}

func TestGuardedDivRemRejectsJoinsMutationsAndWrongSource(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, k, eqz, div := wasm.I32, byte(0x41), byte(0x45), byte(0x6e)
		if wide {
			typ, k, eqz, div = wasm.I64, 0x42, 0x50, 0x80
		}
		for _, variant := range []string{"wrong-guard", "write", "later-operation", "join", "nested-block"} {
			body := []byte{0, 0x02, 0x40, 0x03, 0x40}
			if variant == "join" {
				body = append(body, 0x20, 2, 0x04, 0x40)
			}
			source, depth := byte(1), byte(1)
			if variant == "wrong-guard" {
				source = 0
			}
			if variant == "join" {
				depth = 2
			}
			body = append(body, 0x20, source, eqz, 0x0d, depth)
			if variant == "write" {
				body = append(body, k, 0, 0x21, 1)
			}
			if variant == "join" {
				body = append(body, 0x0b)
			}
			if variant == "nested-block" {
				body = append(body, 0x02, 0x40, 0x01, 0x0b)
			}
			body = append(body, 0x20, 0, 0x20, 1, div, 0x21, 0)
			if variant == "later-operation" {
				body = append(body, k, 0, 0x21, 1, 0x20, 0, 0x20, 1, div, 0x21, 0)
			}
			body = append(body, 0x0c, 1, 0x0b, 0x0b, 0x20, 0, 0x0b)
			params, result := []wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{typ}
			m := modFuncs(t, funcDef{params, result, body}, funcDef{params, result, []byte{0, 0x20, 0, 0x0b}})
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, flag := range []uint64{0, 1} {
						for _, b := range []uint64{0, 5} {
							want, trap := uint64(17), false
							skipped := variant != "wrong-guard" && b == 0 && (variant != "join" || flag != 0)
							if !skipped {
								trap = b == 0 || variant == "write" || variant == "later-operation"
								if !trap {
									want = 17 / b
								}
							}
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"guarded-div-rem": enabled}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, 17, b, flag)
							if trap {
								var e *rt.TrapError
								if !errors.As(err, &e) || e.Code != rt.TrapDivZero {
									t.Fatalf("wide=%t variant=%s enabled=%t b=%d flag=%d missing trap: %v", wide, variant, enabled, b, flag, err)
								}
							} else if err != nil || got != want {
								t.Fatalf("wide=%t variant=%s enabled=%t b=%d flag=%d got=%d err=%v want=%d", wide, variant, enabled, b, flag, got, err, want)
							}
							if diagnosticsEnabled {
								wantHit := 0
								if variant == "later-operation" && enabled && !profileEnabled {
									wantHit = 1
								}
								if stats.Funcs[0].Peephole["guarded-div-rem"] != wantHit {
									t.Fatalf("proof escaped source region: variant=%s stats=%v", variant, stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestGuardedDivRemDoesNotConfuseI64GuardWithI32Divisor(t *testing.T) {
	params, result := []wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32}
	body := []byte{0, 0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x50, 0x0d, 1, 0x20, 0, 0x20, 1, 0xa7, 0x6e, 0x21, 0, 0x0c, 1, 0x0b, 0x0b, 0x20, 0, 0x0b}
	m := modFuncs(t, funcDef{params, result, body}, funcDef{params, result, []byte{0, 0x20, 0, 0x0b}})
	for _, enabled := range []bool{false, true} {
		_, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"guarded-div-rem": enabled}}, 17, 1<<32)
		var trap *rt.TrapError
		if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
			t.Fatalf("width mismatch removed trap: %v", err)
		}
	}
}

func TestGuardedUnsignedRemainderLoopUnroll(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, eqz, rem, add, mul := wasm.I32, byte(0x7f), byte(0x41), byte(0x45), byte(0x70), byte(0x6a), byte(0x6c)
		mask := uint64(0xffffffff)
		if wide {
			typ, bt, k, eqz, rem, add, mul = wasm.I64, 0x7e, 0x42, 0x50, 0x82, 0x7c, 0x7e
			mask = ^uint64(0)
		}
		body := []byte{1, 2, bt, 0x02, 0x40, 0x03, 0x40, 0x20, 1, eqz, 0x0d, 1, 0x20, 0, 0x20, 1, rem, 0x21, 3, 0x20, 1, 0x21, 0, 0x20, 3, 0x21, 1, 0x20, 2, k, 1, add, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x20, 0, k, 17, mul, 0x20, 2, add, 0x0b}
		params, result := []wasm.ValType{typ, typ}, []wasm.ValType{typ}
		m := modFuncs(t, funcDef{params, result, body}, funcDef{params, result, []byte{0, 0x20, 0, 0x0b}})
		for _, guard := range []bool{false, true} {
			for _, unroll := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, input := range [][2]uint64{{0, 0}, {0, 1}, {17, 5}, {mask, 7}, {mask - 1, mask}, {123456789012345, 9876543210}} {
						a, b, steps := input[0]&mask, input[1]&mask, uint64(0)
						for b != 0 {
							a, b = b, a%b
							steps++
						}
						want := (17*a + steps) & mask
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"guarded-div-rem": guard, "scalar-loop-unroll": unroll}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, input[:]...)
						if err != nil || got&mask != want {
							t.Fatalf("wide=%t guard=%t unroll=%t workers=%d input=%x got=%x err=%v want=%x", wide, guard, unroll, workers, input, got, err, want)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-loop-unroll-counter"] != 0) != (guard && unroll && !profileEnabled) {
							t.Fatalf("unroll path mismatch wide=%t guard=%t unroll=%t stats=%v", wide, guard, unroll, stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}
