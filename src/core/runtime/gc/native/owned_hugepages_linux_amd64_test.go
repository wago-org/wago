// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && !tinygo && !wago_target_tinygo

package gc

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func ownedHugePageFlag(t *testing.T, address uintptr) bool {
	t.Helper()
	data, err := os.ReadFile("/proc/self/smaps")
	if err != nil {
		t.Fatal(err)
	}
	selected := false
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if span := strings.Split(fields[0], "-"); len(span) == 2 {
			lo, e1 := strconv.ParseUint(span[0], 16, 64)
			hi, e2 := strconv.ParseUint(span[1], 16, 64)
			selected = e1 == nil && e2 == nil && address >= uintptr(lo) && address < uintptr(hi)
		}
		if selected && fields[0] == "VmFlags:" {
			for _, flag := range fields[1:] {
				if flag == "hg" {
					return true
				}
			}
			return false
		}
	}
	t.Fatalf("mapping not found at %#x", address)
	return false
}

func TestOwnedHugePagesLogicalPrefix(t *testing.T) {
	// Check support on a separate mapping. This does not change host policy.
	probe, err := syscall.Mmap(-1, 0, 4096, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	err = syscall.Madvise(probe, syscall.MADV_HUGEPAGE)
	if releaseErr := syscall.Munmap(probe); releaseErr != nil {
		t.Fatal(releaseErr)
	}
	if err != nil {
		t.Skipf("MADV_HUGEPAGE unavailable: %v", err)
	}
	const huge = uintptr(2 << 20)
	for _, kind := range []string{"heap", "handles"} {
		t.Run(kind, func(t *testing.T) {
			var base uintptr
			var grow func(int) bool
			if kind == "heap" {
				h := &throughputHeap{limit: 16 << 20, ownedBacking: true}
				t.Cleanup(h.releaseOwnedBacking)
				grow = h.growOwnedBacking
				if !grow(65536) {
					t.Fatal("owned heap reservation failed")
				}
				base = uintptr(unsafe.Pointer(unsafe.SliceData(h.mem)))
			} else {
				c := &Collector{}
				c.throughput.ownedBacking = true
				t.Cleanup(c.releaseOwnedHandles)
				grow = func(bytes int) bool { return c.growOwnedHandles((bytes + NativeHandleStride - 1) / NativeHandleStride) }
				if !grow(65536) {
					t.Fatal("owned handle reservation failed")
				}
				base = uintptr(unsafe.Pointer(unsafe.SliceData(c.handles)))
			}
			if ownedHugePageFlag(t, base) {
				t.Fatal("small logical prefix was advised")
			}
			aligned := (base + huge - 1) &^ (huge - 1)
			exposed := int(aligned-base+huge) + 65536
			if !grow(exposed) {
				t.Fatal("owned growth failed")
			}
			if !ownedHugePageFlag(t, aligned) {
				t.Fatal("complete aligned logical range lacks huge-page advice")
			}
			if ownedHugePageFlag(t, aligned+huge) {
				t.Fatal("incomplete trailing range was advised")
			}
			if aligned > base && ownedHugePageFlag(t, base) {
				t.Fatal("incomplete leading range was advised")
			}
			if ownedHugePageFlag(t, base+(8<<20)) {
				t.Fatal("unexposed reservation was advised")
			}
		})
	}
}
