// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"math/bits"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot"
)

func TestUnsignedMagic32WideReciprocal(t *testing.T) {
	for _, d := range []uint32{3, 7, 14, 19, 31, 37, 63, 97, 127, 255, 65535, 0x7fffffff, 0x80000001, 0xfffffffe, 0xffffffff} {
		m := ^uint64(0)/uint64(d) + 1
		x := uint32(0x12345678)
		for i := range 10000 {
			x ^= x << 13
			x ^= x >> 17
			x ^= x << 5
			n := x
			if i < 6 {
				n = []uint32{0, 1, d - 1, d, d + 1, 0xffffffff}[i]
			}
			q, _ := bits.Mul64(uint64(n), m)
			if q != uint64(n/d) {
				t.Fatalf("n=%x d=%x reciprocal=%x q=%x want=%x", n, d, m, q, n/d)
			}
		}
	}
}

func TestUnsignedMagic32WideExecution(t *testing.T) {
	saved := sharedScalarEnabled
	sharedScalarEnabled = false
	defer func() { sharedScalarEnabled = saved }()
	for _, d := range []uint32{7, 14, 19, 31, 37, 63, 97, 127, 255, 65535, 0x7fffffff} {
		_, _, add := railshot.MagicU(uint64(d), 32)
		for _, rem := range []bool{false, true} {
			op := divOp{"i32.div_u", 0x6e, false, false}
			if rem {
				op = divOp{"i32.rem_u", 0x70, false, true}
			}
			m := divConstMod(t, false, op, uint64(d))
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					var stats ModuleStats
					c, err := CompileModuleWith(m, CompileOptions{Workers: workers, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"magic-div": enabled, "canonical-i32": false, "value-facts": false}})
					if err != nil {
						t.Fatal(err)
					}
					values := []uint32{0, 1, d - 1, d, d + 1, 0x7fffffff, 0x80000000, 0xffffffff}
					x := uint32(0x12345678)
					for range 128 {
						x ^= x << 13
						x ^= x >> 17
						x ^= x << 5
						values = append(values, x)
					}
					for _, n := range values {
						want := n / d
						if rem {
							want = n % d
						}
						if got := runCompiledAmd64u(t, c, uint64(n)|0xdeadbeef00000000); got != uint64(want) {
							t.Fatalf("d=%d n=%x rem=%t enabled=%t workers=%d got=%x want=%x", d, n, rem, enabled, workers, got, want)
						}
					}
					if c.CodeImage != nil {
						c.CodeImage.Close()
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["magic-wide-u32"] != 0) != (enabled && add) {
						t.Fatalf("d=%d rem=%t enabled=%t workers=%d wide reciprocal admission mismatch", d, rem, enabled, workers)
					}
				}
			}
		}
	}
}
