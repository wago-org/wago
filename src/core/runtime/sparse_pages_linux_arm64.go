// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && arm64

package runtime

import (
	"syscall"
	"unsafe"
)

const arm64PMDPageBytes = 2 << 20

func preferSmallLinearPages(mem []byte, initialBytes int) {
	// Keep the initially sparse prefix on base pages. A growing memory can
	// still use huge pages beyond this window.
	if initialBytes <= arm64PMDPageBytes && len(mem) >= arm64PMDPageBytes {
		preferBasePages(mem[:arm64PMDPageBytes])
	}
}

func preferSmallStackPages(mem []byte) {
	// Native stacks grow downward. A shallow invocation should not commit a
	// huge page at the top of a mostly untouched reservation.
	if len(mem) >= arm64PMDPageBytes {
		preferBasePages(mem[len(mem)-arm64PMDPageBytes:])
	}
}

func preferBasePages(mem []byte) {
	if syscall.Getpagesize() != 4096 {
		return
	}
	// Older kernels and policies that reject the advice can use the mapping
	// with their ordinary backing strategy.
	_, _, _ = syscall.Syscall(syscall.SYS_MADVISE, uintptr(unsafe.Pointer(unsafe.SliceData(mem))),
		uintptr(len(mem)), syscall.MADV_NOHUGEPAGE)
}

// Reuse normally preserves a reservation's previous advice. At the boundary
// size, always use the same policy as a new reservation.
func prepareReusedLinearPages(mem []byte, initialBytes int) {
	if initialBytes == arm64PMDPageBytes && len(mem) > arm64PMDPageBytes {
		preferBasePages(mem[:arm64PMDPageBytes])
	}
}

// reclaimSparseLinearPages retains at most 64 KiB of working pages. The
// caller owns the entire rounded mapping and has stopped guest execution.
func reclaimSparseLinearPages(mem []byte, logicalBytes int) bool {
	const page, maxResident = 4096, 16
	const pages = arm64PMDPageBytes/page + 1
	if logicalBytes != arm64PMDPageBytes || syscall.Getpagesize() != page || len(mem) != pages*page {
		return false
	}
	var present [pages]byte
	_, _, errno := syscall.Syscall(syscall.SYS_MINCORE,
		uintptr(unsafe.Pointer(unsafe.SliceData(mem))), uintptr(len(mem)),
		uintptr(unsafe.Pointer(&present[0])))
	if errno != 0 {
		return false
	}
	count := 0
	for _, status := range present {
		count += int(status & 1)
	}
	if count == 0 || count > maxResident {
		return false
	}
	for start := 0; start < pages; {
		resident := present[start]&1 != 0
		end := start + 1
		for end < pages && (present[end]&1 != 0) == resident {
			end++
		}
		part := mem[start*page : end*page]
		if resident {
			clear(part)
		} else if err := madviseDontNeed(part); err != nil {
			// A nonresident page can hold swapped data. Preserve zero-on-reuse
			// via the ordinary whole-range reclaim if a gap could not be reset.
			return false
		}
		start = end
	}
	return true
}
