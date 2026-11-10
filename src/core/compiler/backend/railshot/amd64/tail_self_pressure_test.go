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

func TestSelfTailLoopRestoresRelinquishedParameters(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, mul, sub := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x6c), byte(0x6b)
		if wide {
			typ, bt, k, add, mul, sub = wasm.I64, 0x7e, 0x42, 0x7c, 0x7e, 0x7d
		}
		body := []byte{1, 4, bt}
		for i := byte(4); i < 8; i++ {
			body = append(body, 0x20, 1, 0x21, i)
		}
		body = append(body, 0x20, 0, 0x45, 0x04, bt, 0x20, 1, 0x20, 2, k, 3, mul, add, 0x20, 3, k, 5, mul, add, 0x05, 0x20, 0, 0x41, 1, 0x6b)
		var tree func(int, int)
		tree = func(depth, leaf int) {
			if depth == 0 {
				body = append(body, 0x20, byte(4+leaf%4))
				return
			}
			tree(depth-1, leaf*2)
			tree(depth-1, leaf*2+1)
			body = append(body, sub)
		}
		tree(6, 0)
		body = append(body, k, 13, k, 17, 0x12, 0, 0x0b, 0x0b)
		params := []wasm.ValType{wasm.I32, typ, typ, typ}
		m := modFuncs(t, funcDef{params, []wasm.ValType{typ}, body})
		for _, enabled := range []bool{false, true} {
			var stats ModuleStats
			opts := CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"tail-self-loop": enabled}}
			c, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range []uint64{0, 1, 2, 31} {
				args := []uint64{n | 0xdeadbeef00000000, 7, 11, 19}
				if !wide {
					for i := 1; i < 4; i++ {
						args[i] |= 0xcafebabe00000000
					}
				}
				want := uint64(124)
				if n == 0 {
					want = 135
				}
				if got := runCompiledAmd64u(t, c, args...); got != want {
					t.Fatalf("wide=%t enabled=%t n=%d got=%d want=%d", wide, enabled, n, got, want)
				}
			}
			if diagnosticsEnabled && enabled && !profileEnabled && stats.Funcs[0].Peephole["tail-self-loop"] == 0 {
				t.Fatal("fixture did not use self-tail loop")
			}
			if c.CodeImage != nil {
				c.CodeImage.Close()
			}
		}
	}
}
