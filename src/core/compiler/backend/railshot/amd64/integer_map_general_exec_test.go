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
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"golang.org/x/sys/cpu"
)

func TestIntegerMapGeneralExpressions(t *testing.T) {
	if !cpu.X86.HasSSE41 {
		t.Skip("SSE4.1 required")
	}
	old, oldFast := integerMapLoopEnabled, integerMapTestFast
	defer func() { integerMapLoopEnabled, integerMapTestFast = old, oldFast }()
	// The second local is the loop counter; every operation executes modulo i32.
	for _, op := range []byte{0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73} {
		for _, constant := range []byte{0, 1, 7, 31, 63} {
			body := []byte{0, 0x03, 0x40,
				0x20, 0, 0x20, 1, 0x6a,
				0x20, 2, 0x20, 1, 0x6a, 0x28, 2, 0, 0x41, constant, op, 0x36, 2, 0,
				0x20, 1, 0x41, 4, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b, 0x20, 1, 0x0b}
			m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			cases := []struct {
				dst, src, limit uint64
				fast            bool
			}{
				{1024, 2048, 64, true}, {1025, 2049, 68, true}, {1024, 2048, 124, true}, {1024, 2048, 128, true},
				{1024, 1024, 64, true}, {1024, 1028, 64, false}, {1028, 1024, 64, false},
				{65520, 2048, 64, false}, {1024, 65520, 64, false}, {0xfffffffc, 2048, 64, false},
			}
			for _, tc := range cases {
				var expected []byte
				var result uint64
				var trap string
				for _, on := range []bool{false, true} {
					integerMapLoopEnabled = on
					integerMapTestFast = on && tc.fast
					var stats ModuleStats
					got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats), AMD64FeaturesSet: true, AMD64Features: shared.AMD64ModernBaseline, Optimizations: map[string]bool{"interval-region-pins": false}}, func(mem []byte) {
						for i := range mem {
							mem[i] = byte(i*73 + 17)
						}
					}, tc.dst, 0, tc.src, tc.limit)
					if !on {
						expected = mem
						result = got
						trap = fmt.Sprint(err)
						continue
					}
					if tc.fast && diagnosticsEnabled && stats.Funcs[0].Peephole["integer-map-loop"] == 0 {
						t.Fatalf("op=%x const=%d fast path not selected: %v", op, constant, stats.Funcs[0].Peephole)
					}
					if got != result || !bytes.Equal(mem, expected) || fmt.Sprint(err) != trap {
						t.Fatalf("op=%x const=%d case=%+v got=%d want=%d memory=%v trap=%v want=%s", op, constant, tc, got, result, bytes.Equal(mem, expected), err, trap)
					}
				}
			}
		}
	}
}

func TestIntegerMapChainedStreams(t *testing.T) {
	if !cpu.X86.HasSSE41 {
		t.Skip("SSE4.1 required")
	}
	old, oldFast := integerMapLoopEnabled, integerMapTestFast
	defer func() { integerMapLoopEnabled, integerMapTestFast = old, oldFast }()
	// Each source is 100 bytes beyond the previous one. The final source is
	// outside the direct sharing range of the first but shares through the second.
	body := []byte{0, 0x03, 0x40,
		0x20, 0, 0x20, 1, 0x6a,
		0x20, 2, 0x20, 1, 0x6a, 0x28, 2, 0,
		0x20, 2, 0x20, 1, 0x6a, 0x41, 0xe4, 0x00, 0x6a, 0x28, 2, 0, 0x6a,
		0x20, 2, 0x20, 1, 0x6a, 0x41, 0xc8, 0x01, 0x6a, 0x28, 2, 0, 0x6a, 0x36, 2, 0,
		0x20, 1, 0x41, 4, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b, 0x20, 1, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, limit := range []uint64{64, 68, 128} {
		var expected []byte
		var result uint64
		for mode := 0; mode < 2; mode++ {
			integerMapLoopEnabled, integerMapTestFast = mode != 0, mode != 0
			var stats ModuleStats
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats), AMD64FeaturesSet: true, AMD64Features: shared.AMD64ModernBaseline, Optimizations: map[string]bool{"interval-region-pins": false}}, func(mem []byte) {
				for i := range mem {
					mem[i] = byte(i*73 + 17)
				}
			}, 1024, 0, 2048, limit)
			if err != nil {
				t.Fatal(err)
			}
			if mode == 0 {
				expected, result = mem, got
				continue
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["integer-map-loop"] == 0 {
				t.Fatalf("fast path not selected: %v", stats.Funcs[0].Peephole)
			}
			if got != result || !bytes.Equal(mem, expected) {
				t.Fatalf("mode=%d limit=%d got=%d want=%d memory=%v", mode, limit, got, result, bytes.Equal(mem, expected))
			}
		}
	}
}

func TestIntegerMapAffineLiveOut(t *testing.T) {
	if !cpu.X86.HasSSE41 {
		t.Skip("SSE4.1 required")
	}
	old, oldFast, oldExit := integerMapLoopEnabled, integerMapTestFast, integerMapAffineExit
	defer func() { integerMapLoopEnabled, integerMapTestFast, integerMapAffineExit = old, oldFast, oldExit }()
	// Save a computed load address and a modular affine expression in locals.
	// Both are observed after the loop, including after scalar packet fallback.
	body := []byte{1, 2, 0x7f, 0x03, 0x40,
		0x20, 0, 0x20, 1, 0x6a,
		0x20, 2, 0x20, 1, 0x6a, 0x22, 5, 0x28, 2, 0, 0x36, 2, 0,
		0x20, 1, 0x41, 3, 0x6c, 0x20, 4, 0x6a, 0x21, 6,
		0x20, 1, 0x41, 4, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b,
		0x41, 8, 0x20, 5, 0x36, 2, 0, 0x41, 12, 0x20, 6, 0x36, 2, 0, 0x20, 1, 0x0b}
	assign := []byte{0x20, 1, 0x41, 3, 0x6c, 0x20, 4, 0x6a, 0x21, 6}
	postBody := bytes.Replace(body, assign, nil, 1)
	postBody = bytes.Replace(postBody, []byte{0x22, 1, 0x20, 3}, append(append([]byte{0x22, 1}, assign...), 0x20, 3), 1)
	for _, tc := range []struct {
		dst, start, src, limit, base uint64
		fast, post                   bool
	}{
		{1024, 0, 2048, 64, 0xfffffff0, true, false},
		{1024, 16, 2048, 84, 0xfffffffe, true, false},
		{1024, 0, 1024, 68, 7, true, false},
		{1024, 0, 2048, 64, 0xfffffff0, true, true},
		{1024, 16, 2048, 84, 0xfffffffe, true, true},
		{65520, 0, 2048, 64, 13, false, false},
		{1024, 0, 65520, 64, 17, false, true},
	} {
		source := body
		if tc.post {
			source = postBody
		}
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, source)
		var expected []byte
		var result uint64
		var trap string
		for mode := 0; mode < 3; mode++ {
			integerMapLoopEnabled, integerMapTestFast, integerMapAffineExit = mode != 0, mode != 0 && tc.fast, mode == 2
			var stats ModuleStats
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats), AMD64FeaturesSet: true, AMD64Features: shared.AMD64ModernBaseline, Optimizations: map[string]bool{"interval-region-pins": false}}, func(mem []byte) {
				for i := range mem {
					mem[i] = byte(i*73 + 17)
				}
			}, tc.dst, tc.start, tc.src, tc.limit, tc.base)
			if mode == 0 {
				expected, result, trap = mem, got, fmt.Sprint(err)
				continue
			}
			if tc.fast && diagnosticsEnabled && stats.Funcs[0].Peephole["integer-map-loop"] == 0 {
				t.Fatalf("fast path not selected: %v", stats.Funcs[0].Peephole)
			}
			if got != result || !bytes.Equal(mem, expected) || fmt.Sprint(err) != trap {
				t.Fatalf("mode=%d case=%+v got=%d want=%d memory=%v trap=%v want=%s", mode, tc, got, result, bytes.Equal(mem, expected), err, trap)
			}
		}
	}
}
