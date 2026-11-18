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
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"golang.org/x/sys/cpu"
)

func TestIntegerMapLoopFastAndFallback(t *testing.T) {
	for _, prefix := range []bool{false, true} {
		t.Run(fmt.Sprintf("prefix-%v", prefix), func(t *testing.T) { testIntegerMapLoopFastAndFallback(t, prefix) })
	}
}

func testIntegerMapLoopFastAndFallback(t *testing.T, prefix bool) {
	t.Helper()
	if !cpu.X86.HasSSE41 {
		t.Skip("SSE4.1 required")
	}
	old, oldFast := integerMapLoopEnabled, integerMapTestFast
	defer func() { integerMapLoopEnabled, integerMapTestFast = old, oldFast }()
	loop := []byte{0x20, 0, 0x20, 1, 0x6a, 0x20, 2, 0x20, 1, 0x6a, 0x28, 2, 0, 0x22, 4, 0x41, 3, 0x6a, 0x20, 4, 0x20, 4, 0x41, 9, 0x4a, 0x1b, 0x36, 2, 0, 0x20, 1, 0x41, 4, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b}
	body := []byte{1, 1, 0x7f}
	if prefix {
		body = append(body, 0x41, 23)
	}
	body = append(body, 0x03, 0x40)
	body = append(body, loop...)
	body = append(body, 0x20, 4)
	if prefix {
		body = append(body, 0x6a)
	}
	body = append(body, 0x0b)
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, tc := range []struct {
		dst, src, limit uint64
		trap            bool
	}{{1024, 128, 64, false}, {1025, 129, 128, false}, {128, 128, 64, false}, {132, 128, 64, false}, {1024, 65528, 64, true}, {1024, 128, 12, false}} {
		var want uint64
		var expected []byte
		var wantErr string
		for _, on := range []bool{false, true} {
			integerMapLoopEnabled = on
			integerMapTestFast = on && tc.dst >= 1024 && tc.src < 1000 && tc.limit >= 64
			var stats ModuleStats
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: shared.AMD64ModernBaseline, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"interval-region-pins": false}}, func(mem []byte) {
				for i := range mem {
					mem[i] = byte(i*17 + 3)
				}
				for i := uint64(0); i < 32 && tc.src+i*4+4 <= uint64(len(mem)); i++ {
					v := uint32(i * 13)
					if i%3 == 0 {
						v = 0xfffffff0 + uint32(i)
					}
					binary.LittleEndian.PutUint32(mem[tc.src+i*4:], v)
				}
			}, tc.dst, 0, tc.src, tc.limit)
			if (err != nil) != tc.trap {
				t.Fatalf("case=%+v on=%v err=%v", tc, on, err)
			}
			if !on {
				want, expected, wantErr = got, mem, fmt.Sprint(err)
			} else {
				if got != want || !bytes.Equal(mem, expected) || fmt.Sprint(err) != wantErr {
					t.Fatalf("case=%+v changed result, memory or trap", tc)
				}
				if diagnosticsEnabled && stats.Funcs[0].Peephole["integer-map-loop"] != 1 {
					t.Fatalf("candidate not emitted: %v", stats.Funcs[0].Peephole)
				}
			}
		}
	}
}

func TestIntegerMapLoopWithPinExchange(t *testing.T) {
	if !cpu.X86.HasSSE41 {
		t.Skip("SSE4.1 required")
	}
	old, oldFast := integerMapLoopEnabled, integerMapTestFast
	defer func() { integerMapLoopEnabled, integerMapTestFast = old, oldFast }()
	body := []byte{1, 10, 0x7f}
	for local := byte(5); local <= 12; local++ {
		body = append(body, 0x41, local, 0x21, local)
		for j := 0; j < 80; j++ {
			body = append(body, 0x20, local, 0x1a)
		}
	}
	body = append(body, 0x20, 1, 0x21, 13)
	body = append(body, 0x03, 0x40, 0x20, 0, 0x20, 13, 0x6a, 0x20, 2, 0x20, 13, 0x6a, 0x28, 2, 0, 0x22, 4, 0x41, 3, 0x6a, 0x20, 4, 0x20, 4, 0x41, 9, 0x4a, 0x1b, 0x36, 2, 0, 0x20, 13, 0x41, 4, 0x6a, 0x22, 13, 0x20, 3, 0x47, 0x0d, 0, 0x0b, 0x20, 4)
	for local := byte(5); local <= 12; local++ {
		body = append(body, 0x20, local, 0x6a)
	}
	body = append(body, 0x10, 1, 0x0b)
	i32 := []wasm.ValType{wasm.I32}
	m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, i32, body}, funcDef{i32, i32, []byte{0, 0x20, 0, 0x0b}})
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	var want uint64
	var expected []byte
	for _, on := range []bool{false, true} {
		integerMapLoopEnabled, integerMapTestFast = on, on
		var stats ModuleStats
		got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: shared.AMD64ModernBaseline, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"interval-region-pins": false, "inline": false}}, func(mem []byte) {
			for i := uint64(0); i < 16; i++ {
				binary.LittleEndian.PutUint32(mem[128+i*4:], uint32(i*13))
			}
		}, 1024, 0, 128, 64)
		if err != nil {
			t.Fatal(on, err)
		}
		if !on {
			want, expected = got, mem
		} else {
			if got != want || !bytes.Equal(mem, expected) {
				t.Fatal("pin exchange changed result or memory")
			}
			if diagnosticsEnabled && (stats.Funcs[0].Peephole["loop-pin-exchange"] == 0 || stats.Funcs[0].Peephole["integer-map-loop"] == 0) {
				t.Fatalf("pin-exchange fast path not emitted: pins=%d %v", stats.Funcs[0].PinnedLocals, stats.Funcs[0].Peephole)
			}
		}
	}
}

func TestIntegerMapLoopInvariantReadOverlap(t *testing.T) {
	if !cpu.X86.HasSSE41 {
		t.Skip("SSE4.1 required")
	}
	old, oldFast := integerMapLoopEnabled, integerMapTestFast
	defer func() { integerMapLoopEnabled, integerMapTestFast = old, oldFast }()
	// Store load(invariant)+1 at successive addresses. If the invariant aliases
	// a destination, subsequent iterations must see the newly stored value.
	body := []byte{1, 1, 0x7f, 0x03, 0x40,
		0x20, 0, 0x20, 1, 0x6a, 0x20, 2, 0x28, 2, 0, 0x22, 4, 0x41, 1, 0x6a, 0x36, 2, 0,
		0x20, 1, 0x41, 4, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b, 0x20, 4, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, tc := range []struct{ invariant, limit uint64 }{{128, 68}, {1024, 68}, {1028, 72}, {1032, 76}, {1084, 68}} {
		var want uint64
		var expected []byte
		for _, on := range []bool{false, true} {
			integerMapLoopEnabled, integerMapTestFast = on, on
			var stats ModuleStats
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: shared.AMD64ModernBaseline, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"interval-region-pins": false}}, func(mem []byte) {
				for i := range mem {
					mem[i] = byte(i*17 + 3)
				}
			}, 1024, 0, tc.invariant, tc.limit)
			if err != nil {
				t.Fatalf("case=%+v on=%v: %v", tc, on, err)
			}
			if !on {
				want, expected = got, mem
			} else if got != want || !bytes.Equal(mem, expected) {
				t.Fatalf("case=%+v changed result or memory", tc)
			} else if diagnosticsEnabled && stats.Funcs[0].Peephole["integer-map-loop"] != 1 {
				t.Fatalf("candidate not emitted: %v", stats.Funcs[0].Peephole)
			}
		}
	}
}
