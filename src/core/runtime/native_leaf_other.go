// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && (amd64 || arm64) && tinygo

package runtime

import "github.com/wago-org/wago/internal/runtimebridge"

// NativeScalarLeafRegisterEntryOffset is measured from the dispatch wrapper.
// The copied leaf is shorter than 128 bytes; 192 reserves the complete
// argument-buffer wrapper.
const NativeScalarLeafRegisterEntryOffset = 192

// NativeScalarLeafSupported reports whether this build can bind native callbacks.
const NativeScalarLeafSupported = false

func RegisterNativeScalarLeaf(runtimebridge.HostScalarCallAccess, []byte, any, uint32) (bool, error) {
	return false, nil
}
func nativeScalarLeafPtr([]byte) uintptr { return 0 }
func unregisterNativeScalarLeaf([]byte)  {}

func BindNativeScalarLeafImport(runtimebridge.HostScalarCallAccess, []byte, []byte) bool {
	return false
}
