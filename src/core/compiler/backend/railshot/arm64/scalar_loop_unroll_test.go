//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestScalarLoopUnrollChecksEveryIteration(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, add, k, lt := wasm.I32, byte(0x7f), byte(0x6a), byte(0x41), byte(0x49)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, add, k, lt = wasm.I64, 0x7e, 0x7c, 0x42, 0x54
			fold = func(v uint64) uint64 { return v }
		}
		for _, variant := range []string{"ordinary", "signed", "index-read", "post-index-read", "multiply-index", "select-index", "counter-reset", "double-counter", "long-body", "memory", "interruptible", "compact"} {
			branchLT := lt
			if variant == "signed" {
				branchLT--
			}
			repeats := uint64(1)
			if variant == "long-body" {
				repeats = 9
			}
			body := []byte{1, 1, bt, 0x03, 0x40}
			if variant == "post-index-read" {
				body = append(body, 0x20, 1, k, 1, add, 0x21, 1)
			}
			for range repeats {
				body = append(body, 0x20, 3, 0x20, 2)
				if variant == "index-read" || variant == "post-index-read" {
					xor := byte(0x73)
					if wide {
						xor = 0x85
					}
					body = append(body, 0x20, 1, xor)
				} else if variant == "select-index" {
					body = append(body, 0x20, 1, 0x20, 1)
					if wide {
						body = append(body, 0xa7)
					}
					body = append(body, 0x41, 1, 0x71, 0x1b)
				} else if variant == "multiply-index" {
					mul := byte(0x6c)
					if wide {
						mul = 0x7e
					}
					body = append(body, 0x20, 1, mul)
				}
				body = append(body, add, 0x21, 3)
			}
			if variant == "memory" {
				body = append(body, 0x41, 0, 0x20, 1)
				if wide {
					body = append(body, 0xa7)
				}
				body = append(body, 0x36, 2, 0)
			}
			if variant == "counter-reset" {
				body = append(body, 0x20, 0, 0x21, 1)
			}
			if variant == "double-counter" {
				body = append(body, 0x20, 3, k, 1, add, 0x21, 3)
			}
			if variant != "post-index-read" {
				body = append(body, 0x20, 1, k, 1, add, 0x21, 1)
			}
			body = append(body, 0x20, 1, 0x20, 0, branchLT, 0x0d, 0, 0x0b, 0x20, 3, 0x20, 1, add)
			if variant == "memory" {
				body = append(body, 0x41, 0, 0x28, 2, 0)
				if wide {
					body = append(body, 0xad)
				}
				body = append(body, add)
			}
			body = append(body, 0x0b)
			params, results := []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}
			m := modFuncs(t, funcDef{params, results, body}, funcDef{params, results, []byte{0, 0x20, 2, 0x0b}})
			if variant == "memory" {
				m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			}
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for count := uint32(0); count < 14; count++ {
						bound := uint64(count)
						initials := []uint64{0, ^uint64(0), ^uint64(0) - 1}
						if variant == "signed" {
							sign := uint64(1) << 31
							if wide {
								sign = uint64(1) << 63
							}
							bound = sign + uint64(count)
							initials = []uint64{sign - 1, sign, sign + 1}
						}
						for _, initial := range initials {
							iterations, last, i := uint64(0), fold(initial), fold(initial)
							for {
								iterations++
								last = i
								if variant == "counter-reset" {
									i = bound
								}
								i = fold(i + 1)
								keep := i < bound
								if variant == "signed" {
									if wide {
										keep = int64(i) < int64(bound)
									} else {
										keep = int32(i) < int32(bound)
									}
								}
								if !keep {
									break
								}
							}
							for _, value := range []uint64{3, 0x8000000000000001, ^uint64(0)} {
								var stats ModuleStats
								opts := CompileOptions{Workers: workers, Interruptible: variant == "interruptible", CompactNative: variant == "compact", Optimizations: map[string]bool{"scalar-loop-unroll": enabled}}
								if diagnosticsEnabled {
									opts.Stats = &stats
								}
								nArg, iArg := bound, initial
								if !wide {
									nArg |= 0xdeadbeef00000000
									iArg |= 0xcafebabe00000000
								}
								got, err := runArm64WrapperWithOptions(t, m, opts, nArg, iArg, value)
								sum, index := uint64(0), fold(initial)
								for range iterations {
									term := fold(value)
									if variant == "index-read" {
										term = fold(value ^ index)
									}
									if variant == "select-index" && index&1 == 0 {
										term = index
									}
									if variant == "multiply-index" {
										term = fold(fold(value) * index)
									}
									if variant == "counter-reset" {
										index = bound
									}
									index = fold(index + 1)
									if variant == "post-index-read" {
										term = fold(value ^ index)
									}
									sum = fold(sum + repeats*term)
									if variant == "double-counter" {
										sum = fold(sum + 1)
									}
								}
								want := fold(sum + i)
								if variant == "memory" {
									want = fold(want + uint64(uint32(last)))
								}
								if err != nil || fold(got) != want {
									t.Fatalf("wide=%t variant=%s enabled=%t workers=%d count=%d initial=%x value=%x got=%x err=%v want=%x", wide, variant, enabled, workers, count, initial, value, got, err, want)
								}
								positive := variant == "ordinary" || variant == "signed" || variant == "index-read" || variant == "post-index-read" || variant == "multiply-index" || variant == "select-index"
								if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-loop-unroll"] != 0) != (enabled && positive && !profileEnabled) {
									t.Fatalf("unroll path mismatch: wide=%t variant=%s enabled=%t", wide, variant, enabled)
								}
							}
						}
					}
				}
			}
		}
	}
}
