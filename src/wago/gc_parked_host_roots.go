// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Each parked activation has separate journals for roots received from Wasm
// and roots staged for its return. The public state mutex protects both.
type parkedHostRoots struct {
	arguments parkedHostRootSlots
	results   parkedHostRootSlots
}

type parkedHostRootSlots struct {
	slots [coreruntime.MaxHostArity]uint32
	made  uint8
	count uint8
}

// Callers check the arity before adding a root and hold the collector lock.
func (r *parkedHostRootSlots) add(collector *gc.Collector, ref gc.Ref, class gc.RootClass) error {
	if r.count == r.made {
		slot, err := collector.NewCheckedClassifiedGlobalSlot(ref, class)
		if err != nil {
			return err
		}
		r.slots[r.count] = slot
		r.made++
	} else if err := collector.SetGlobalSlot(r.slots[r.count], ref); err != nil {
		return err
	}
	r.count++
	return nil
}

func (r *parkedHostRootSlots) clear(collector *gc.Collector) (uint8, error) {
	for i := uint8(0); i < r.count; i++ {
		if err := collector.SetGlobalSlot(r.slots[i], gc.Null()); err != nil {
			return i, err
		}
	}
	r.count = 0
	return 0, nil
}
