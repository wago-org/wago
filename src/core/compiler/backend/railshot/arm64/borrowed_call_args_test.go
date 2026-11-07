//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestBorrowedCallArgumentsPreserveLocalsAndLowerStackArm64(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, c, add, mul, xor := wasm.I32, byte(0x41), byte(0x6a), byte(0x6c), byte(0x73)
		if wide {
			typ, c, add, mul, xor = wasm.I64, 0x42, 0x7c, 0x7e, 0x85
		}
		for _, n := range []int{2, 4, 6, 8} {
			types := make([]wasm.ValType, n)
			for i := range types {
				types[i] = typ
			}
			for mode := 0; mode < 4; mode++ {
				// A deferred value stays below the arguments until call staging. Every
				// original local is also read after the call, including high argument pins.
				body := []byte{0, 0x20, 0, 0x20, 1, xor}
				indices := make([]int, n)
				factors := make([]uint64, n)
				offsets := make([]int64, n)
				for i := 0; i < n; i++ {
					k := n - 1 - i
					if mode == 1 {
						k = i % 2
					}
					indices[i], factors[i] = k, 1
					body = append(body, 0x20, byte(k))
					if mode == 2 && i%2 != 0 {
						factors[i] = uint64(i + 1)
						body = append(body, c, byte(i+1), mul)
					}
					if mode == 3 && i%2 != 0 {
						offsets[i] = int64(i + 1)
						body = append(body, c, byte(i+1))
						if i%4 == 1 {
							body = append(body, add)
						} else {
							offsets[i] = -offsets[i]
							if wide {
								body = append(body, 0x7d)
							} else {
								body = append(body, 0x6b)
							}
						}
					}
				}
				body = append(body, 0x10, 1, add)
				for i := 0; i < n; i++ {
					body = append(body, 0x20, byte(i), c, byte(i+3), mul, add)
				}
				body = append(body, 0x0b)
				callee := []byte{0}
				for i := 0; i < n; i++ {
					callee = append(callee, 0x20, byte(i), c, byte(i+1), mul)
					if i > 0 {
						callee = append(callee, add)
					}
				}
				callee = append(callee, 0x0b)
				m := modFuncs(t, funcDef{params: types, results: []wasm.ValType{typ}, body: body}, funcDef{params: types, results: []wasm.ValType{typ}, body: callee})
				for _, enabled := range []bool{false, true} {
					for _, seed := range []uint64{7, 0x8877665544332211} {
						args := make([]uint64, n)
						for i := range args {
							args[i] = seed + uint64(i)*0x102030405060708
							if !wide {
								args[i] = uint64(uint32(args[i]))
							}
						}
						want := args[0] ^ args[1]
						for i := 0; i < n; i++ {
							want += (args[indices[i]]*factors[i]+uint64(offsets[i]))*uint64(i+1) + args[i]*uint64(i+3)
						}
						if !wide {
							want = uint64(uint32(want))
						}
						var stats ModuleStats
						opts := CompileOptions{Optimizations: map[string]bool{"inline": false, "register-call-borrowed-args": enabled}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, args...)
						if err != nil || got != want {
							t.Fatalf("wide=%v args=%d mode=%d enabled=%v: got=%x want=%x err=%v", wide, n, mode, enabled, got, want, err)
						}
						if diagnosticsEnabled {
							hits := stats.Funcs[0].Peephole["register-call-borrowed-arg"]
							if mode == 3 {
								immediateHits := stats.Funcs[0].Peephole["register-call-immediate-arg"]
								if enabled && immediateHits == 0 || !enabled && immediateHits != 0 {
									t.Fatalf("wide=%v args=%d enabled=%v: immediate selections=%d", wide, n, enabled, immediateHits)
								}
							}
							if enabled && hits == 0 || !enabled && hits != 0 {
								t.Fatalf("wide=%v args=%d mode=%d enabled=%v: selections=%d", wide, n, mode, enabled, hits)
							}
						}
					}
				}
			}
		}
	}
}

func TestImmediateCallArgumentsRetirePendingNodesArm64(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, constant, add := wasm.I32, byte(0x41), byte(0x6a)
		if wide {
			typ, constant, add = wasm.I64, 0x42, 0x7c
		}
		// Each call consumes one deferred immediate argument. Cross the packet
		// limit repeatedly so an abandoned deferred-node count cannot hide in a
		// small function that ends before the next packet-cap check.
		body := []byte{0}
		for range 96 {
			body = append(body, 0x20, 0, constant, 1, add, 0x10, 1, 0x1a)
		}
		body = append(body, 0x20, 0, constant, 5, add, 0x0b)
		m := modFuncs(t,
			funcDef{params: []wasm.ValType{typ}, results: []wasm.ValType{typ}, body: body},
			funcDef{params: []wasm.ValType{typ}, results: []wasm.ValType{typ}, body: []byte{0, 0x20, 0, constant, 3, add, 0x0b}})
		for _, enabled := range []bool{false, true} {
			for _, arg := range []uint64{7, 0x88776655fffffffe} {
				want := arg + 5
				if !wide {
					want = uint64(uint32(want))
				}
				opts := CompileOptions{Optimizations: map[string]bool{"inline": false, "register-call-borrowed-args": enabled}}
				var stats ModuleStats
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, arg)
				if err != nil || got != want {
					t.Fatalf("wide=%v enabled=%v: got=%x want=%x err=%v", wide, enabled, got, want, err)
				}
				if diagnosticsEnabled {
					wantHits := uint64(0)
					if enabled {
						wantHits = 96
					}
					if got := stats.Funcs[0].Peephole["register-call-immediate-arg"]; uint64(got) != wantHits {
						t.Fatalf("immediate selections=%d, want %d", got, wantHits)
					}
				}
			}
		}
	}
}
