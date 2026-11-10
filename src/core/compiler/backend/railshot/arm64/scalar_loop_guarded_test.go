// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestGuardedScalarUnrollTailsAndUnsignedBoundaries(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, xor, lt := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x73), byte(0x49)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, add, xor, lt = wasm.I64, 0x7e, 0x42, 0x7c, 0x85, 0x54
			fold = func(x uint64) uint64 { return x }
		}
		// Both sides of the induction update read the index. Its final value
		// and an accumulator that wraps are observable after a partial group.
		body := []byte{1, 1, bt, 0x03, 0x40, 0x20, 3, 0x20, 1, 0x20, 2, xor, add, 0x21, 3,
			0x20, 1, k, 1, add, 0x21, 1, 0x20, 1, 0x20, 0, lt, 0x0d, 0,
			0x0b, 0x20, 3, 0x20, 1, add, 0x0b}
		params, results := []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}
		m := modFuncs(t, funcDef{params, results, body}, funcDef{params, results, []byte{0, 0x20, 0, 0x0b}})
		max := fold(^uint64(0))
		pairs := [][2]uint64{{0, 0}, {0, max}, {3, max}, {max, max - 1}, {max, max - 4}, {max, max - 7}}
		for n := uint64(1); n < 18; n++ {
			pairs = append(pairs, [2]uint64{n, 0})
		}
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for _, pair := range pairs {
					bound, index, value := pair[0], pair[1], fold(0x8000000000000003)
					sum, last := uint64(0), index
					for {
						sum = fold(sum + (last ^ value))
						last = fold(last + 1)
						if last >= bound {
							break
						}
					}
					var stats ModuleStats
					opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"scalar-loop-unroll": enabled, "value-facts": false}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					a, b := bound, index
					if !wide {
						a |= 0xdeadbeef00000000
						b |= 0xcafebabe00000000
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, a, b, value)
					if err != nil || got != fold(sum+last) {
						t.Fatalf("wide=%t enabled=%t workers=%d bound=%x index=%x got=%x err=%v want=%x", wide, enabled, workers, bound, index, got, err, fold(sum+last))
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-loop-unroll-guarded"] != 0) != (enabled && !profileEnabled) {
						t.Fatalf("guarded admission wide=%t enabled=%t stats=%v", wide, enabled, stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}

func TestGuardedScalarUnrollRejectsUnprovedBoundsAndFlags(t *testing.T) {
	// Counter W5, bound W1, temporary W9. These proof failures must leave the
	// already-emitted body untouched, so the established unroller can handle it.
	for _, tc := range []struct {
		words []uint32
		inc   int
		cc    Cond
	}{
		{[]uint32{0x110004a5, 0x6b0100bf}, 0, condL},                         // signed comparison
		{[]uint32{0x11000421, 0x110004a5, 0x6b0100bf}, 1, condB},             // bound changes
		{[]uint32{0x1a830043, 0x110004a5, 0x6b0100bf}, 1, condB},             // CSEL reads incoming flags
		{[]uint32{0x32000063, 0x1a830043, 0x110004a5, 0x6b0100bf}, 2, condB}, // ORR is not a flag producer
		{[]uint32{0x110004a5, 0x6b0104bf}, 0, condB},                         // shifted bound
		{[]uint32{0x110004a5, 0xeb0100bf}, 0, condB},                         // counter/comparison widths differ
	} {
		f := gpClassEmitter()
		if f.unrollGuardedScalarLatch(0, tc.words, tc.inc, X5, X9, tc.cc) {
			t.Fatalf("unsafe guarded loop admitted: %x", tc.words)
		}
		if len(f.a.B) != 0 {
			t.Fatal("rejected proof emitted instructions")
		}
	}
}
