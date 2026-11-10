// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestBulkCopyMinimumBounds(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, enabled := range []bool{false, true} {
		body := []byte{0, 0x41, 0, 0x41, 1, 0x41, 31, 0xfc, 10, 0, 0, 0x0b}
		m, err := wasm.DecodeModule(wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		))
		if err != nil {
			t.Fatal(err)
		}
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"memory-min-bounds": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		want := 0
		if enabled {
			want = 2
		}
		if got := stats.Funcs[0].Peephole["memory-min-bounds"]; got != want {
			t.Fatalf("enabled=%t proofs=%d want=%d", enabled, got, want)
		}
	}
}
