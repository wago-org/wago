package gc

import "errors"

// Descriptors partition the existing shared old/LOS address space, never its
// ownership. Physical (including class padding/tails) intersections are counted
// in every covered page. No descriptor address is exposed to generated code.
type matureRegion struct {
	live, large, reserved, reservedLarge uint32
	epoch                                uint64
	retire                               bool
}

type matureHeap struct {
	pressure            bool
	poison              error
	cursor              uint32
	diagnostics         CollectionDiagnostics
	admission, required bool
	regions             []matureRegion
	// First cause belongs to this attempt. Optional refusal cannot latch a
	// required failure; a new attempt clears the previous outcome.
	cause   error
	refused bool
	retired uint64
}

func (h *throughputHeap) beginMatureAdmission(required bool) {
	if h.mature == nil {
		return
	}
	if h.mature.admission {
		panic("gc: nested mature admission")
	}
	h.mature.admission, h.mature.required = true, required
	h.mature.cause, h.mature.refused = nil, false
}

func (h *throughputHeap) matureRefusal(err error) {
	if h.mature == nil || err == nil {
		return
	}
	if h.mature.cause == nil {
		h.mature.cause = err
	}
	if h.mature.required {
		h.mature.refused = true
		h.mature.pressure = true
	}
}

func (h *throughputHeap) commitMatureAdmission() {
	if h.mature == nil || !h.mature.admission {
		return
	}
	for i := range h.mature.regions {
		r := &h.mature.regions[i]
		r.live += r.reserved
		r.large += r.reservedLarge
		r.reserved, r.reservedLarge = 0, 0
	}
	h.mature.admission = false
}

func (h *throughputHeap) cancelMatureAdmission() {
	if h.mature == nil {
		return
	}
	for _, r := range h.mature.regions {
		if r.reserved != 0 {
			panic("gc: mature cancellation omitted destination rollback")
		}
	}
	h.mature.admission = false
}

func (h *throughputHeap) matureIntersections(e handleEntry, visit func(*matureRegion, uint32)) {
	end := uint64(e.off) + uint64(h.handleAllocSize(e))
	for off := uint64(e.off); off < end; {
		i := off / uint64(h.pageBytes)
		next := min(end, (i+1)*uint64(h.pageBytes))
		visit(&h.mature.regions[i], uint32(next-off))
		off = next
	}
}

func (h *throughputHeap) prepareMature(e handleEntry) error {
	end := uint64(e.off) + uint64(h.handleAllocSize(e))
	if end > uint64(h.limit) {
		return errThroughputHeapExhausted
	}
	count := (end + uint64(h.pageBytes) - 1) / uint64(h.pageBytes)
	if count > uint64(len(h.mature.regions)) {
		if err := injectFailure(h, failMatureMetadata); err != nil {
			return err
		}
		// Geometric growth stays bounded by ceil(limit/pageBytes), including
		// a partially configured final page. Default mode allocates nothing.
		maximum := (uint64(h.limit) + uint64(h.pageBytes) - 1) / uint64(h.pageBytes)
		count = min(maximum, max(count, uint64(len(h.mature.regions))*2))
		regions := make([]matureRegion, int(count))
		copy(regions, h.mature.regions)
		h.mature.regions = regions
	}
	h.matureIntersections(e, func(r *matureRegion, n uint32) {
		if uint64(r.live)+uint64(r.reserved)+uint64(n) > uint64(h.pageBytes) {
			panic("gc: overlapping mature reservation")
		}
		if h.mature.admission {
			r.reserved += n
			if e.space == spaceLarge {
				r.reservedLarge += n
			}
		} else {
			r.live += n
			if e.space == spaceLarge {
				r.large += n
			}
		}
	})
	return nil
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
	mem, bump := h.mem, h.bump
	e, err := h.allocRaw(size, sp)
	if err == nil {
		err = h.prepareMature(e)
		if err != nil {
			if e.off < bump {
				if undo := h.insertFreeSpan(throughputFreeSpan{e.off, h.handleAllocSize(e)}); undo != nil {
					panic("gc: mature preparation rollback: " + undo.Error())
				}
			}
			h.mem, h.bump = mem, bump
		}
	}
	if err != nil {
		if !h.mature.admission {
			h.mature.pressure = true
		}
		h.matureRefusal(err)
		return handleEntry{}, err
	}
	return e, nil
}

func (h *throughputHeap) releaseMature(e handleEntry) {
	if h.mature == nil {
		return
	}
	h.matureIntersections(e, func(r *matureRegion, n uint32) {
		if h.mature.admission {
			if r.reserved < n {
				panic("gc: mature reserved underflow")
			}
			r.reserved -= n
			if e.space == spaceLarge {
				r.reservedLarge -= n
			}
		} else {
			if r.live < n {
				panic("gc: mature live underflow")
			}
			r.live -= n
			if e.space == spaceLarge {
				r.large -= n
			}
			if r.live == 0 {
				r.retire = true
			}
		}
	})
}

// Full tracing has removed dead handles and left exact physical spans in the
// shared ledger. Empty descriptors trigger reconciliation (and tail trimming),
// rather than returning page extents a second time. Mixed pages keep live spans;
// crossing LOS extents are never split or relocated. Failed reconciliation
// leaves candidates and authoritative pending debt retryable.
func (h *throughputHeap) reclaimEmptyMature() error {
	if h.mature == nil {
		return nil
	}
	candidate := false
	for _, r := range h.mature.regions {
		candidate = candidate || (r.retire && r.live == 0 && r.reserved == 0)
	}
	if !candidate {
		return nil
	}
	if err := h.sweepAllPending(); err != nil {
		return err
	}
	for i := range h.mature.regions {
		r := &h.mature.regions[i]
		if r.retire && r.live == 0 && r.reserved == 0 {
			r.retire = false
			r.epoch++
			h.mature.retired++
		}
	}
	return nil
}

func (h *throughputHeap) verifyMature(handles []handleEntry) error {
	if h.mature == nil {
		return nil
	}
	if h.mature.admission {
		return errors.New("gc: unpublished mature admission")
	}
	want := make([]matureRegion, len(h.mature.regions))
	for _, e := range handles {
		if e.space != spaceOld && e.space != spaceLarge {
			continue
		}
		end := uint64(e.off) + uint64(h.handleAllocSize(e))
		if end > uint64(len(want))*uint64(h.pageBytes) {
			return errors.New("gc: missing mature descriptor")
		}
		for off := uint64(e.off); off < end; {
			i := off / uint64(h.pageBytes)
			next := min(end, (i+1)*uint64(h.pageBytes))
			want[i].live += uint32(next - off)
			if e.space == spaceLarge {
				want[i].large += uint32(next - off)
			}
			off = next
		}
	}
	for i, r := range h.mature.regions {
		if r.live != want[i].live || r.large != want[i].large || r.reserved != 0 || r.reservedLarge != 0 {
			return errors.New("gc: mature descriptor occupancy mismatch")
		}
	}
	return nil
}
