// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func compileSharedDefinedCastFixture(t *testing.T, shared bool) (ModuleStats, int) {
	t.Helper()
	saved, savedInline := gcSharedStubsEnabled, gcInlineCastsEnabled
	gcSharedStubsEnabled, gcInlineCastsEnabled = shared, false
	defer func() { gcSharedStubsEnabled, gcInlineCastsEnabled = saved, savedInline }()

	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x4f, 0, 0x5f, 0},          // final struct type 0
			[]byte{0x60, 1, 0x6e, 1, 0x64, 0}, // (anyref) -> (ref 0)
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xfb, 22, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xfb, 22, 0, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats})
	if cm != nil && cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	return stats, len(cm.Code)
}

func TestModuleSharedGCDefinedCastReducesDuplicateBodies(t *testing.T) {
	requireCompilerDiagnostics(t)
	shared, sharedBytes := compileSharedDefinedCastFixture(t, true)
	_, inlineBytes := compileSharedDefinedCastFixture(t, false)
	if shared.GCSharedStubs != 1 || shared.GCSharedStubCallSites != 2 || shared.GCSharedStubBytes == 0 {
		t.Fatalf("shared defined-cast stats = bodies %d calls %d bytes %d, want 1/2/nonzero", shared.GCSharedStubs, shared.GCSharedStubCallSites, shared.GCSharedStubBytes)
	}
	if sharedBytes >= inlineBytes {
		t.Fatalf("shared defined-cast code bytes = %d, want less than inline %d", sharedBytes, inlineBytes)
	}
}
