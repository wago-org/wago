// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"encoding/binary"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Native frame readers validate the complete frame and each compiler-owned
// offset before these accesses. Numeric native addresses have no Go-heap
// provenance; avoid allocating a pointer-sized temporary under checkptr=2.
//
//go:nocheckptr
func nativeGCFrameWord(address uintptr) uint64 {
	return binary.LittleEndian.Uint64(unsafe.Slice((*byte)(offHeapPtr(address)), 8))
}

//go:nocheckptr
func nativeGCFrameRootSlot(address uintptr) *gc.Root {
	return (*gc.Root)(offHeapPtr(address))
}
