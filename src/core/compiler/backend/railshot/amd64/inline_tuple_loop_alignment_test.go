// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestInlineTupleLoopRetainsSmallLoopAlignment(t *testing.T) {
	// A long caller contains one small private loop. Inlining must retain the
	// callee's loop alignment even though the caller exceeds the small-body cap.
	helper := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1,
		0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0,
		0x0c, 0, 0x0b, 0x0b, 0x20, 0, 0x20, 1, 0x0b}
	caller := []byte{1, 2, 0x7f, 0x20, 0, 0x10, 1, 0x21, 2, 0x21, 1}
	for range 80 {
		caller = append(caller, 0x01)
	}
	caller = append(caller, 0x20, 2, 0x0b)
	m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, caller}, funcDef{[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}, helper})
	for _, align := range []bool{false, true} {
		for _, workers := range []int{1, 4} {
			for _, n := range []uint64{0, 1, 3, 9} {
				var stats ModuleStats
				opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"compact-loop-align32": align}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, _, err := runMemAmd64WithOptions(t, m, opts, nil, n)
				if err != nil || got != n {
					t.Fatalf("align=%t workers=%d n=%d got=%d err=%v", align, workers, n, got, err)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["compact-loop-align32"] != 0) != (align && !profileEnabled) {
					t.Fatalf("align=%t profile=%t stats=%v", align, profileEnabled, stats.Funcs[0].Peephole)
				}
			}
		}
	}
}
