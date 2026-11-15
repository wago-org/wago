// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && wagodebug && !tinygo && !wago_target_tinygo

package gc

import (
	"errors"
	"fmt"
	"testing"
	"unsafe"
)

func TestOwnedBackingGrowthFailureKeepsReservation(t *testing.T) {
	h := ownedTestHeap(t, 1<<20)
	if _, err := h.grow(4096); err != nil {
		t.Fatal(err)
	}
	base := unsafe.SliceData(h.mem)
	before := h.beginAllocTransaction()
	cancel := armFailure(h, failBackingGrowth, 0)
	_, err := h.grow(4096)
	cancel()
	if !errors.Is(err, errInjectedFailure) {
		t.Fatalf("error=%v, want backing-growth injection", err)
	}
	if !h.mapped || len(h.mem) != len(before.mem) || cap(h.mem) != cap(before.mem) || h.bump != before.bump || unsafe.SliceData(h.mem) != base {
		t.Fatal("failed logical growth changed reservation state")
	}
	if _, err = h.grow(4096); err != nil {
		t.Fatal(err)
	}
	if unsafe.SliceData(h.mem) != base {
		t.Fatal("retry moved the reserved heap")
	}
}

func TestOwnedBackingPublicationRollbackReleasesFirstReservation(t *testing.T) {
	c := newTestCollector(t, Config{LargeObjectBytes: 16, ThroughputHeapBytes: 1 << 20})
	c.UseOwnedThroughputBacking()
	before := snapshotPromotionState(c)
	cancel := armFailure(c, failHandlePublication, 0)
	_, err := c.NewStructDefault(0)
	cancel()
	if !errors.Is(err, errInjectedFailure) {
		t.Fatalf("error=%v, want publication injection", err)
	}
	assertPromotionStateEqual(t, c, before)
	if c.throughput.mapped || len(c.throughput.mem) != 0 {
		t.Fatal("publication rollback retained first mapped owner")
	}
	if _, err = c.NewStructDefault(0); err != nil {
		t.Fatal(err)
	}
	if !c.throughput.mapped {
		t.Fatal("retry did not acquire backing")
	}
}

func TestOwnedBackingPromotionFailureKeepsExistingReservation(t *testing.T) {
	for _, point := range []failurePoint{failPromotionPlan, failPromotionDestination, failPromotionCommit} {
		t.Run(fmt.Sprint(point), func(t *testing.T) {
			c := newTestCollector(t, Config{NurseryBytes: 16384, DisableMovingNursery: true, ThroughputHeapBytes: 16384, ThroughputPageBytes: 4096})
			c.UseOwnedThroughputBacking()
			if err := c.throughput.growBacking(4096); err != nil {
				t.Fatal(err)
			}
			c.refreshNativeView()
			base := unsafe.SliceData(c.throughput.mem)
			roots := make([]Root, 200)
			for i := range roots {
				ref, err := c.NewStructDefault(0)
				if err != nil {
					t.Fatal(err)
				}
				roots[i] = Root(ref)
			}
			before := snapshotPromotionState(c)
			cancel := armFailure(c, point, 1)
			err := c.CollectMinor(stressRootSlots(roots))
			cancel()
			if !errors.Is(err, errInjectedFailure) {
				t.Fatalf("collection error=%v, want injected promotion failure", err)
			}
			assertPromotionStateEqual(t, c, before)
			if !c.throughput.mapped || unsafe.SliceData(c.throughput.mem) != base || cap(c.throughput.mem) != 4096 {
				t.Fatal("failed promotion lost existing mapped ownership or logical extent")
			}
			if err = c.CollectMinor(stressRootSlots(roots)); err != nil {
				t.Fatalf("collection retry: %v", err)
			}
			if !c.throughput.mapped || unsafe.SliceData(c.throughput.mem) != base {
				t.Fatal("collection retry moved existing backing")
			}
		})
	}
}
