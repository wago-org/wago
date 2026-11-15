// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && !tinygo && !wago_target_tinygo

package gc

import (
	"syscall"
	"unsafe"
)

// Ref stores handle<<1 in32bits. Include the unused zero slot, and reserve
// enough virtual space for every valid index. Unused pages are not touched.
const ownedHandleEntries = 1 << 31
const ownedHandleBytes = ownedHandleEntries * NativeHandleStride
const ownedHandleCapacityStep = 4096

func (c *Collector) mappedHandleBacking() []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(c.handles))), ownedHandleBytes)
}

func (c *Collector) growOwnedHandles(required int) bool {
	if required > ownedHandleEntries || c.closed {
		return false
	}
	capacity := (required + ownedHandleCapacityStep - 1) & -ownedHandleCapacityStep
	if c.handlesBacking == handlesBackingMapped {
		previous := cap(c.handles) * NativeHandleStride
		entries := unsafe.Slice((*handleEntry)(unsafe.Pointer(unsafe.SliceData(c.handles))), capacity)
		c.handles = entries[:len(c.handles):capacity]
		adviseOwnedHugePages(c.mappedHandleBacking()[:capacity*NativeHandleStride], previous)
		return true
	}
	if c.handlesBacking != handlesBackingDefault || !c.throughput.ownedBacking {
		return false
	}
	mem, err := syscall.Mmap(-1, 0, ownedHandleBytes, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		c.handlesBacking = handlesBackingFailed
		return false
	}
	entries := unsafe.Slice((*handleEntry)(unsafe.Pointer(unsafe.SliceData(mem))), capacity)
	copy(entries, c.handles)
	c.handles = entries[:len(c.handles):capacity]
	c.handlesBacking = handlesBackingMapped
	adviseOwnedHugePages(mem[:capacity*NativeHandleStride], 0)
	return true
}

func (c *Collector) releaseOwnedHandles() {
	if c.handlesBacking == handlesBackingMapped {
		err := injectFailure(c, failHandleBackingRelease)
		if err == nil {
			err = syscall.Munmap(c.mappedHandleBacking())
		}
		if err != nil {
			panic("gc: cannot release handle backing: " + err.Error())
		}
	}
	c.handlesBacking = handlesBackingDefault
}
