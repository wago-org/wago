// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestInlineTupleLoopDroppedResultTrapOrigin(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, div, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x6e), byte(0x6a)
		if wide {
			typ, bt, k, div, add = wasm.I64, 0x7e, 0x42, 0x80, 0x7c
		}
		for _, prefixTraps := range []bool{false, true} {
			prefix := []byte{k, 7}
			if prefixTraps {
				prefix = []byte{k, 1, 0x20, 1, div}
			}
			caller := append(append([]byte(nil), prefix...), 2, bt, 0x20, 0, 0x20, 1, 0x10, 0, 0x1a, 0x0b, add, 0x0b)
			helper := []byte{3, 0x40, 1, 0x0b, 0x20, 0, 0x20, 0, 0x20, 1, div, 0x0b}
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}), wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ}))),
				wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(helper), wasmtest.Code(caller))))
			fallbackPC := make(map[int]uint32)
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					config := NewRuntimeConfig().WithFunctionWorkers(workers).WithOptimization("straight-line-block", enabled).WithOptimization("inline", true)
					compiled, err := Compile(config, module)
					if err != nil {
						t.Fatal(err)
					}
					defer compiled.Close()
					instance, err := Instantiate(compiled)
					if err != nil {
						t.Fatal(err)
					}
					defer instance.Close()
					_, err = instance.Invoke("call", 19, 0)
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapDivZero || len(trap.Frames) != 1 {
						t.Fatalf("wide=%t prefix=%t enabled=%t workers=%d: %v", wide, prefixTraps, enabled, workers, err)
					}
					wantFunc := uint32(0)
					if prefixTraps {
						wantFunc = 1
					}
					frame := trap.Frames[0]
					if frame.FunctionIndex != wantFunc || !frame.HasProgramCounter {
						t.Fatalf("wide=%t prefix=%t enabled=%t workers=%d: frame=%+v want func%d with source PC", wide, prefixTraps, enabled, workers, frame, wantFunc)
					}
					// Deferred traps currently carry their materialization boundary's
					// PC. Retaining registers must preserve that fallback attribution.
					if !enabled {
						fallbackPC[workers] = frame.ProgramCounter
					} else if frame.ProgramCounter != fallbackPC[workers] {
						t.Fatalf("optimized source PC=%d, fallback PC=%d", frame.ProgramCounter, fallbackPC[workers])
					}
					result, err := instance.Invoke("call", 19, 3)
					want := uint64(26)
					if prefixTraps {
						want = 19
					}
					if err != nil || len(result) != 1 || result[0] != want {
						t.Fatalf("nontrapping control=%v err=%v want=%d", result, err, want)
					}
				}
			}
		}
	}
}
