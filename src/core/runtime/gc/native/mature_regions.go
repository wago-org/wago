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
	e, err := h.allocRaw(size, sp)
	if err != nil {
		if !h.mature.admission {
			h.mature.pressure = true
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
