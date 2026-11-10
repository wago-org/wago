// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_standalone_generator && !wago_standalone

package wago

import "github.com/wago-org/wago/internal/standalonegen"

// The build helper also compiles the runtime's managed-call trampoline. Final
// executables link that factory instead of compiling an internal Wasm module.
func init() {
	standalonegen.Dispatcher = func() (any, error) { return Compile(nil, managedVoidDispatcherWasm) }
}
