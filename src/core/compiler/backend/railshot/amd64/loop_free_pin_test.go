// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestLoopFreePinsPreserveLocalsAcrossCalls(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	body := []byte{1, 10, 0x7f, 0x41, 0, 0x28, 2, 0, 0x1a}
	for local := byte(1); local <= 8; local++ {
		body = append(body, 0x41, local, 0x21, local)
		for j := 0; j < 80; j++ {
			body = append(body, 0x20, local, 0x1a)
		}
	}
	body = append(body,
		0x20, 0, 0x21, 9, 0x20, 0, 0x21, 10,
		0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0,
		0x03, 0x40,
		0x20, 9, 0x20, 9, 0x6a, 0x20, 10, 0x6a, 0x21, 9,
		0x20, 10, 0x41, 1, 0x6b, 0x22, 10, 0x0d, 0,
		0x0b, 0x0b,
		0x20, 9, 0x20, 1, 0x6a, 0x20, 2, 0x6a,
		0x20, 3, 0x6a, 0x20, 4, 0x6a, 0x20, 5, 0x6a,
		0x20, 6, 0x6a, 0x20, 7, 0x6a, 0x20, 8, 0x6a, 0x10, 1, 0x0b)
	m := modFuncs(t, funcDef{i32, i32, body}, funcDef{i32, i32, []byte{0, 0x20, 0, 0x0b}})
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	old := loopFreePinEnabled
	defer func() { loopFreePinEnabled = old }()
	for _, enabled := range []bool{false, true} {
		loopFreePinEnabled = enabled
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"inline": false}})
		if err != nil {
			t.Fatal(err)
		}
		if diagnosticsEnabled && (stats.Funcs[0].Peephole["loop-free-pin"] > 0) != enabled {
			t.Fatalf("enabled=%v: loop free pin count = %d; counters=%v", enabled, stats.Funcs[0].Peephole["loop-free-pin"], stats.Funcs[0].Peephole)
		}
		for _, n := range []uint32{0, 1, 2, 3, 17, 31} {
			acc := n
			for count := n; count != 0; count-- {
				acc = acc*2 + count
			}
			want := acc + 36
			if got := runCompiledAmd64u(t, cm, uint64(n)); got != uint64(want) {
				t.Fatalf("enabled=%v n=%d: got %d, want %d", enabled, n, got, want)
			}
		}
		cm.CodeImage.Close()
	}
}

func TestLoopFreePinsPreserveStoresBeforeTrap(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	body := []byte{1, 10, 0x7f}
	for local := byte(2); local <= 9; local++ {
		body = append(body, 0x41, local, 0x21, local)
		for j := 0; j < 80; j++ {
			body = append(body, 0x20, local, 0x1a)
		}
	}
	body = append(body,
		0x41, 5, 0x41, 11, 0x3a, 0, 0, // store that must survive a later trap
		0x20, 1, 0x21, 10, 0x20, 0, 0x21, 11,
		0x02, 0x40, 0x20, 0, 0x45, 0x0d, 0,
		0x03, 0x40,
		0x20, 10, 0x20, 10, 0x2d, 0, 0, 0x41, 1, 0x6a, 0x3a, 0, 0,
		0x20, 11, 0x41, 1, 0x6b, 0x22, 11, 0x0d, 0,
		0x0b, 0x0b,
		0x20, 10, 0x10, 1, 0x0b)
	m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, i32, body}, funcDef{i32, i32, []byte{0, 0x20, 0, 0x0b}})
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	old := loopFreePinEnabled
	defer func() { loopFreePinEnabled = old }()
	for _, enabled := range []bool{false, true} {
		loopFreePinEnabled = enabled
		for _, tc := range []struct{ count, base uint32 }{
			{0, 65536}, {3, 10}, {1, 65535}, {2, 65535}, {1, 65536}, {2, ^uint32(0)},
		} {
			var stats ModuleStats
			got, memory, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"inline": false}}, func(mem []byte) {
				for i := range mem {
					mem[i] = 7
				}
			}, uint64(tc.count), uint64(tc.base))
			want := make([]byte, 65536)
			for i := range want {
				want[i] = 7
			}
			want[5] = 11
			ptr, trapped := tc.base, false
			for i := uint32(0); i < tc.count; i++ {
				if ptr >= uint32(len(want)) {
					trapped = true
					break
				}
				want[ptr]++
			}
			if (err != nil) != trapped || !trapped && got != uint64(ptr) || !bytes.Equal(memory, want) {
				t.Fatalf("enabled=%v count=%d base=%d: got=%d err=%v, want=%d trap=%v, memory matches=%v", enabled, tc.count, tc.base, got, err, ptr, trapped, bytes.Equal(memory, want))
			}
			if diagnosticsEnabled && (stats.Funcs[0].Peephole["loop-free-pin"] > 0) != enabled {
				t.Fatalf("enabled=%v: loop free pin count = %d; counters=%v", enabled, stats.Funcs[0].Peephole["loop-free-pin"], stats.Funcs[0].Peephole)
			}
		}
	}
}
