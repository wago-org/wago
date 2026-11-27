// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo && wago_no_reference_types && wago_no_typed_function_references && wago_no_gc && wago_no_exception_handling

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func admitLocalMutableFunctionGlobals(*wasm.Module) (bool, error) {
	return false, nil
}

func admitLocalFunctionGlobal(wasm.Global, *wasm.Module, int, bool) error {
	return unsupported("reference-valued globals are disabled in this build")
}
