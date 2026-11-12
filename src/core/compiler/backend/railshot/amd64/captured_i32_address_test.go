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

func TestCapturedI32AddressSurvivesLocalOverwrite(t *testing.T) {
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0,
		0x20, 0, // capture old address
		0x20, 1, 0x21, 0, // overwrite the local
		0x2d, 0, 0, // load through the captured address
		0x0b,
	})
	for _, facts := range []bool{false, true} {
		for _, tc := range []struct {
			address uint64
			trap    bool
		}{
			{0xdeadbeef00000007, false},
			{0xfeedface0000ffff, false},
			{0x0000000000010000, true},
			{0xffffffffffffffff, true},
		} {
			var stats ModuleStats
			got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"value-facts": facts}}, func(mem []byte) {
				mem[7], mem[65535] = 0x51, 0xa7
			}, tc.address, 123)
			if diagnosticsEnabled && (stats.Funcs[0].Peephole["addr-zext-elim"] != 0) != facts {
				t.Fatalf("facts=%v: address zero-extension selection = %d", facts, stats.Funcs[0].Peephole["addr-zext-elim"])
			}
			if (err != nil) != tc.trap {
				t.Fatalf("facts=%v address=%#x: trap=%v, want %v", facts, tc.address, err, tc.trap)
			}
			if !tc.trap {
				want := uint64(0x51)
				if uint32(tc.address) == 65535 {
					want = 0xa7
				}
				if got != want {
					t.Fatalf("facts=%v address=%#x: got %#x, want %#x", facts, tc.address, got, want)
				}
			}
		}
	}
}
