// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !tinygo

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestCompiledStartMetadataRejectsMalformedState(t *testing.T) {
	c, err := Compile(nil, wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(8, []byte{0}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0x41, 1, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.validateMetadata(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Compiled){
		"negative-local":  func(v *Compiled) { v.StartLocalFunc = -1 },
		"missing-local":   func(v *Compiled) { v.StartLocalFunc = len(v.Funcs) },
		"nonvoid-local":   func(v *Compiled) { v.StartLocalFunc = 1 },
		"missing-import":  func(v *Compiled) { v.StartIsImport = true },
		"inactive-kind":   func(v *Compiled) { v.HasStart = false; v.StartIsImport = true },
		"inactive-local":  func(v *Compiled) { v.HasStart = false; v.StartLocalFunc = 1 },
		"inactive-import": func(v *Compiled) { v.HasStart = false; v.StartImportIdx = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := *c.executionView()
			change(&candidate)
			if err := candidate.validateMetadata(); err == nil || !strings.Contains(err.Error(), "start function") {
				t.Fatalf("malformed start metadata: %v", err)
			}
		})
	}
	empty, err := Compile(nil, wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	candidate := *empty.executionView()
	candidate.HasStart = true
	if err := candidate.validateMetadata(); err == nil || !strings.Contains(err.Error(), "start function") {
		t.Fatalf("start without a function: %v", err)
	}
}
