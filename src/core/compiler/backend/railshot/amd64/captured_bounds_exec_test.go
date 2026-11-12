// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestCapturedBoundsSurviveLocalOverwrite(t *testing.T) {
	old := capturedBoundsEnabled
	defer func() { capturedBoundsEnabled = old }()
	for _, offset := range []byte{0, 4} {
		// Keep the original address on the operand stack; prove it readable;
		// perform a visible store, replace the source local, and store through
		// the captured old address. The larger offset needs its own check.
		body := []byte{0, 0x20, 0, 0x20, 0, 0x28, 2, 0, 0x1a,
			0x41, 0xe4, 0, 0x41, 0xcd, 0, 0x3a, 0, 0,
			0x20, 1, 0x21, 0, 0x41, 42, 0x36, 2, offset, 0x20, 0, 0x0b}
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, address := range []uint64{7, 65528, 65532, 65533, 0xffffffff, 0xdeadbeef00000007} {
			var want uint64
			var wantMem []byte
			var wantErr string
			for _, enabled := range []bool{false, true} {
				capturedBoundsEnabled = enabled
				var stats ModuleStats
				got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats)}, func(mem []byte) {
					for i := range mem {
						mem[i] = byte(i*29 + 9)
					}
				}, address, 123)
				if !enabled {
					want, wantMem, wantErr = got, mem, strings.SplitN(fmt.Sprint(err), "\n", 2)[0]
					continue
				}
				if got != want || strings.SplitN(fmt.Sprint(err), "\n", 2)[0] != wantErr || !bytes.Equal(mem, wantMem) {
					t.Fatalf("offset=%d address=%x got=%d want=%d err=%v wantErr=%s memoryEqual=%v", offset, address, got, want, err, wantErr, bytes.Equal(mem, wantMem))
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["captured-bounds"] != 0) != (offset == 0) {
					t.Fatalf("offset=%d stats=%v", offset, stats.Funcs[0].Peephole)
				}
			}
		}
	}
}

func TestCapturedBoundsDoNotFollowArithmetic(t *testing.T) {
	body := []byte{0, 0x20, 0, 0x20, 0, 0x28, 2, 0, 0x1a,
		0x41, 0xe4, 0, 0x41, 0xcd, 0, 0x3a, 0, 0,
		0x20, 1, 0x21, 0, 0x41, 4, 0x6a, 0x41, 42, 0x36, 2, 0, 0x20, 0, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	old := capturedBoundsEnabled
	defer func() { capturedBoundsEnabled = old }()
	for _, enabled := range []bool{false, true} {
		capturedBoundsEnabled = enabled
		_, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{}, nil, 65532, 7)
		if err == nil || mem[100] != 77 {
			t.Fatalf("enabled=%v: err=%v earlier store=%d", enabled, err, mem[100])
		}
	}
}
