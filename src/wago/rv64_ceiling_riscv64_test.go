// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Exercise the frozen receipt, entry/frame directory and actual
// instance construction together. Cross-compilation alone does not execute it.
func TestRV64CeilingRootedInstance(t *testing.T) {
	const count = 1025
	funcs, codes := make([][]byte, count), make([][]byte, count)
	for i := range funcs {
		funcs[i] = []byte{0}
		body := []byte{0x41, 7, 0x0b}
		if i == count-1 {
			body = []byte{0xfb, 1, 1, 0x1a, 0x41, 7, 0x0b}
		}
		codes[i] = wasmtest.Code(body)
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), []byte{0x5f, 0})),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, count-1))),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
	cfg := rv64RootedScalarConfig()
	compiled, err := Compile(cfg, source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Entry) != count || view.genericGCFrameRoots() == nil {
		t.Fatal("large rooted receipt/frame directory")
	}
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	module, err := rt.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	instance, err := rt.Instantiate(context.Background(), module, WithImports(NewImports()), WithGC(GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}))
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 7 {
		t.Fatal("last rooted entry", got, err)
	}
}
