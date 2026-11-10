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

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestStagedTable64AdmittedOnArm64(t *testing.T) {
	module := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 0x01, 0x02})))
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Table64 = true
	compiled, err := compileWithFrontendFeatures(cfg, module, features)
	if err != nil {
		t.Fatalf("compile table64 on arm64: %v", err)
	}
	defer compiled.Close()
	if !compiled.requiredFeatures.IsEnabled(CoreFeatureTable64) || !compiled.TableAddr64 {
		t.Fatalf("table64 metadata = features %s addr64=%v", compiled.requiredFeatures, compiled.TableAddr64)
	}
}
