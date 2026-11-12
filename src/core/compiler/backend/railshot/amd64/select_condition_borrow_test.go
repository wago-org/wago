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

func TestSelectBorrowedConditionPreserved(t *testing.T) {
	// Reuse the condition and evaluate a fixed-register shift in the first
	// arm. Both the select and the later local read must see the original value.
	body := []byte{0}
	for range 5 {
		body = append(body, 0x20, 0, 0x20, 1, 0x74, 0x20, 1, 0x20, 2, 0x1b, 0x1a)
	}
	body = append(body, 0x20, 0, 0x20, 1, 0x74, 0x20, 1, 0x20, 2, 0x1b, 0x20, 2, 0x6a, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if diagnosticsEnabled && stats.Funcs[0].Peephole["select-condition-read-borrow"] == 0 {
		t.Fatal("condition was not borrowed")
	}
	for _, c := range []uint64{0, 1, 2, 0xffffffff} {
		want := uint64(uint32(c) + 3)
		if c != 0 {
			want = uint64(uint32(c) + 96)
		}
		if got := runCompiledAmd64u(t, cm, 12, 3, c); got != want {
			t.Fatalf("condition=%x: got %x want %x", c, got, want)
		}
	}
}

func TestSelectBorrowedConditionUsedByDeferredArm(t *testing.T) {
	// The condition is also a shift count and the false alternative. It must
	// survive fixed-register shift lowering and remain available afterward.
	body := []byte{0}
	for range 5 {
		body = append(body, 0x20, 0, 0x20, 1, 0x74, 0x20, 1, 0x20, 1, 0x1b, 0x1a)
	}
	body = append(body, 0x20, 0, 0x20, 1, 0x74, 0x20, 1, 0x20, 1, 0x1b, 0x20, 1, 0x6a, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if diagnosticsEnabled && stats.Funcs[0].Peephole["select-condition-read-borrow"] == 0 {
		t.Fatal("condition was not borrowed")
	}
	for _, condition := range []uint32{0, 1, 2, 31, 32, ^uint32(0)} {
		want := condition
		if condition != 0 {
			want += uint32(12) << (condition & 31)
		}
		if got := runCompiledAmd64u(t, cm, 12, uint64(condition)); got != uint64(want) {
			t.Fatalf("condition=%x: got %x want %x", condition, got, want)
		}
	}
}
