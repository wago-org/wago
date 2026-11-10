// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && (tinygo || wago_profile)

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// The measured pilot uses standard Go production builds. TinyGo retains the
// established path for release size; profiling builds retain it for code
// identity whether source/unwind recording is enabled or disabled. Admission
// still precedes emission.
var sharedScalarEnabled = false

func (f *fn) admitScalar(*wasm.Func) shared.ScalarSummary { return shared.ScalarSummary{} }
func (f *fn) scalarBody(*wasm.Func) error {
	return fmt.Errorf("amd64: shared scalar pilot is unavailable in TinyGo or profiling builds")
}
