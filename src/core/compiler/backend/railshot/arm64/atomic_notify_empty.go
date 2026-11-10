// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/runtime/abi"

var atomicNotifyEmptyEnabled = true

// Arguments and caller homes are already published to the control frame. Both
// paths join before the ordinary result/local reconstruction, so the shortcut
// obeys the same register and GC frame contract as the helper.
func (f *fn) notifyEmptyShortcut(memoryIndex uint32) int {
	var fallback [5]int
	n := 0
	cold := func(site int) { fallback[n] = site; n++ }
	f.ld64(X9, X11, hcArgs)
	f.ld64(X10, X11, hcArgs+16)
	f.a.Adds64(X9, X9, X10)
	cold(f.a.Bcond(condAE))
	f.a.TstImm64(X9, 3)
	cold(f.a.Bcond(condNE))
	if memoryIndex == 0 && !f.threadedMemory0 {
		f.a.MovReg64(X10, linMemReg)
	} else {
		f.ld64(X10, linMemReg, -int32(offMemoryDirPtr))
		entry := f.memoryDirectoryEntry(X10, memoryIndex)
		f.ld64(X10, X10, entry+abi.MemoryDirBaseOffset)
	}
	f.a.MovImm64(X16, 4)
	f.a.Adds64(X9, X9, X16)
	cold(f.a.Bcond(condAE))
	if f.memoryShared(memoryIndex) {
		f.loadSharedMemoryBytes(X16, X10)
	} else {
		f.ld64(X16, X10, -int32(bdCurBytes))
	}
	f.cmpRR(X9, X16, true)
	cold(f.a.Bcond(condA))
	if f.memoryShared(memoryIndex) {
		f.a.SubImm64(X16, X10, abi.SharedNotifyPendingOffset)
		// Acquire alone does not order an earlier plain Wasm store before
		// this load. Pair a full barrier with wait's SC pending publication.
		f.a.DmbIsh()
		f.a.Ldar(X9, X16, 4)
		cold(f.a.Cbnz32(X9))
	}
	f.st64(X11, hcResults, ZR)
	done := f.a.Branch()
	for _, site := range fallback[:n] {
		f.patchBranch19(site, f.a.Len())
	}
	f.stats.peep("atomic-notify-empty")
	return done
}
