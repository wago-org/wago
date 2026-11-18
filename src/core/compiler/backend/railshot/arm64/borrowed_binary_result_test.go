//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestBorrowedBinaryResultPreservesInputsArm64(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, store, load, add, xor := wasm.I32, byte(0x36), byte(0x28), byte(0x6a), byte(0x73)
		if wide {
			typ, store, load, add, xor = wasm.I64, 0x37, 0x29, 0x7c, 0x85
		}
		for _, op := range []byte{add, add + 1, add + 2, add + 7, add + 8, add + 9} { // add/sub/mul/and/or/xor
			for _, deferred := range []bool{false, true} {
				body := []byte{0, 0x41, 0, 0x20, 0, 0x20, 1}
				if deferred {
					body = append(body, 0x20, 1, add)
				}
				body = append(body, op, store, 0, 0, 0x41, 0, load, 0, 0, 0x20, 0, xor, 0x0b)
				m := modMem(t, 1, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
				for _, enabled := range []bool{false, true} {
					for _, pair := range [][2]uint64{{0, 0}, {7, 5}, {0xffffffff, 0x12345678}, {0xfedcba9876543210, 0x8877665544332211}} {
						a, b := pair[0], pair[1]
						if deferred {
							b += b
						}
						if !wide {
							a, b = uint64(uint32(a)), uint64(uint32(b))
						}
						want := uint64(0)
						switch op {
						case add:
							want = a + b
						case add + 1:
							want = a - b
						case add + 2:
							want = a * b
						case add + 7:
							want = a & b
						case add + 8:
							want = a | b
						case add + 9:
							want = a ^ b
						}
						want ^= a
						if !wide {
							want = uint64(uint32(want))
						}
						opts := CompileOptions{Optimizations: map[string]bool{"three-op-sink": enabled}}
						var stats ModuleStats
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, pair[:]...)
						if diagnosticsEnabled && op == xor && deferred {
							hits := stats.Funcs[0].Peephole["borrowed-binary-result"]
							if enabled && hits == 0 || !enabled && hits != 0 {
								t.Fatalf("enabled=%v: borrowed result selections=%d", enabled, hits)
							}
						}
						if err != nil || got != want {
							t.Fatalf("wide=%v op=%x deferred=%v enabled=%v args=%x: got=%x want=%x err=%v", wide, op, deferred, enabled, pair, got, want, err)
						}
					}
				}
			}
		}
	}
}

func TestBorrowedImmediateResultPreservesSourceArm64(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, c, add, store, load, xor := wasm.I32, byte(0x41), byte(0x6a), byte(0x36), byte(0x28), byte(0x73)
		constants := []int64{-4095, -1, 0, 1, 4095, 4096, 0x1234567}
		if wide {
			typ, c, add, store, load, xor = wasm.I64, 0x42, 0x7c, 0x37, 0x29, 0x85
			constants = append(constants, -1<<63, 1<<63-1)
		}
		for _, subtract := range []bool{false, true} {
			for _, immediate := range constants {
				op := add
				if subtract {
					op++
				}
				body := []byte{0, 0x41, 0, 0x20, 0, c}
				body = append(body, wasmtest.SLEB64(immediate)...)
				body = append(body, op, store, 0, 0, 0x41, 0, load, 0, 0, 0x20, 0, xor, 0x0b)
				m := modMem(t, 1, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
				for _, enabled := range []bool{false, true} {
					for _, source := range []uint64{0, 7, 0xffffffff, 0x8877665544332211} {
						a := source
						if !wide {
							a = uint64(uint32(a))
						}
						want := a + uint64(immediate)
						if subtract {
							want = a - uint64(immediate)
						}
						want ^= a
						if !wide {
							want = uint64(uint32(want))
						}
						var stats ModuleStats
						opts := CompileOptions{Optimizations: map[string]bool{"three-op-sink": enabled}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, source)
						if err != nil || got != want {
							t.Fatalf("wide=%v sub=%v immediate=%d enabled=%v source=%x: got=%x want=%x err=%v", wide, subtract, immediate, enabled, source, got, want, err)
						}
						if diagnosticsEnabled {
							hits := stats.Funcs[0].Peephole["borrowed-immediate-result"]
							if !enabled && hits != 0 || enabled && subtract && immediate == 1 && hits == 0 {
								t.Fatalf("enabled=%v sub=%v constant=%d: selections=%d", enabled, subtract, immediate, hits)
							}
						}
					}
				}
			}
		}
	}
}

func TestBorrowedConstantBinaryResultPreservesSourceArm64(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, c, add, store, load, xor := wasm.I32, byte(0x41), byte(0x6a), byte(0x36), byte(0x28), byte(0x73)
		constants := []int64{-4095, -1, 0, 1, 4095, 4096, 0x1234567}
		if wide {
			typ, c, add, store, load, xor = wasm.I64, 0x42, 0x7c, 0x37, 0x29, 0x85
			constants = append(constants, -1<<63, 1<<63-1)
		}
		for _, opcodeOffset := range []byte{2, 7, 8, 9} {
			for _, immediate := range constants {
				op := add + opcodeOffset
				body := []byte{0, 0x41, 0, 0x20, 0, c}
				body = append(body, wasmtest.SLEB64(immediate)...)
				body = append(body, op, store, 0, 0, 0x41, 0, load, 0, 0, 0x20, 0, xor, 0x0b)
				m := modMem(t, 1, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
				for _, enabled := range []bool{false, true} {
					for _, source := range []uint64{0, 7, 0xffffffff, 0x8877665544332211} {
						a := source
						if !wide {
							a = uint64(uint32(a))
						}
						var want uint64
						switch opcodeOffset {
						case 2:
							want = a * uint64(immediate)
						case 7:
							want = a & uint64(immediate)
						case 8:
							want = a | uint64(immediate)
						case 9:
							want = a ^ uint64(immediate)
						}
						want ^= a
						if !wide {
							want = uint64(uint32(want))
						}
						var stats ModuleStats
						opts := CompileOptions{Optimizations: map[string]bool{"three-op-sink": enabled}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, source)
						if err != nil || got != want {
							t.Fatalf("wide=%v op=%x immediate=%d enabled=%v source=%x: got=%x want=%x err=%v", wide, op, immediate, enabled, source, got, want, err)
						}
						if diagnosticsEnabled {
							hits := stats.Funcs[0].Peephole["borrowed-immediate-result"]
							if !enabled && hits != 0 || enabled && opcodeOffset == 2 && immediate == 0x1234567 && hits == 0 {
								t.Fatalf("enabled=%v op=%x constant=%d: selections=%d", enabled, op, immediate, hits)
							}
						}
					}
				}
			}
		}
	}
}
