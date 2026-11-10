// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func callFreeExitRegisterModule(t testing.TB, changeLimit, callInLoop bool) *wasm.Module {
	body := []byte{0, 0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x20, 0, 0x4f, 0x0d, 1,
		0x20, 2, 0x20, 1, 0x6a, 0x21, 2}
	if callInLoop {
		body = append(body, 0x20, 2, 0x10, 1, 0x1a)
	}
	body = append(body, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1)
	if changeLimit {
		body = append(body, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0)
	}
	body = append(body, 0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x10, 1, 0x0b)
	return modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body},
		funcDef{[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x0b}})
}

func TestCallFreeExitRegisters(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, change := range []bool{false, true} {
		for _, call := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				var stats ModuleStats
				cm, err := CompileModuleWith(callFreeExitRegisterModule(t, change, call), CompileOptions{Stats: &stats, Optimizations: map[string]bool{"inline": false, "callfree-loop-reg-state": enabled}})
				if err != nil {
					t.Fatal(err)
				}
				wantSelected := 0
				if enabled && !call {
					wantSelected = 1
				}
				if got := stats.Funcs[0].Peephole["callfree-loop-exit-register-state"]; got != wantSelected {
					cm.CodeImage.Close()
					t.Fatalf("change=%t call=%t on=%t: selected=%d want=%d", change, call, enabled, got, wantSelected)
				}
				for _, args := range [][3]uint32{{0, 0, 7}, {1, 0, 0xffffffff}, {2, 2, 5}, {2, 0xfffffffe, 3}, {17, 0, 0xfffffff0}, {255, 1, 0}, {0xffffffff, 0xfffffff0, 7}} {
					limit, index, sum := args[0], args[1], args[2]
					for index < limit {
						sum += index
						index++
						if change {
							limit--
						}
					}
					got := runCompiledAmd64u(t, cm, uint64(args[0])|0xaabbccdd00000000, uint64(args[1])|0xaabbccdd00000000, uint64(args[2])|0xaabbccdd00000000)
					if uint32(got) != sum {
						cm.CodeImage.Close()
						t.Fatalf("change=%t call=%t on=%t args=%v: got=%d want=%d", change, call, enabled, args, got, sum)
					}
				}
				cm.CodeImage.Close()
			}
		}
	}
}
