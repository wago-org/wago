// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func tailEntryFixture(wide, nonzeroTaken bool) []byte {
	code := make([]byte, 48)
	copy(code, []byte{0x48, 0x81, 0xec, 0, 0, 0, 0, 0x41, 0x89, 0xc1, 0x45, 0x85, 0xc9, 0x0f, 0x84})
	if wide {
		code[7], code[10] = 0x49, 0x4d
	}
	if nonzeroTaken {
		code[14] = 0x85
	}
	binary.LittleEndian.PutUint32(code[15:], 10)
	return code
}

func TestLocalTailEntryTargetsAndFallbacks(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, nonzeroTaken := range []bool{false, true} {
			code := tailEntryFixture(wide, nonzeroTaken)
			zero, nonzero := tailEdgeZero32, tailEdgeNonzero32
			if wide {
				zero, nonzero = tailEdgeZero64, tailEdgeNonzero64
			}
			wantZero, wantNonzero := 29, 19
			if nonzeroTaken {
				wantZero, wantNonzero = 19, 29
			}
			if got := localTailEntryTarget(code, 0, len(code), zero); got != wantZero {
				t.Fatalf("wide=%t nonzeroTaken=%t zero=%d want=%d", wide, nonzeroTaken, got, wantZero)
			}
			if got := localTailEntryTarget(code, 0, len(code), nonzero); got != wantNonzero {
				t.Fatalf("wide=%t nonzeroTaken=%t nonzero=%d want=%d", wide, nonzeroTaken, got, wantNonzero)
			}
			for _, mutate := range []func([]byte){
				func(b []byte) { b[0] = 0x49 },
				func(b []byte) { b[3] = 1 },    // a real frame cannot be skipped
				func(b []byte) { b[7] ^= 8 },   // operand width mismatch
				func(b []byte) { b[9] = 0xc2 }, // different parameter home
				func(b []byte) { b[12] = 0xc8 },
				func(b []byte) { b[14] = 0x8a },
				func(b []byte) { binary.LittleEndian.PutUint32(b[15:], 0) },
				func(b []byte) { binary.LittleEndian.PutUint32(b[15:], 0x7fffffff) },
			} {
				changed := append([]byte(nil), code...)
				mutate(changed)
				if got := localTailEntryTarget(changed, 0, len(changed), nonzero); got != 0 {
					t.Fatalf("unproved entry %x threaded to %d", changed[:19], got)
				}
			}
			for _, tc := range []struct {
				start, end int
				edge       uint8
			}{{0, 18, nonzero}, {0, 29, nonzero}, {-1, len(code), nonzero}, {0, len(code) + 1, nonzero}, {0, len(code), 0}, {0, len(code), 5}} {
				if got := localTailEntryTarget(code, tc.start, tc.end, tc.edge); got != tc.start {
					t.Fatalf("invalid range/edge %+v threaded to %d", tc, got)
				}
			}
			embedded := append([]byte{0xb8, 0x48}, code...)
			if got := localTailEntryTarget(embedded, 0, len(embedded), nonzero); got != 0 {
				t.Fatal("matched an instruction-like suffix instead of entry boundary")
			}
		}
	}
}

func TestLocalTailEntryRelocationsKeepOrdinaryFallback(t *testing.T) {
	for _, valid := range []bool{false, true} {
		code := make([]byte, 96)
		copy(code[32:], tailEntryFixture(false, true))
		if !valid {
			code[35] = 16
		}
		relocs := [][]callReloc{{{at: 2, target: 1, internal: true, tailEdge: tailEdgeNonzero32}, {at: 10, target: 1, internal: true, tailEdge: tailEdgeZero32}}, nil}
		if err := patchModuleRelocs(code, []int{0, 32}, []int{0, 32}, relocs, -1, [gcSharedStubMax]int{}); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct{ site, want int }{{2, 61}, {10, 51}} {
			want := tc.want
			if !valid {
				want = 32
			}
			got := tc.site + 4 + int(int32(binary.LittleEndian.Uint32(code[tc.site:])))
			if got != want {
				t.Fatalf("valid=%t site=%d target=%d want=%d", valid, tc.site, got, want)
			}
		}
	}
}

func TestLocalTailEntryFallbackPreservesABI(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, block, add := wasm.I32, byte(0x7f), byte(0x6a)
		if wide {
			typ, block, add = wasm.I64, 0x7e, 0x7c
		}
		params, results := []wasm.ValType{typ}, []wasm.ValType{typ}
		m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x12, 1, 0x0b}}, funcDef{params, results, []byte{1, 1, block, 0x20, 0, 0x20, 1, add, 0x0b}})
		for _, fold := range []bool{false, true} {
			var stats ModuleStats
			c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"branch-fold": fold}})
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range []uint64{0, 1, 31, 0xffffffff, 0x8000000000000000, ^uint64(0)} {
				input, want := n, n
				if !wide {
					input = uint64(uint32(n)) | 0xdeadbeef00000000
					want = uint64(uint32(n))
				}
				if got := runCompiledAmd64u(t, c, input); got != want {
					t.Fatalf("wide=%t fold=%t n=%x got=%x want=%x", wide, fold, n, got, want)
				}
			}
			if c.CodeImage != nil {
				c.CodeImage.Close()
			}
			if diagnosticsEnabled && (stats.Funcs[0].Peephole["tail-local-entry-thread"] != 0) != (fold && !profileEnabled) {
				t.Fatal("ordinary-entry fallback was not exercised")
			}
		}
	}
}
