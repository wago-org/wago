// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestBorrowedLonePredicateLoop(t *testing.T) {
	// The loop's counter is a pinned local; both taken and untaken backedges
	// must observe the updated value without disturbing the accumulator.
	body := []byte{1, 1, 0x7f, 0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0,
		0x03, 0x40, 0x20, 1, 0x20, 0, 0x6a, 0x21, 1,
		0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0,
		0x0b, 0x0b, 0x20, 1, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	old := lonePredicateEnabled
	defer func() { lonePredicateEnabled = old }()
	for _, enabled := range []bool{false, true} {
		lonePredicateEnabled = enabled
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
		if err != nil {
			t.Fatal(err)
		}
		if enabled && diagnosticsEnabled && stats.Funcs[0].Peephole["lone-predicate-borrowed"] == 0 {
			t.Fatalf("borrowed predicate not selected: %v", stats.Funcs[0].Peephole)
		}
		for _, n := range []uint32{0, 1, 2, 17, 255} {
			want := uint32(19 + n*(n+1)/2)
			if got := runCompiledAmd64u(t, cm, uint64(n), 19); got != uint64(want) {
				t.Fatalf("enabled=%v n=%d got=%d want=%d", enabled, n, got, want)
			}
		}
		cm.CodeImage.Close()
	}
}
