// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin) && arm64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestArm64TailCallFeatureAdmission(t *testing.T) {
	if !SupportedFeatures().IsEnabled(CoreFeatureTailCall) {
		t.Fatal("arm64 explicit-bounds backend must advertise tail-call execution")
	}
	if err := NewRuntimeConfig().WithFeature(CoreFeatureTailCall, true).Validate(); err != nil {
		t.Fatalf("Validate tail-call feature: %v", err)
	}
}

func TestStagedDirectTailCallsCompileOnArm64(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x12, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x00, 0x0b}),
		)),
	)
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TailCalls = true
	compiled, err := compileWithFrontendFeatures(cfg, module, features)
	if err != nil {
		t.Fatalf("staged arm64 direct-tail compile: %v", err)
	}
	_ = compiled.Close()
}
