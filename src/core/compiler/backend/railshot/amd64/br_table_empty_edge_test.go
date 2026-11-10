// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestBrTableDirectEmptyEdgesPreserveTargetsAndPrefix(t *testing.T) {
	for _, labels := range [][]uint32{nil, {0}, {0, 1, 2}, {3, 1, 0}, {0, 0, 2}} {
		for _, prefix := range []bool{false, true} {
			for _, outerIf := range []bool{false, true} {
				const def = uint32(3)
				m := directEmptyEdgeTable(t, labels, def, prefix, outerIf)
				for _, enabled := range []bool{false, true} {
					for _, compact := range []bool{false, true} {
						for _, workers := range []int{1, 4} {
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, CompactNative: compact, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"branch-fold": enabled, "canonical-i32": false}}
							cm, err := CompileModuleWith(m, opts)
							if err != nil {
								t.Fatal(err)
							}
							for _, index := range []uint32{0, 1, 2, 3, 4, 0x80000000, 0xffffffff} {
								label := def
								if uint64(index) < uint64(len(labels)) {
									label = labels[index]
								}
								want := uint64(1000 + label)
								if prefix {
									want += 41
								}
								if got := runCompiledAmd64u(t, cm, uint64(index)|0xdeadbeef00000000, 1); got != want {
									t.Fatalf("labels=%v prefix=%t outerIf=%t enabled=%t compact=%t workers=%d index=%x got=%d want=%d", labels, prefix, outerIf, enabled, compact, workers, index, got, want)
								}
							}
							if cm.CodeImage != nil {
								cm.CodeImage.Close()
							}
							wantMarkers := 0
							if enabled && !profileEnabled {
								wantMarkers = len(labels)
							}
							if diagnosticsEnabled && stats.Funcs[0].Peephole["br-table-empty-edge"] != wantMarkers {
								t.Fatalf("empty edge admission: %v", stats.Funcs[0].Peephole)
							}
							_, _, err = runMemAmd64WithOptions(t, m, opts, nil, 13, 0)
							var trap *rt.TrapError
							if !errors.As(err, &trap) || trap.Code != rt.TrapDivZero {
								t.Fatalf("dispatch index trap disappeared: %v", err)
							}
						}
					}
				}
			}
		}
	}
}

func directEmptyEdgeTable(t testing.TB, labels []uint32, def uint32, prefix, outerIf bool) *wasm.Module {
	t.Helper()
	body := []byte{0}
	if prefix {
		body = append(body, 0x41, 41)
	}
	for i := uint32(0); i <= def; i++ {
		if i == 0 && outerIf {
			body = append(body, 0x41, 1, 0x04, 0x40)
		} else {
			body = append(body, 0x02, 0x40)
		}
	}
	// A zero-arity edge discards this operand while retaining the frame prefix.
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(99)...)
	body = append(body, 0x20, 0, 0x20, 1, 0x6e, 0x0e)
	body = append(body, wasmtest.ULEB(uint32(len(labels)))...)
	for _, label := range labels {
		body = append(body, wasmtest.ULEB(label)...)
	}
	body = append(body, wasmtest.ULEB(def)...)
	for i := uint32(0); i <= def; i++ {
		body = append(body, 0x0b, 0x41)
		body = append(body, wasmtest.SLEB32(int32(1000+i))...)
		if prefix {
			body = append(body, 0x6a)
		}
		body = append(body, 0x0f)
	}
	body = append(body, 0x0b)
	return modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body})
}
