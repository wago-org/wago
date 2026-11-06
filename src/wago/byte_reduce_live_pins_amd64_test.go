//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func byteReduceLivePinsModule(weighted bool, extra int) []byte {
	code := []byte{0}
	if extra != 0 {
		code = []byte{1, byte(extra), 0x7f}
	}
	for i := 0; i < extra; i++ {
		code = append(code, 0x41)
		code = append(code, wasmtest.SLEB32(int32(101+i))...)
		code = append(code, 0x21, byte(3+i))
	}
	code = append(code, 0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x20, 0, 0x4f, 0x0d, 1,
		0x20, 2, 0x20, 1, 0x2d, 0, 0)
	if weighted {
		code = append(code, 0x20, 1, 0x41, 1, 0x6a, 0x6c)
	}
	code = append(code, 0x6a, 0x21, 2, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x0c, 0, 0x0b, 0x0b, 0x20, 2)
	for i := 0; i < extra; i++ {
		code = append(code, 0x20, byte(3+i), 0x6a)
	}
	code = append(code, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))),
	)
}

func TestByteReduceLoopLivePins(t *testing.T) {
	for _, weighted := range []bool{false, true} {
		for _, extra := range []int{0, 2, 4, 6} {
			for _, enabled := range []bool{false, true} {
				t.Run(fmt.Sprintf("weighted=%t/locals=%d/on=%t", weighted, extra, enabled), func(t *testing.T) {
					c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("byte-reduce-loop", enabled).Compile(byteReduceLivePinsModule(weighted, extra))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					mem := in.Memory().UnsafeBytes()
					for _, filled := range []bool{false, true} {
						if filled {
							for i := range mem {
								mem[i] = byte(i*37 + 19)
							}
						}
						for _, limit := range []uint32{64, 0, 1, 3, 15, 16, 17, 63, 65, 65536, 65537} {
							for _, start := range []uint32{0, 1, limit} {
								for _, initial := range []uint32{7, 0xfffffff1} {
									want, traps := initial, false
									for i := start; i < limit; i++ {
										if i >= uint32(len(mem)) {
											traps = true
											break
										}
										v := uint32(mem[i])
										if weighted {
											v *= i + 1
										}
										want += v
									}
									for i := 0; i < extra; i++ {
										want += uint32(101 + i)
									}
									high := uint64(0)
									if filled {
										high = 0xfedcba9800000000
									}
									got, err := in.Invoke("run", high|uint64(limit), high|uint64(start), high|uint64(initial))
									if traps {
										var trap *TrapError
										if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
											t.Fatalf("limit=%d start=%d: expected bounds trap, got %v", limit, start, err)
										}
									} else if err != nil || len(got) != 1 || uint32(got[0]) != want {
										t.Fatalf("filled=%t limit=%d start=%d initial=%d: got %v/%v, want %d", filled, limit, start, initial, got, err, want)
									}
								}
							}
						}
					}
				})
			}
		}
	}
}
