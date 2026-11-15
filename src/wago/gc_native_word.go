// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// nativeGCReferenceResult is used only for collector-reference result lanes.
// Opaque function/extern words and public tokens do not pass through it.
func (in *Instance) nativeGCReferenceResult(ref gc.Ref) uint64 {
	if gc.NativeTypeCarriersEnabled() {
		return in.gc.TryNativeReferenceWord(ref)
	}
	return uint64(ref)
}
