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

func TestIntegerMapPackedStreams(t *testing.T) {
	if !cpu.X86.HasSSE41 {
		t.Skip("SSE4.1 required")
	}
	old, oldFast, oldPack, oldReuse, oldWide, oldIncrement, oldAddress, oldSLP, oldSLPWide := integerMapLoopEnabled, integerMapTestFast, integerMapPackEnabled, integerMapInvariantReuse, integerMapWideInterleave, integerMapIncrementCache, integerMapAddressCache, integerMapSLPEnabled, integerMapSLPWide
	defer func() {
		integerMapLoopEnabled, integerMapTestFast, integerMapPackEnabled, integerMapInvariantReuse, integerMapWideInterleave, integerMapIncrementCache, integerMapAddressCache, integerMapSLPEnabled, integerMapSLPWide = old, oldFast, oldPack, oldReuse, oldWide, oldIncrement, oldAddress, oldSLP, oldSLPWide
	}()
	// Two adjacent word stores from the same pair of streams, with an invariant
	// load between them. The invariant may alias either output inside a packet.
	body := []byte{0x00, 0x03, 0x40,
		0x20, 0, 0x20, 1, 0x6a,
		0x20, 2, 0x20, 1, 0x6a, 0x28, 2, 0, 0x20, 4, 0x28, 2, 0, 0x6a, 0x36, 2, 0,
		0x20, 0, 0x20, 1, 0x6a, 0x41, 4, 0x6a,
		0x20, 2, 0x20, 1, 0x6a, 0x41, 4, 0x6a, 0x28, 2, 0, 0x20, 4, 0x28, 2, 0, 0x6a, 0x36, 2, 0,
		0x20, 1, 0x41, 8, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b, 0x20, 1, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	cases := []struct {
		dst, src, limit, inv uint64
		fast                 bool
	}{
		{1024, 2048, 128, 4096, true}, {1024, 2048, 128, 1024, true}, {1024, 2048, 128, 1028, true},
		{1024, 2048, 128, 1084, true}, {1024, 2048, 128, 1148, true}, {1024, 2048, 128, 2048, true},
		{1024, 2048, 136, 4096, true}, {1024, 2048, 144, 1028, true}, {1024, 2048, 152, 1152, true},
		{1024, 2048, 128, 65532, true}, {65408, 2048, 128, 4096, true}, {1024, 65408, 128, 4096, true},
		{65409, 2048, 128, 4096, false}, {1024, 65409, 128, 4096, false},
		{1024, 1024, 128, 4096, false}, {1024, 1028, 128, 4096, false}, {1028, 1024, 128, 4096, false},
		{65520, 2048, 32, 4096, false}, {1024, 65520, 32, 4096, false}, {1024, 2048, 128, 65536, false},
		{0xfffffffc, 2048, 32, 4096, false}, {1024, 0xfffffffc, 32, 4096, false}, {1024, 2048, 8, 4096, false},
		{1024, 2048, 32, 1020, false}, {1024, 2048, 40, 1028, false},
	}
	// Sweep just before, within, and just beyond the full output span; the
	// unaligned four-byte invariant access can overlap either packet edge.
	for offset := uint64(0); offset < 136; offset++ {
		cases = append(cases, struct {
			dst, src, limit, inv uint64
			fast                 bool
		}{1024, 2048, 128, 1020 + offset, true})
	}
	// Exercise packet-to-scalar transitions at each short-loop tail, including
	// byte-overlap on either edge of the output span.
	for _, limit := range []uint64{136, 144, 152} {
		for offset := uint64(0); offset < limit+8; offset++ {
			cases = append(cases, struct {
				dst, src, limit, inv uint64
				fast                 bool
			}{1024, 2048, limit, 1020 + offset, true})
		}
	}
	cacheHits := 0
	for _, tc := range cases {
		var expected []byte
		var result uint64
		var trap string
		for mode := 0; mode < 19; mode++ {
			if mode >= 15 && mode < 17 && !cpu.X86.HasAVX2 {
				continue
			}
			variant := mode
			if mode >= 17 {
				variant -= 10
			} else if mode >= 15 {
				variant -= 8
			} else if mode >= 13 {
				variant -= 6
			} else if mode >= 11 {
				variant -= 4
			} else if mode >= 9 {
				variant -= 2
			}
			on, packed, reuse, wide := variant != 0, variant != 0 && (variant-1)%4 >= 2, variant != 0 && variant%2 == 0, variant >= 5
			integerMapLoopEnabled, integerMapPackEnabled, integerMapInvariantReuse, integerMapWideInterleave, integerMapIncrementCache, integerMapAddressCache, integerMapSLPEnabled, integerMapSLPWide = on, packed, reuse, wide, mode < 9 || mode >= 11, mode < 11 || mode >= 13, mode < 13 || mode >= 15, mode >= 15
			integerMapTestFast = on && tc.fast
			var stats ModuleStats
			features := shared.AMD64ModernBaseline
			if mode >= 15 && mode < 17 {
				features |= shared.AMD64AVX2
			} else if mode == 18 {
				features = shared.AMD64SSE41 | shared.AMD64AVX2
			}
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats), AMD64FeaturesSet: true, AMD64Features: features, Optimizations: map[string]bool{"interval-region-pins": false}}, func(mem []byte) {
				for i := range mem {
					mem[i] = byte(i*17 + 3)
				}
			}, tc.dst, 0, tc.src, tc.limit, tc.inv)
			if diagnosticsEnabled && tc.fast && mode >= 15 && mode < 17 {
				cacheHits += stats.Funcs[0].Peephole["integer-map-wide-invariant-cache"]
			}
			if on && tc.fast && diagnosticsEnabled && (stats.Funcs[0].Peephole["integer-map-packed-stores"] != 0) != packed {
				t.Fatalf("packed=%v selection=%v", packed, stats.Funcs[0].Peephole)
			}
			if on && tc.fast && packed && wide && diagnosticsEnabled && (stats.Funcs[0].Peephole["integer-map-slp"] != 0) != integerMapSLPEnabled {
				t.Fatalf("slp=%v selection=%v", integerMapSLPEnabled, stats.Funcs[0].Peephole)
			}
			if on && tc.fast && packed && wide && diagnosticsEnabled && (stats.Funcs[0].Peephole["integer-map-slp-wide"] != 0) != (mode >= 15 && mode < 17) {
				t.Fatalf("mode=%d wide selection=%v", mode, stats.Funcs[0].Peephole)
			}
			if !on {
				expected = mem
				result = got
				trap = fmt.Sprint(err)
				continue
			}
			if got != result || !bytes.Equal(mem, expected) || fmt.Sprint(err) != trap {
				t.Fatalf("mode=%d case=%+v got=%d want=%d memory=%v trap=%v want=%s", mode, tc, got, result, bytes.Equal(mem, expected), err, trap)
			}
		}
	}
	if diagnosticsEnabled && cpu.X86.HasAVX2 && integerMapWideInvariantCache && cacheHits == 0 {
		t.Fatal("alias sweep did not select invariant caching")
	}
	integerMapLoopEnabled, integerMapPackEnabled, integerMapWideInterleave, integerMapSLPEnabled, integerMapSLPWide, integerMapTestFast = true, true, true, true, true, false
	for _, tc := range []struct {
		features shared.AMD64Features
		wide     bool
	}{
		{shared.AMD64ModernBaseline, false},
		{shared.AMD64SSE41 | shared.AMD64AVX2, false},
		{shared.AMD64ModernBaseline | shared.AMD64AVX2, true},
	} {
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), AMD64FeaturesSet: true, AMD64Features: tc.features, Optimizations: map[string]bool{"interval-region-pins": false}})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		if cm.RequiresAVX2 != tc.wide {
			t.Fatalf("features=%x requires AVX2=%v, want %v", tc.features, cm.RequiresAVX2, tc.wide)
		}
		if tc.wide && !shared.AMD64Features(cm.RequiredAMD64Features).Has(shared.AMD64AVX|shared.AMD64AVX2) {
			t.Fatalf("missing wide requirements: %x", cm.RequiredAMD64Features)
		}
		if diagnosticsEnabled && (stats.Funcs[0].Peephole["integer-map-slp-wide"] != 0) != tc.wide {
			t.Fatalf("features=%x selection=%v", tc.features, stats.Funcs[0].Peephole)
		}
	}
}

func TestIntegerMapPackedSignedMinimumAndLiveOut(t *testing.T) {
	if !cpu.X86.HasSSE41 {
		t.Skip("SSE4.1 required")
	}
	old, oldFast, oldPack, oldSLP, oldSLPWide := integerMapLoopEnabled, integerMapTestFast, integerMapPackEnabled, integerMapSLPEnabled, integerMapSLPWide
	defer func() {
		integerMapLoopEnabled, integerMapTestFast, integerMapPackEnabled, integerMapSLPEnabled, integerMapSLPWide = old, oldFast, oldPack, oldSLP, oldSLPWide
	}()
	body := []byte{1, 2, 0x7f, 0x03, 0x40,
		0x20, 0, 0x20, 1, 0x6a,
		0x20, 0, 0x20, 1, 0x6a, 0x28, 2, 0, 0x22, 5,
		0x20, 2, 0x20, 1, 0x6a, 0x28, 2, 0, 0x20, 4, 0x28, 2, 0, 0x6a, 0x22, 6,
		0x20, 6, 0x20, 5, 0x4a, 0x1b, 0x36, 2, 0,
		0x20, 0, 0x20, 1, 0x6a, 0x41, 4, 0x6a,
		0x20, 0, 0x20, 1, 0x6a, 0x41, 4, 0x6a, 0x28, 2, 0, 0x22, 5,
		0x20, 2, 0x20, 1, 0x6a, 0x41, 4, 0x6a, 0x28, 2, 0, 0x20, 4, 0x28, 2, 0, 0x6a, 0x22, 6,
		0x20, 6, 0x20, 5, 0x4a, 0x1b, 0x36, 2, 0,
		0x20, 1, 0x41, 8, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b,
		0x20, 5, 0x20, 6, 0x6a, 0x20, 1, 0x6a, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	cases := []struct {
		dst, src, limit, inv uint64
		fast                 bool
	}{
		{1024, 2048, 128, 4096, true}, {1024, 2048, 128, 1024, true}, {1024, 2048, 128, 1028, true},
		{1024, 2048, 128, 1087, true}, {1024, 2048, 136, 1149, true}, {1024, 1024, 128, 4096, false},
		{1024, 1028, 128, 4096, false}, {65520, 2048, 32, 4096, false}, {1024, 65520, 32, 4096, false},
	}
	for offset := uint64(0); offset < 132; offset++ {
		cases = append(cases, struct {
			dst, src, limit, inv uint64
			fast                 bool
		}{1024, 2048, 128, 1024 + offset, true})
	}
	cacheHits := 0
	for _, tc := range cases {
		var expected []byte
		var result uint64
		var trap string
		for mode := 0; mode < 4; mode++ {
			if mode == 3 && !cpu.X86.HasAVX2 {
				continue
			}
			on := mode != 0
			integerMapLoopEnabled, integerMapPackEnabled = on, on
			integerMapSLPEnabled, integerMapSLPWide = mode >= 2, mode == 3
			integerMapTestFast = on && tc.fast
			var stats ModuleStats
			features := shared.AMD64ModernBaseline
			if mode == 3 {
				features |= shared.AMD64AVX2
			}
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats), AMD64FeaturesSet: true, AMD64Features: features, Optimizations: map[string]bool{"interval-region-pins": false}}, func(mem []byte) {
				for i := range mem {
					mem[i] = byte(i*97 + 131)
				}
			}, tc.dst, 0, tc.src, tc.limit, tc.inv)
			if diagnosticsEnabled && tc.fast && mode == 3 {
				cacheHits += stats.Funcs[0].Peephole["integer-map-wide-invariant-cache"]
			}
			if on && tc.fast && diagnosticsEnabled && stats.Funcs[0].Peephole["integer-map-packed-stores"] == 0 {
				t.Fatalf("packed path not selected: %v", stats.Funcs[0].Peephole)
			}
			if on && tc.fast && diagnosticsEnabled && (stats.Funcs[0].Peephole["integer-map-slp"] != 0) != integerMapSLPEnabled {
				t.Fatalf("slp=%v selection=%v", integerMapSLPEnabled, stats.Funcs[0].Peephole)
			}
			if on && tc.fast && diagnosticsEnabled && (stats.Funcs[0].Peephole["integer-map-slp-wide"] != 0) != (mode == 3) {
				t.Fatalf("mode=%d wide selection=%v", mode, stats.Funcs[0].Peephole)
			}
			if !on {
				expected = mem
				result = got
				trap = fmt.Sprint(err)
				continue
			}
			if got != result || !bytes.Equal(mem, expected) || fmt.Sprint(err) != trap {
				t.Fatalf("case=%+v got=%d want=%d memory=%v trap=%v want=%s", tc, got, result, bytes.Equal(mem, expected), err, trap)
			}
		}
	}
	if diagnosticsEnabled && cpu.X86.HasAVX2 && integerMapWideInvariantCache && cacheHits == 0 {
		t.Fatal("live-out alias sweep did not select invariant caching")
	}
}

func TestIntegerMapSLPExpressions(t *testing.T) {
	if !cpu.X86.HasSSE41 {
		t.Skip("SSE4.1 required")
	}
	old, oldFast, oldSLP, oldSLPWide := integerMapLoopEnabled, integerMapTestFast, integerMapSLPEnabled, integerMapSLPWide
	defer func() {
		integerMapLoopEnabled, integerMapTestFast, integerMapSLPEnabled, integerMapSLPWide = old, oldFast, oldSLP, oldSLPWide
	}()
	for _, op := range []byte{0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73} {
		for shape := 0; shape < 4; shape++ {
			rightOp := op
			if shape == 1 {
				rightOp = 0x6a
				if rightOp == op {
					rightOp = 0x6b
				}
			}
			body := []byte{0, 0x03, 0x40,
				0x20, 0, 0x20, 1, 0x6a,
				0x20, 2, 0x20, 1, 0x6a, 0x28, 2, 0, 0x20, 4, 0x28, 2, 0, op, 0x36, 2, 0,
				0x20, 0, 0x20, 1, 0x6a, 0x41, 4, 0x6a}
			if shape == 2 {
				body = append(body, 0x20, 4, 0x28, 2, 0)
			}
			body = append(body, 0x20, 2, 0x20, 1, 0x6a, 0x41, 4, 0x6a, 0x28, 2, 0)
			if shape != 2 {
				body = append(body, 0x20, 4, 0x28, 2, 0)
			}
			body = append(body, rightOp, 0x36, 2, 0,
				0x20, 1, 0x41, 8, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b, 0x20, 1, 0x0b)
			if shape == 3 {
				// Pair adjacent sources in the opposite lane order.
				short := []byte{0x20, 2, 0x20, 1, 0x6a, 0x28, 2, 0}
				long := []byte{0x20, 2, 0x20, 1, 0x6a, 0x41, 4, 0x6a, 0x28, 2, 0}
				body = bytes.Replace(body, long, short, 1)
				body = bytes.Replace(body, short, long, 1)
			}
			m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			for _, tc := range []struct {
				dst, src, limit, inv uint64
				fast                 bool
			}{
				{1024, 2048, 128, 4096, true}, {1024, 2048, 136, 1028, true},
				{1024, 2048, 144, 1095, true}, {1024, 2048, 152, 1152, true},
				{1024, 1024, 128, 4096, false}, {65520, 2048, 32, 4096, false},
				{1024, 0xfffffffc, 128, 4096, false},
			} {
				var expected []byte
				var result uint64
				var trap string
				for mode := 0; mode < 4; mode++ {
					if mode == 3 && !cpu.X86.HasAVX2 {
						continue
					}
					integerMapLoopEnabled, integerMapTestFast, integerMapSLPEnabled, integerMapSLPWide = mode != 0, mode != 0 && tc.fast, mode >= 2, mode == 3
					var stats ModuleStats
					features := shared.AMD64ModernBaseline
					if mode == 3 {
						features |= shared.AMD64AVX2
					}
					got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats), AMD64FeaturesSet: true, AMD64Features: features, Optimizations: map[string]bool{"interval-region-pins": false}}, func(mem []byte) {
						for i := range mem {
							mem[i] = byte(i*97 + 131)
						}
					}, tc.dst, 0, tc.src, tc.limit, tc.inv)
					if mode == 0 {
						expected, result, trap = mem, got, fmt.Sprint(err)
						continue
					}
					wantSLP := mode >= 2 && (shape == 0 || shape == 2 && op != 0x6b || shape == 3)
					if tc.fast && diagnosticsEnabled && (stats.Funcs[0].Peephole["integer-map-slp"] != 0) != wantSLP {
						t.Fatalf("op=%x shape=%d mode=%d selection=%v", op, shape, mode, stats.Funcs[0].Peephole)
					}
					if tc.fast && diagnosticsEnabled && (stats.Funcs[0].Peephole["integer-map-slp-wide"] != 0) != (wantSLP && mode == 3) {
						t.Fatalf("op=%x shape=%d mode=%d wide selection=%v", op, shape, mode, stats.Funcs[0].Peephole)
					}
					if got != result || !bytes.Equal(mem, expected) || fmt.Sprint(err) != trap {
						t.Fatalf("op=%x shape=%d mode=%d case=%+v got=%d want=%d memory=%v trap=%v want=%s", op, shape, mode, tc, got, result, bytes.Equal(mem, expected), err, trap)
					}
				}
			}
		}
	}
}

func TestIntegerMapSLPNestedExpressions(t *testing.T) {
	if !cpu.X86.HasSSE41 {
		t.Skip("SSE4.1 required")
	}
	old, oldFast, oldSLP, oldSLPWide := integerMapLoopEnabled, integerMapTestFast, integerMapSLPEnabled, integerMapSLPWide
	defer func() {
		integerMapLoopEnabled, integerMapTestFast, integerMapSLPEnabled, integerMapSLPWide = old, oldFast, oldSLP, oldSLPWide
	}()
	ops := []byte{0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73}
	for _, inner := range ops {
		for _, outer := range ops {
			body := []byte{0, 0x03, 0x40,
				0x20, 0, 0x20, 1, 0x6a,
				0x20, 2, 0x20, 1, 0x6a, 0x28, 2, 0, 0x20, 4, 0x28, 2, 0, inner, 0x41, 7, outer, 0x36, 2, 0,
				0x20, 0, 0x20, 1, 0x6a, 0x41, 4, 0x6a,
				0x20, 2, 0x20, 1, 0x6a, 0x41, 4, 0x6a, 0x28, 2, 0, 0x20, 4, 0x28, 2, 0, inner, 0x41, 7, outer, 0x36, 2, 0,
				0x20, 1, 0x41, 8, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b, 0x20, 1, 0x0b}
			m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			for _, inv := range []uint64{4096, 1028} {
				var expected []byte
				var result uint64
				for mode := 0; mode < 4; mode++ {
					if mode == 3 && !cpu.X86.HasAVX2 {
						continue
					}
					integerMapLoopEnabled, integerMapTestFast, integerMapSLPEnabled, integerMapSLPWide = mode != 0, mode != 0, mode >= 2, mode == 3
					var stats ModuleStats
					features := shared.AMD64ModernBaseline
					if mode == 3 {
						features |= shared.AMD64AVX2
					}
					got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats), AMD64FeaturesSet: true, AMD64Features: features, Optimizations: map[string]bool{"interval-region-pins": false}}, func(mem []byte) {
						for i := range mem {
							mem[i] = byte(i*31 + 109)
						}
					}, 1024, 0, 2048, 136, inv)
					if err != nil {
						t.Fatal(err)
					}
					if mode == 0 {
						expected, result = mem, got
						continue
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["integer-map-slp"] != 0) != (mode >= 2) {
						t.Fatalf("inner=%x outer=%x mode=%d selection=%v", inner, outer, mode, stats.Funcs[0].Peephole)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["integer-map-slp-wide"] != 0) != (mode == 3) {
						t.Fatalf("inner=%x outer=%x mode=%d wide selection=%v", inner, outer, mode, stats.Funcs[0].Peephole)
					}
					if got != result || !bytes.Equal(mem, expected) {
						t.Fatalf("inner=%x outer=%x mode=%d inv=%d got=%d want=%d memory=%v", inner, outer, mode, inv, got, result, bytes.Equal(mem, expected))
					}
				}
			}
		}
	}
}
