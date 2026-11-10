// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestLinearIntegerPairReducesRepeatedCoefficient(t *testing.T) {
	for _, coefficient := range []int32{2, 3, 5, 9, 17, -2147483647} {
		// k*x + (y-x), with a live prefix and dirty upper halves on I32 inputs.
		body := []byte{0, 0x03, 0x7f, 0x20, 2, 0x20, 0, 0x41}
		body = append(body, wasmtest.SLEB32(coefficient)...)
		body = append(body, 0x6c, 0x20, 1, 0x20, 0, 0x6b, 0x6a, 0x6a, 0x0b, 0x0b)
		params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}
		m := modFuncs(t, funcDef{params, []wasm.ValType{wasm.I32}, body}, funcDef{params, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x0b}})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for _, x := range []uint64{0, 1, 0x80000000, 0xdeadbeefffffffff} {
					for _, y := range []uint64{0, 7, 0xcafebabe80000001} {
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"linear-integer-tree": enabled}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, x, y, 0x12345678)
						want := uint64(uint32((uint32(coefficient)-1)*uint32(x) + uint32(y) + 0x12345678))
						if err != nil || got != want {
							t.Fatalf("k=%d enabled=%t workers=%d x=%x y=%x got=%x err=%v want=%x", coefficient, enabled, workers, x, y, got, err, want)
						}
						if diagnosticsEnabled && (stats.Funcs[0].Peephole["linear-integer-pair"] != 0) != enabled {
							t.Fatalf("path mismatch k=%d enabled=%t stats=%v", coefficient, enabled, stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}

func TestLinearIntegerPairPreservesCapturedValuesAndTraps(t *testing.T) {
	for _, variant := range []string{"old-product", "old-difference", "division"} {
		prefix := []byte{0, 0x03, 0x7f}
		product := []byte{0x20, 0, 0x41, 3, 0x6c}
		difference := []byte{0x20, 1, 0x20, 0, 0x6b}
		mutation := []byte{0x20, 0, 0x41, 1, 0x6a, 0x21, 0}
		body := append([]byte{}, prefix...)
		switch variant {
		case "old-product":
			body = append(body, product...)
			body = append(body, mutation...)
			body = append(body, difference...)
		case "old-difference":
			body = append(body, difference...)
			body = append(body, mutation...)
			body = append(body, product...)
		case "division":
			body = append(body, product...)
			body = append(body, 0x20, 1, 0x20, 0, 0x6e, 0x20, 0, 0x6b)
		}
		body = append(body, 0x6a, 0x0b, 0x0b)
		params := []wasm.ValType{wasm.I32, wasm.I32}
		m := modFuncs(t, funcDef{params, []wasm.ValType{wasm.I32}, body}, funcDef{params, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x0b}})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for _, x := range []uint32{0, 1, 0x80000000, 0xffffffff} {
					y := uint32(0x87654321)
					var stats ModuleStats
					opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"linear-integer-tree": enabled}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, uint64(x)|0xdeadbeef00000000, uint64(y))
					if variant == "division" && x == 0 {
						var trap *rt.TrapError
						if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
							t.Fatalf("missing zero trap %v", err)
						}
						continue
					}
					want := 2*x + y - 1
					if variant == "old-difference" {
						want = 2*x + y + 3
					}
					if variant == "division" {
						want = 2*x + y/x
					}
					if err != nil || got != uint64(want) {
						t.Fatalf("variant=%s enabled=%t workers=%d x=%x got=%x err=%v want=%x", variant, enabled, workers, x, got, err, want)
					}
					if diagnosticsEnabled && stats.Funcs[0].Peephole["linear-integer-pair"] != 0 {
						t.Fatalf("captured/trapping source admitted: %s", variant)
					}
				}
			}
		}
	}
}

func TestLinearIntegerPairPressureRejectsBeforeEmission(t *testing.T) {
	for _, kind := range []storageKind{stLocalReg, stLocalRef} {
		for _, blocker := range []string{"pinned", "local", "reserved"} {
			f := gpClassEmitter()
			f.policy = currentCodegenPolicy()
			f.hasLoop = true
			blocked := allAllocatableGP().remove(X0).remove(X1)
			switch blocker {
			case "pinned":
				f.pinned = blocked
			case "local":
				f.pinnedLocalMask = blocked
			case "reserved":
				f.reserved = blocked
			}
			f.pushValue(storage{kind: kind, typ: mtI32, reg: X0, idx: 0})
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: 3})
			f.pushBinOp(opMul, mtI32)
			f.pushValue(storage{kind: kind, typ: mtI32, reg: X1, idx: 1})
			f.pushValue(storage{kind: kind, typ: mtI32, reg: X0, idx: 0})
			f.pushBinOp(opSub, mtI32)
			f.pushBinOp(opAdd, mtI32)
			root, pins := f.s.back(), f.pinned
			if got := f.tryLinearIntegerTree(root, regNone); got != regNone {
				t.Fatalf("pressure admitted kind=%v blocker=%s", kind, blocker)
			}
			if len(f.a.B) != 0 || f.pinned != pins || f.s.back() != root || !root.isDeferred() {
				t.Fatal("fallback mutated state")
			}
		}
	}
}
