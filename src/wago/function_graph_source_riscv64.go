// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !wago_standalone && !tinygo && !wago_target_tinygo

package wago

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func railshotPrimitiveFunctionGraphSource(m *wasm.Module) bool {
	return railshot.AdmitPrimitiveFunctionGraphModule(m)
}

func railshotTypeDeclarationsSource(m *wasm.Module) bool {
	return railshot.AdmitTypeDeclarations(m)
}
