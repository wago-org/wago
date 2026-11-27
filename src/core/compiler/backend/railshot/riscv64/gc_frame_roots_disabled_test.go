// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo && wago_no_gc && wago_no_exception_handling

package riscv64

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64DisabledCollectorFramePlans(t *testing.T) {
	roots := &shared.GCModuleFrameRootPlan{}
	for _, tc := range []struct {
		name string
		opts CompileOptions
	}{
		{"plan", CompileOptions{GCFrameRoots: roots}},
		{"source witness", CompileOptions{GCFrameRootsSource: true}},
		{"both", CompileOptions{GCFrameRoots: roots, GCFrameRootsSource: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CompileModuleWith(&wasm.Module{}, tc.opts)
			if err == nil || !strings.Contains(err.Error(), "GC frame-root source plan") {
				t.Fatalf("frame plan in a build without exact maps: %v", err)
			}
		})
	}
	if _, err := compileDecodedModule(&wasm.Module{}, nil, CompileOptions{GCFrameRoots: roots}); err == nil || !strings.Contains(err.Error(), "GC frame-root source plan") {
		t.Fatalf("decoded lowering accepted a frame plan in a build without exact maps: %v", err)
	}
}
