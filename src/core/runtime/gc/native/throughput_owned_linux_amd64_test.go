// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && !tinygo && !wago_target_tinygo

package gc

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func ownedTestHeap(t *testing.T, limit uint32) *throughputHeap {
	t.Helper()
	h := new(throughputHeap)
	if err := h.Init(Config{ThroughputHeapBytes: limit, ThroughputPageBytes: 4096, ThroughputClassLimit: 32768}); err != nil {
		t.Fatal(err)
	}
	h.ownedBacking = true
	t.Cleanup(h.Close)
	return h
}

// This mechanism witness also compiles against the previous source. That source
// has no opt-in and copies the backing at each growth instead of retaining its base.
func TestOwnedThroughputBackingPreservesGrowth(t *testing.T) {
	c, err := NewCollector(Config{NurseryBytes: 4096, ThroughputHeapBytes: 2 << 20}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if owner, ok := any(c).(interface{ UseOwnedThroughputBacking() }); ok {
		owner.UseOwnedThroughputBacking()
	}
	h := &c.throughput
	if _, err = h.grow(512 << 10); err != nil {
		t.Fatal(err)
	}
	base := unsafe.SliceData(h.mem)
	h.mem[0] = 37
	if _, err = h.grow(16); err != nil {
		t.Fatal(err)
	}
	if len(h.mem) != 768<<10 || cap(h.mem) != 768<<10 {
		t.Fatalf("logical extent=%d/%d, want 768KiB", len(h.mem), cap(h.mem))
	}
	if unsafe.SliceData(h.mem) != base {
		t.Fatal("heap growth copied the backing to a new address")
	}
	if h.mem[0] != 37 {
		t.Fatal("growth changed existing payload")
	}
	before := h.bump
	if _, ok, err := h.tryAllocRun(32768, 16, spaceLarge); err != nil || ok || h.bump != before {
		t.Fatalf("run exceeded logical bounds: accepted=%v error=%v bump=%d/%d", ok, err, h.bump, before)
	}
}

func TestOwnedThroughputBackingRollbackAndRelease(t *testing.T) {
	t.Run("first reservation", func(t *testing.T) {
		h := ownedTestHeap(t, 1<<20)
		tx := h.beginAllocTransaction()
		if _, err := h.grow(4096); err != nil {
			t.Fatal(err)
		}
		if !h.mapped {
			t.Fatal("owned heap did not map storage")
		}
		full := h.mappedBacking()
		h.restoreAllocTransaction(tx)
		if h.mapped || len(h.mem) != 0 || h.bump != 0 {
			t.Fatal("rollback kept a first reservation")
		}
		if err := syscall.Munmap(full); !errors.Is(err, syscall.EINVAL) {
			t.Fatalf("first reservation not released: %v", err)
		}
		if _, err := h.grow(4096); err != nil {
			t.Fatal(err)
		}
		if !h.mapped {
			t.Fatal("retry did not reserve storage")
		}
		full = h.mappedBacking()
		h.Close()
		h.Close()
		if h.mapped || len(h.mem) != 0 || h.ownedBacking {
			t.Fatal("Close retained owner state")
		}
		if err := syscall.Munmap(full); !errors.Is(err, syscall.EINVAL) {
			t.Fatalf("Close did not release complete owner: %v", err)
		}
	})
	t.Run("existing reservation", func(t *testing.T) {
		h := ownedTestHeap(t, 1<<20)
		if _, err := h.grow(4096); err != nil {
			t.Fatal(err)
		}
		h.mem[0] = 77
		base := unsafe.SliceData(h.mem)
		tx := h.beginAllocTransaction()
		if _, err := h.grow(4096); err != nil {
			t.Fatal(err)
		}
		for i := len(tx.mem); i < len(h.mem); i++ {
			h.mem[i] = 0xcc
		}
		h.restoreAllocTransaction(tx)
		if !h.mapped || len(h.mem) != 4096 || cap(h.mem) != 4096 || h.bump != 4096 {
			t.Fatal("rollback changed logical state or owner")
		}
		if _, err := h.grow(4096); err != nil {
			t.Fatal(err)
		}
		if unsafe.SliceData(h.mem) != base || h.mem[0] != 77 {
			t.Fatal("rollback moved or changed existing storage")
		}
		for i, b := range h.mem[4096:] {
			if b != 0 {
				t.Fatalf("regrown suffix dirty at %d", i)
			}
		}
	})
}

func TestOwnedThroughputBackingNonPageLimit(t *testing.T) {
	h := ownedTestHeap(t, 4112)
	for _, size := range []uint32{4096, 16} {
		if _, err := h.grow(size); err != nil {
			t.Fatal(err)
		}
	}
	if !h.mapped || len(h.mem) != 4112 || cap(h.mem) != 4112 || uintptr(unsafe.Pointer(unsafe.SliceData(h.mem)))%16 != 0 {
		t.Fatal("non-page reservation extent or alignment changed")
	}
	if _, err := h.grow(16); !errors.Is(err, errThroughputHeapExhausted) {
		t.Fatalf("exhaustion error=%v", err)
	}
	if h.bump != 4112 {
		t.Fatal("failed allocation changed bump")
	}
}

func TestOwnedThroughputBackingDefaultStaysGoOwned(t *testing.T) {
	c, err := NewCollector(Config{NurseryBytes: 4096, ThroughputHeapBytes: 1 << 20}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.throughput.grow(4096); err != nil {
		t.Fatal(err)
	}
	if c.throughput.mapped || c.throughput.ownedBacking {
		t.Fatal("standalone collector acquired explicit ownership")
	}
}

func TestOwnedThroughputBackingReservationFailure(t *testing.T) {
	const marker = "WAGO_TEST_RESERVATION_FAILURE"
	if os.Getenv(marker) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestOwnedThroughputBackingReservationFailure$", "-test.v")
		cmd.Env = append(os.Environ(), marker+"=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("reservation fallback subprocess: %v\n%s", err, out)
		}
		return
	}
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		t.Fatal(err)
	}
	pages, err := strconv.ParseUint(strings.Fields(string(data))[0], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	var limit syscall.Rlimit
	if err = syscall.Getrlimit(syscall.RLIMIT_AS, &limit); err != nil {
		t.Fatal(err)
	}
	limit.Cur = pages*uint64(os.Getpagesize()) + (32 << 20)
	if limit.Cur > limit.Max {
		t.Fatalf("existing address-space hard limit %d", limit.Max)
	}
	if err = syscall.Setrlimit(syscall.RLIMIT_AS, &limit); err != nil {
		t.Fatal(err)
	}
	h := ownedTestHeap(t, 1<<30)
	if _, err = h.grow(32); err != nil {
		t.Fatal(err)
	}
	if h.mapped || h.ownedBacking || len(h.mem) != 4096 || h.bump != 32 {
		t.Fatal("failed full reservation did not use the small Go fallback")
	}
}
