// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build ((linux && amd64) || ((linux || darwin || windows) && arm64)) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func byteReduceModule(offset uint32, weighted bool, bias int32, afterLoop bool) []byte {
	// Parameters are limit, index, accumulator. Both modified locals escape in
	// the result, so an optimized loop must publish the exact final state.
	body := []byte{0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x20, 0, 0x4f, 0x0d, 1,
		0x20, 2, 0x20, 1, 0x2d, 0}
	body = append(body, wasmtest.ULEB(offset)...)
	if weighted {
		body = append(body, 0x20, 1, 0x41)
		body = append(body, wasmtest.SLEB32(bias)...)
		body = append(body, 0x6a, 0x6c)
	}
	body = append(body, 0x6a, 0x21, 2, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x0c, 0, 0x0b)
	if afterLoop {
		// The top-tested exit skips this write. A fast path must not land here.
		body = append(body, 0x41, 0x7f, 0x21, 2)
	}
	body = append(body, 0x0b, 0x20, 2, 0xad, 0x20, 1, 0xad, 0x42, 32, 0x86, 0x84, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestByteReduceLoopExactResultsAndFallback(t *testing.T) {
	for _, weighted := range []bool{false, true} {
		for _, bias := range []int32{0, 1, -1, 0x7fffffff, -0x80000000} {
			for _, offset := range []uint32{0, 1, 7, 65536, 0xffffffff} {
				for _, enabled := range []bool{false, true} {
					t.Run(fmt.Sprintf("weighted=%t/bias=%d/offset=%d/on=%t", weighted, bias, offset, enabled), func(t *testing.T) {
						c, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("byte-reduce-loop", enabled).Compile(byteReduceModule(offset, weighted, bias, false))
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
						for i := range mem {
							mem[i] = byte(i*37 + 19)
						}
						for _, n := range []uint32{0, 1, 3, 4, 7, 8, 15, 16, 17, 31, 32, 63, 64, 65, 256, 65536, 65537, 0xffffffff} {
							for _, start := range []uint32{0, 1, n, 65535, 0xfffffffe} {
								const initial = uint32(0xfffffff1)
								sum, index := initial, start
								traps := false
								for index < n {
									address := uint64(index) + uint64(offset)
									if address >= uint64(len(mem)) {
										traps = true
										break
									}
									value := uint32(mem[address])
									if weighted {
										value *= index + uint32(bias)
									}
									sum += value
									index++
								}
								got, err := in.Invoke("run", uint64(n), uint64(start), uint64(initial))
								if traps {
									var trap *TrapError
									if !errors.As(err, &trap) {
										t.Fatalf("n=%d start=%d: expected bounds trap, got %v", n, start, err)
									}
									continue
								}
								want := uint64(index)<<32 | uint64(sum)
								if err != nil || len(got) != 1 || got[0] != want {
									t.Fatalf("n=%d start=%d: %v/%v, want %#x", n, start, got, err, want)
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestByteReduceLoopRejectsSkippedOuterEffects(t *testing.T) {
	c, err := NewRuntimeConfig().WithOptimization("byte-reduce-loop", true).Compile(byteReduceModule(0, true, 1, true))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("run", 64, 0, 7)
	if err != nil || len(got) != 1 || got[0] != uint64(64)<<32|7 {
		t.Fatalf("outer effect executed: %v/%v", got, err)
	}
}
