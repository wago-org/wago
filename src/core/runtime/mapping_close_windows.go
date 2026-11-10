// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build windows && (amd64 || arm64)

package runtime

import "unsafe"

// closeOwnedMapping drops the owner's GC-scanned address before VirtualFree can
// make that range available to the Go heap. Keep only integer addresses across
// release; reconstruct the still-mapped view if release fails.
func closeOwnedMapping(mem *[]byte, base, size uintptr) error {
	length, capacity := len(*mem), cap(*mem)
	address := slicePtr(*mem)
	if base == 0 {
		base, size = address, uintptr(length)
	}
	*mem = nil
	if err := munmapRange(base, size); err != nil {
		if capacity != 0 {
			*mem = unsafe.Slice((*byte)(offHeapPointer(address)), capacity)[:length]
		}
		return err
	}
	return nil
}
