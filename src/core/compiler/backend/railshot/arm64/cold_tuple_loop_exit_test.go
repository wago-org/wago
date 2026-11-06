//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestColdTupleLoopExitPreservesRecurrence(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, eqz, sub, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x45), byte(0x6b), byte(0x6a)
		mask := uint64(0xffffffff)
		if wide {
			typ, bt, k, eqz, sub, add = wasm.I64, 0x7e, 0x42, 0x50, 0x7d, 0x7c
			mask = ^uint64(0)
		}
		for pick := byte(0); pick < 2; pick++ {
			caller := []byte{1, 2, bt, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x21, 4, 0x21, 3, 0x20, 3 + pick, 0x0b}
			helper := []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x03, 1, 0x21, 2, 0x21, 1, 0x21, 0, 0x20, 0, eqz, 0x04, 2, 0x20, 1, 0x20, 2, 0x05, 0x20, 0, k, 1, sub, 0x20, 2, 0x20, 1, 0x20, 2, add, 0x0c, 1, 0x0b, 0x0b, 0x0b}
			params, pair := []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ, typ}
			m := modFuncs(t, funcDef{params, []wasm.ValType{typ}, caller}, funcDef{params, pair, helper}, funcDef{nil, pair, []byte{0, k, 0, k, 0, 0x0b}})
			for _, enabled := range []bool{false, true} {
				for _, inline := range []bool{false, true} {
					for _, workers := range []int{1, 4} {
						for count := uint64(0); count < 14; count++ {
							for _, initial := range [][2]uint64{{0, 1}, {1, 0}, {mask, 7}, {0x8000000000000001, mask}} {
								a, b := initial[0]&mask, initial[1]&mask
								for range count {
									a, b = b, (a+b)&mask
								}
								want := a
								if pick == 1 {
									want = b
								}
								var stats ModuleStats
								opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"cold-tuple-loop-exit": enabled, "inline": inline}}
								if diagnosticsEnabled {
									opts.Stats = &stats
								}
								n := count
								if !wide {
									n |= 0xdeadbeef00000000
								}
								got, err := runArm64WrapperWithOptions(t, m, opts, n, initial[0], initial[1])
								if err != nil || got&mask != want {
									t.Fatalf("wide=%t pick=%d enabled=%t inline=%t workers=%d count=%d initial=%x got=%x err=%v want=%x", wide, pick, enabled, inline, workers, count, initial, got, err, want)
								}
								if diagnosticsEnabled {
									if (stats.Funcs[1].Peephole["scalar-loop-unroll-pretest"] != 0) != (enabled && !profileEnabled) {
										t.Fatalf("cold loop unroll path missing: enabled=%t stats=%v", enabled, stats.Funcs[1].Peephole)
									}
									if (stats.Funcs[1].Peephole["scalar-loop-unroll-pair"] != 0) != (enabled && !profileEnabled) {
										t.Fatalf("pair cover missing enabled=%t stats=%v", enabled, stats.Funcs[1].Peephole)
									}
									if (stats.Funcs[1].Peephole["scalar-loop-unroll-latch"] != 0) != (enabled && !profileEnabled) {
										t.Fatalf("latch path mismatch enabled=%t stats=%v", enabled, stats.Funcs[1].Peephole)
									}
									hit := false
									for _, s := range stats.Funcs {
										hit = hit || s.Peephole["cold-tuple-loop-exit"] != 0
									}
									if hit != (enabled && !profileEnabled) {
										t.Fatalf("cold exit path mismatch wide=%t enabled=%t inline=%t stats=%v", wide, enabled, inline, stats.Funcs)
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestColdTupleLoopExitEffectsAndFallbacks(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, eqz, sub, add, div := wasm.I32, byte(0x7f), byte(0x41), byte(0x45), byte(0x6b), byte(0x6a), byte(0x6e)
		mask := uint64(0xffffffff)
		if wide {
			typ, bt, k, eqz, sub, add, div = wasm.I64, 0x7e, 0x42, 0x50, 0x7d, 0x7c, 0x80
			mask = ^uint64(0)
		}
		for _, variant := range []string{"trapping-else", "then-expression", "local-write", "nested-else", "wrong-backedge", "prefix", "lazy-zero", "compact", "interruptible", "no-direct-homes", "unroll-off", "zero-branch-off", "counter-result", "post-locals"} {
			caller := []byte{1, 2, bt}
			if variant == "prefix" {
				caller = append(caller, k, 7)
			}
			caller = append(caller, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x21, 4, 0x21, 3, 0x20, 3)
			if variant == "prefix" {
				caller = append(caller, add)
			}
			caller = append(caller, 0x0b)
			helper := []byte{0}
			if variant == "lazy-zero" {
				helper = []byte{1, 1, bt}
			}
			helper = append(helper, 0x20, 0, 0x20, 1, 0x20, 2, 0x03, 1, 0x21, 2, 0x21, 1, 0x21, 0, 0x20, 0, eqz, 0x04, 2, 0x20)
			firstResult := byte(1)
			if variant == "counter-result" || variant == "post-locals" {
				firstResult = 0
			}
			helper = append(helper, firstResult)
			if variant == "then-expression" {
				helper = append(helper, k, 1, add)
			}
			secondResult := byte(2)
			if variant == "post-locals" {
				secondResult = 0
			}
			helper = append(helper, 0x20, secondResult, 0x05)
			if variant == "local-write" {
				helper = append(helper, 0x20, 1, k, 1, add, 0x21, 1)
			}
			if variant == "nested-else" {
				helper = append(helper, 0x02, 0x40, 0x01, 0x0b)
			}
			helper = append(helper, 0x20, 0, k, 1, sub, 0x20, 2, 0x20, 1)
			if variant == "trapping-else" {
				helper = append(helper, k, 0, div)
			} else {
				helper = append(helper, 0x20, 2, add)
				if variant == "lazy-zero" {
					helper = append(helper, 0x20, 3, add)
				}
			}
			label := byte(1)
			if variant == "wrong-backedge" {
				label = 2
			}
			helper = append(helper, 0x0c, label, 0x0b, 0x0b)
			if variant == "lazy-zero" {
				helper = append(helper, 0x21, 2, 0x21, 1, 0x20, 1, 0x20, 3, add, 0x20, 2)
			}
			if variant == "post-locals" {
				helper = append(helper, 0x1a, 0x1a, 0x20, 1, 0x20, 2)
			}
			helper = append(helper, 0x0b)
			params, pair := []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ, typ}
			m := modFuncs(t, funcDef{params, []wasm.ValType{typ}, caller}, funcDef{params, pair, helper}, funcDef{nil, pair, []byte{0, k, 0, k, 0, 0x0b}})
			for _, enabled := range []bool{false, true} {
				for _, inline := range []bool{false, true} {
					for _, workers := range []int{1, 4} {
						for _, count := range []uint64{0, 1, 2, 3, 4, 7, 13} {
							a, b := mask, uint64(7)
							iterations := count
							if variant == "wrong-backedge" && iterations > 1 {
								iterations = 1
							}
							for range iterations {
								next := (a + b) & mask
								if variant == "local-write" {
									next = (next + 1) & mask
								}
								a, b = b, next
							}
							want := a
							if variant == "counter-result" {
								want = 0
							}
							if variant == "then-expression" {
								want = (want + 1) & mask
							}
							if variant == "prefix" {
								want = (want + 7) & mask
							}
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, CompactNative: variant == "compact", Interruptible: variant == "interruptible", Optimizations: map[string]bool{"cold-tuple-loop-exit": enabled, "inline": inline, "loop-local-params": variant != "no-direct-homes", "scalar-loop-unroll": variant != "unroll-off", "zero-branch": variant != "zero-branch-off"}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, count, mask, 7)
							if variant == "trapping-else" && count > 0 {
								var trap *rt.TrapError
								if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
									t.Fatalf("unselected/selected effect variant=%s wide=%t count=%d err=%v", variant, wide, count, err)
								}
							} else if err != nil || got&mask != want {
								t.Fatalf("variant=%s wide=%t enabled=%t inline=%t workers=%d count=%d got=%x err=%v want=%x", variant, wide, enabled, inline, workers, count, got, err, want)
							}
							eligible := variant == "trapping-else" || variant == "prefix" || variant == "lazy-zero" || variant == "unroll-off" || variant == "zero-branch-off" || variant == "counter-result" || variant == "post-locals"
							if diagnosticsEnabled {
								hit := false
								for _, s := range stats.Funcs {
									hit = hit || s.Peephole["cold-tuple-loop-exit"] != 0
								}
								if hit != (enabled && eligible && !profileEnabled) {
									t.Fatalf("fallback mismatch variant=%s enabled=%t stats=%v", variant, enabled, stats.Funcs)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestColdTupleLoopExitBooleanCondition(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, eqz, sub, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x45), byte(0x6b), byte(0x6a)
		mask := uint64(0xffffffff)
		if wide {
			typ, bt, k, eqz, sub, add = wasm.I64, 0x7e, 0x42, 0x50, 0x7d, 0x7c
			mask = ^uint64(0)
		}
		params, pair := []wasm.ValType{typ, typ, typ, wasm.I32}, []wasm.ValType{typ, typ}
		caller := []byte{1, 2, bt, 0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x10, 1, 0x21, 5, 0x21, 4, 0x20, 4, 0x0b}
		helper := []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x03, 1, 0x21, 3, 0x21, 2, 0x21, 1, 0x21, 0, 0x20, 0, eqz, 0x20, 3, 0x72, 0x04, 2, 0x20, 1, 0x20, 2, 0x05, 0x20, 0, k, 1, sub, 0x20, 2, 0x20, 1, 0x20, 2, add, 0x20, 3, 0x0c, 1, 0x0b, 0x0b, 0x0b}
		m := modFuncs(t, funcDef{params, []wasm.ValType{typ}, caller}, funcDef{params, pair, helper}, funcDef{nil, pair, []byte{0, k, 0, k, 0, 0x0b}})
		for _, enabled := range []bool{false, true} {
			for _, zeroBranch := range []bool{false, true} {
				for _, inline := range []bool{false, true} {
					for _, count := range []uint64{0, 1, 4, 13} {
						for _, stop := range []uint64{0, 1, 0xfeedface00000000, ^uint64(0)} {
							a, b := mask, uint64(7)
							iterations := count
							if uint32(stop) != 0 {
								iterations = 0
							}
							for range iterations {
								a, b = b, (a+b)&mask
							}
							var stats ModuleStats
							opts := CompileOptions{Optimizations: map[string]bool{"cold-tuple-loop-exit": enabled, "zero-branch": zeroBranch, "inline": inline}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, count, mask, 7, stop)
							if err != nil || got&mask != a {
								t.Fatalf("wide=%t enabled=%t zeroBranch=%t inline=%t count=%d stop=%x got=%x err=%v want=%x", wide, enabled, zeroBranch, inline, count, stop, got, err, a)
							}
							if diagnosticsEnabled && (stats.Funcs[1].Peephole["cold-tuple-loop-exit"] != 0) != (enabled && !profileEnabled) {
								t.Fatalf("boolean cold path missing enabled=%t stats=%v", enabled, stats.Funcs[1].Peephole)
							}
						}
					}
				}
			}
		}
	}
}
