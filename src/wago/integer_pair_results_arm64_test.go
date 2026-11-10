// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && arm64 && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestIntegerPairResultsThroughPublicInvokeArm64(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, count := range []int{2, 8} {
			t.Run(fmt.Sprintf("wide=%t/args=%d", wide, count), func(t *testing.T) {
				typ, k := wasm.I32, byte(0x41)
				fold := func(x uint64) uint64 { return uint64(uint32(x)) }
				add := byte(0x6a)
				if wide {
					typ, k, add = wasm.I64, 0x42, 0x7c
					fold = func(x uint64) uint64 { return x }
				}
				params := make([]wasm.ValType, count)
				for i := range params {
					params[i] = typ
				}
				body := []byte{0x20, byte(count - 1), 0x20, 0, k, 7, add, 0x0b}
				source := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{typ, typ}))),
					wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code(body))))
				args := make([]uint64, count)
				for i := range args {
					args[i] = 0xdeadbeef80000000 + uint64(i)
				}
				args[0] = ^uint64(0)
				want := [2]uint64{fold(args[count-1]), fold(args[0] + 7)}
				for _, enabled := range []bool{false, true} {
					for _, workers := range []int{1, 4} {
						c, err := NewRuntimeConfig().WithFunctionWorkers(workers).WithOptimization("reg-abi", enabled).Compile(source)
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						in, err := Instantiate(c)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						fn, err := in.WasmFunc("run")
						if err != nil {
							t.Fatal(err)
						}
						got, err := fn.Invoke(args...)
						if err != nil || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
							t.Fatalf("enabled=%t workers=%d got=%x err=%v want=%x", enabled, workers, got, err, want)
						}
					}
				}
			})
		}
	}
}
