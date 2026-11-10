// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package main

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func compileExplain(m *wasm.Module, guard, compact bool) (string, error) {
	var ms railshot.ModuleStats
	if _, err := railshot.CompileModuleWith(m, railshot.CompileOptions{
		ElideBoundsChecks: guard,
		Stats:             &ms,
		CompactNative:     compact,
	}); err != nil {
		return "", err
	}
	return ms.String(), nil
}
