// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// gcArgumentRoots keeps incoming references live until the native invocation
// takes ownership. It reuses the same collector slots across calls.
type gcArgumentRoots struct {
	count uint32
	made  uint32
	slots [gcPublicSlotLimit]uint32
	extra []uint32
}

func (r *gcArgumentRoots) slot(index uint32) uint32 {
	if index < gcPublicSlotLimit {
		return r.slots[index]
	}
	return r.extra[index-gcPublicSlotLimit]
}

func (r *gcArgumentRoots) stage(collector *gc.Collector, ref gc.Ref) error {
	if r.count == r.made {
		slot, err := collector.NewCheckedClassifiedGlobalSlot(ref, gc.RootForeignInstance)
		if err != nil {
			return err
		}
		if r.made < gcPublicSlotLimit {
			r.slots[r.made] = slot
		} else {
			r.extra = append(r.extra, slot)
		}
		r.made++
	} else if err := collector.SetGlobalSlot(r.slot(r.count), ref); err != nil {
		return err
	}
	r.count++
	return nil
}

func (r *gcArgumentRoots) clear(collector *gc.Collector) {
	for i := uint32(0); i < r.count; i++ {
		if err := collector.SetGlobalSlot(r.slot(i), gc.Null()); err != nil {
			panic(gcHelperFailuref("clear GC reference argument root %d: %w", i, err))
		}
	}
	r.count = 0
}

// gcCloneRoot holds the reconstructed graph through public token publication.
type gcCloneRoot struct {
	slot uint32
	made bool
}

func (r *gcCloneRoot) stage(collector *gc.Collector, ref gc.Ref) error {
	if r.made {
		return collector.SetGlobalSlot(r.slot, ref)
	}
	slot, err := collector.NewCheckedClassifiedGlobalSlot(ref, gc.RootForeignInstance)
	if err != nil {
		return err
	}
	r.slot = slot
	r.made = true
	return nil
}

func (r *gcCloneRoot) clear(collector *gc.Collector) {
	if r.made {
		_ = collector.SetGlobalSlot(r.slot, gc.Null())
	}
}
