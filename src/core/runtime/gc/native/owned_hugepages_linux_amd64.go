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

// Advise only newly completed, aligned 2MiB ranges of an owned mapping's
// logical prefix. Small owners and unexposed reservation pages keep their
// original policy. The hint does not touch pages and may be declined by Linux.
func adviseOwnedHugePages(mem []byte, previous int) {
	const mask = uintptr((2 << 20) - 1)
	base := uintptr(unsafe.Pointer(unsafe.SliceData(mem)))
	start := (base + uintptr(previous)) &^ mask
	first := (base + mask) &^ mask
	if start < first {
		start = first
	}
	end := (base + uintptr(len(mem))) &^ mask
	if end > start {
		_ = syscall.Madvise(mem[start-base:end-base], syscall.MADV_HUGEPAGE)
	}
}
