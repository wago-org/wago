// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestMulAddUsesLoopConstantLeases(t *testing.T) {
	body := []byte{1, 2, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x20, 0, 0x4f, 0x0d, 1,
		0x20, 2, 0x20, 1, 0x41}
	body = append(body, wasmtest.SLEB32(997)...)
	body = append(body, 0x6c, 0x41)
	body = append(body, wasmtest.SLEB32(12345)...)
	body = append(body, 0x6a, 0x6a, 0x21, 2, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, n := range []uint64{0, 1, 2, 31, 128, 4096} {
		opts := CompileOptions{}
		var stats ModuleStats
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		got, err := runArm64WrapperWithOptions(t, m, opts, n|0xdeadbeef00000000)
		want := uint64(uint32(997*n*(n-1)/2 + 12345*n))
		if err != nil || got != want {
			t.Fatalf("n=%d got=%d,%v want %d", n, got, err, want)
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["mul-add-cached-constant"] < 2 {
			t.Fatal("fused multiply-add ignored constant leases")
		}
	}
}
