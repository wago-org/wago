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

// mappedBacking reconstructs the original mmap extent from its immutable limit.
// mem always keeps its base while mapped; only its logical length/capacity change.
// The two owner flags fit the existing throughputHeap padding.
func (h *throughputHeap) mappedBacking() []byte {
	return unsafe.Slice(unsafe.SliceData(h.mem), int(h.limit))
}

func (h *throughputHeap) growOwnedBacking(reserve int) bool {
	if h.mapped {
		previous := len(h.mem)
		h.mem = h.mappedBacking()[:reserve:reserve]
		adviseOwnedHugePages(h.mem, previous)
		return true
	}
	if !h.ownedBacking {
		return false
	}
	mem, err := syscall.Mmap(-1, 0, int(h.limit), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		// A full address reservation can fail even when a small Go growth fits.
		// Preserve that allocation and avoid repeated failed reservations.
		h.ownedBacking = false
		return false
	}
	h.mem = mem[:reserve:reserve]
	h.mapped = true
	adviseOwnedHugePages(h.mem, 0)
	return true
}

func (h *throughputHeap) releaseOwnedBacking() {
	if !h.mapped {
		return
	}
	if err := syscall.Munmap(h.mappedBacking()); err != nil {
		// Do not discard ownership or claim a successful release on an OS error.
		panic("gc: cannot release throughput backing: " + err.Error())
	}
	h.mapped = false
}
