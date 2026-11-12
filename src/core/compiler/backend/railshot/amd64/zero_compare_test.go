// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestZeroComparePredicates(t *testing.T) {
	selected := 0
	for _, wide := range []bool{false, true} {
		typ, sub, xor := wasm.I32, byte(0x6b), byte(0x73)
		if wide {
			typ, sub, xor = wasm.I64, 0x7d, 0x85
		}
		for _, op := range []byte{sub, xor} {
			for _, inverted := range []bool{false, true} {
				for _, branch := range []bool{false, true} {
					body := []byte{0}
					if branch {
						body = append(body, 0x02, 0x40)
					}
					body = append(body, 0x20, 0, 0x20, 1, op)
					eq := false
					if wide {
						body = append(body, 0x50)
						eq = true
					}
					if inverted {
						body = append(body, 0x45)
						eq = !eq
					}
					if branch {
						body = append(body, 0x0d, 0, 0x41, 1, 0x0f, 0x0b, 0x41, 0, 0x0b)
					} else {
						body = append(body, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 0, 0x0b, 0x0b)
					}
					m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{wasm.I32}, body)
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"tuple-if-select": false, "pure-integer-if-select": false}})
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					if diagnosticsEnabled {
						selected += stats.Funcs[0].Peephole["zero-compare"]
					}
					for _, p := range [][2]uint64{{0, 0}, {1, 0}, {0, 1}, {0xffffffff, 1}, {0x80000000, 0}, {^uint64(0), 0}, {1 << 63, 1 << 63}} {
						equal := p[0] == p[1]
						if !wide {
							equal = uint32(p[0]) == uint32(p[1])
						}
						truth := equal == eq
						if branch {
							truth = !truth
						}
						got := runCompiledAmd64u(t, cm, p[0], p[1])
						if (got != 0) != truth {
							t.Fatalf("wide=%v op=%x inverted=%v branch=%v args=%x got=%d", wide, op, inverted, branch, p, got)
						}
					}
				}
			}
		}
	}
	if diagnosticsEnabled && selected == 0 {
		t.Fatal("no test exercised the zero comparison lowering")
	}
}
