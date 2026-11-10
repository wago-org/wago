// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && !tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestTailHelperCallerRemainsUnboundedAmd64(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x10, 1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 17, 0x6a, 0x12, 1, 0x0b, 0x0b}))))
	for _, workers := range []int{1, 4} {
		c, err := NewRuntimeConfig().WithFunctionWorkers(workers).Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		if c.directPreparedBoundedAt(0) || c.boundedNativeScalarLeaf() {
			c.Close()
			t.Fatalf("workers=%d admitted unbounded tail helper caller", workers)
		}
		in, err := Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		fn, err := in.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []uint64{0, 1, 2, 31, 4096, 100000} {
			got, err := fn.Invoke(n|0xdeadbeef00000000, 0xfffffff0)
			want := uint64(uint32(0xfffffff0 + n*17))
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("workers=%d n=%d got=%v,%v want %d", workers, n, got, err, want)
			}
		}
		in.Close()
		c.Close()
	}
}

func TestConstantTailHelperCallerRemainsUnboundedAmd64(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x41, 7, 0x10, 1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 17, 0x6a, 0x12, 1, 0x0b, 0x0b}))))
	for _, workers := range []int{1, 4} {
		c, err := NewRuntimeConfig().WithFunctionWorkers(workers).Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		if c.directPreparedBoundedAt(0) || c.directPreparedBoundedAt(1) || c.boundedNativeScalarLeaf() {
			c.Close()
			t.Fatalf("workers=%d admitted unbounded specialized tail helper", workers)
		}
		in, err := Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		fn, err := in.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []uint64{0, 1, 2, 31, 4096, 100000} {
			got, err := fn.Invoke(n | 0xdeadbeef00000000)
			want := uint64(uint32(7 + n*17))
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("workers=%d n=%d got=%v err=%v want=%d", workers, n, got, err, want)
			}
		}
		in.Close()
		c.Close()
	}
}

func TestTailPairHelperCallerRemainsUnboundedAmd64(t *testing.T) {
	member := func(value, target byte) []byte {
		return wasmtest.Code([]byte{0x20, 0, 0x45, 0x04, 0x7f, 0x41, value, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x12, target, 0x0b, 0x0b})
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(member(5, 1), member(9, 0), wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}))))
	for _, workers := range []int{1, 4} {
		c, err := NewRuntimeConfig().WithFunctionWorkers(workers).Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		if c.directPreparedBoundedAt(0) || c.directPreparedBoundedAt(1) || c.directPreparedBoundedAt(2) || c.boundedNativeScalarLeaf() {
			c.Close()
			t.Fatalf("workers=%d admitted an unbounded tail cycle", workers)
		}
		in, err := Instantiate(c)
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		fn, err := in.WasmFunc("run")
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []uint64{0, 1, 2, 31, 4096, 100000} {
			got, err := fn.Invoke(n | 0xdeadbeef00000000)
			want := uint64(5)
			if n&1 != 0 {
				want = 9
			}
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("workers=%d n=%d got=%v err=%v want=%d", workers, n, got, err, want)
			}
		}
		in.Close()
		c.Close()
	}
}
