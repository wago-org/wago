// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

import "errors"

// matureHeap retains collection policy and attempt state only. Exact mature
// ownership belongs to the throughput allocator's bump, indexed free extents,
// pending debt, and handles; no per-page occupancy mirror is maintained.
type matureHeap struct {
	pressure            bool
	poison              error
	cursor              uint32
	diagnostics         CollectionDiagnostics
	occupancy           []matureOccupancyBlockState
	admission, required bool
	transaction         *evacuationTransaction
}

func (h *throughputHeap) beginMatureAdmission(required bool) {
	if h.mature == nil {
		return
	}
	if h.mature.admission {
		panic("gc: nested mature admission")
	}
	h.mature.admission, h.mature.required = true, required
}

func (h *throughputHeap) matureRefusal(err error) {
	if h.mature != nil && err != nil && h.mature.required {
		h.mature.pressure = true
	}
}

func (h *throughputHeap) commitMatureAdmission() {
	if h.mature != nil {
		h.mature.admission = false
	}
}

func (h *throughputHeap) cancelMatureAdmission() {
	if h.mature != nil {
		h.mature.admission = false
	}
}

func (h *throughputHeap) alloc(size uint32, sp spaceKind) (handleEntry, error) {
	if h.mature == nil {
		return h.allocRaw(size, sp)
	}
	// Reconcile before the local rollback checkpoint: trimming already-dead
	// trailing capacity is valid and must not be undone into an unindexed gap.
	if err := h.sweepAllPending(); err != nil {
		if !h.mature.admission {
			h.mature.pressure = true
		}
		h.matureRefusal(err)
		return handleEntry{}, err
	}
	initialBump := h.bump
	e, err := h.allocRaw(size, sp)
	if err != nil {
		if !h.mature.admission {
			h.mature.pressure = true
		}
		h.matureRefusal(err)
		return handleEntry{}, err
	}
	if err := h.claimOccupancy(e); err != nil {
		if e.off >= initialBump {
			h.bump = initialBump
			h.mature.occupancy = h.mature.occupancy[:occupancyBlockCount(initialBump)]
		} else if freeErr := h.freeRaw(e); freeErr != nil {
			panic("gc: cannot undo unclaimed mature allocation: " + freeErr.Error())
		}
		h.matureRefusal(err)
		return handleEntry{}, err
	}
	return e, nil
}

// Full-collection debt reconciliation is an explicit policy: after tracing has
// appended exact dead extents, reconcile all debt even when every page remains
// partially live. Failed insertion leaves the extent and pendingBytes retryable.
func (h *throughputHeap) reclaimEmptyMature() error {
	if h.mature == nil || len(h.pendingFree) == 0 {
		return nil
	}
	return h.sweepAllPending()
}

func (h *throughputHeap) verifyMature([]handleEntry) error {
	if h.mature != nil && h.mature.admission {
		return errors.New("gc: unpublished mature admission")
	}
	return nil
}
