// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build ((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package runtime

import (
	"sync/atomic"
	"unsafe"
)

// loadTrap and storeTrap are shared by the synchronous engine and every host
// interruption implementation. The trap buffer is mmap/VirtualAlloc-backed and
// therefore stable while native code may hold its address.
func loadTrap(trap []byte) uint32 {
	if len(trap) < 4 {
		return 0
	}
	return atomic.LoadUint32((*uint32)(unsafe.Pointer(&trap[0])))
}

func storeTrap(trap []byte, value uint32) {
	if len(trap) >= 4 {
		atomic.StoreUint32((*uint32)(unsafe.Pointer(&trap[0])), value)
	}
}
