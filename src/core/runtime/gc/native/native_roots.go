// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

import "github.com/wago-org/wago/src/core/nativeabi"

// NativeRootKind and related aliases expose the native frame contract at the
// collector boundary without making compiler backends depend on collector
// implementation details.
type NativeRootKind = nativeabi.RootKind
type NativeRootSlot = nativeabi.RootSlot
type NativeRootMap = nativeabi.FunctionRootMap

const (
	NativeRootGCRef   = nativeabi.RootGCRef
	NativeRootFuncRef = nativeabi.RootFuncRef
)

// ValidateNativeRootMaps rejects malformed frame metadata before a collector or
// reference-lifecycle scanner can trust native offsets.
func ValidateNativeRootMaps(maps []NativeRootMap, localFunctions int) error {
	return nativeabi.ValidateRootMaps(maps, localFunctions)
}
