// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && !wago_regalloccheck

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func compileSourceModuleWith(m *wasm.Module, opts CompileOptions, validatedGCStores bool) (*encoder.CompiledModule, error) {
	return compileModuleWithValidation(m, opts, validatedGCStores)
}
