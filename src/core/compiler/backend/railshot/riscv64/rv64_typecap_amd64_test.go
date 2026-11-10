// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && !tinygo

package riscv64

import (
	"testing"

	amd "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func typeCapAMD64Control(t *testing.T, m *wasm.Module) {
	t.Helper()
	if _, err := amd.CompileModuleWith(m, amd.CompileOptions{DeferCodeMapping: true}); err != nil {
		t.Fatal("amd64 control", err)
	}
}
