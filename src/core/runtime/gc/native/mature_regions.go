package gc

import (
	"errors"
	"slices"
)

// Descriptors partition the existing shared old/LOS address space, never its
// ownership. Physical (including class padding/tails) intersections are counted
// in every covered page. No descriptor address is exposed to generated code.
type matureRegion struct {
	live, reserved           uint32
	retire, admissionTouched bool
}

type matureHeap struct {
	pressure            bool
	poison              error
	cursor              uint32
	diagnostics         CollectionDiagnostics
	admission, required bool
	regions             []matureRegion
	touched             []uint32
	transaction         *evacuationTransaction
}

func (h *throughputHeap) beginMatureAdmission(required bool) {
	if h.mature == nil {
		return
	}
	if h.mature.admission {
		panic("gc: nested mature admission")
	}
	if len(h.mature.touched) != 0 {
		panic("gc: stale mature admission indexes")
	}
	h.mature.admission, h.mature.required = true, required
}

func (h *throughputHeap) matureRefusal(err error) {
	if h.mature == nil || err == nil {
		return
	}
	if h.mature.required {
		h.mature.pressure = true
	}
}

func (h *throughputHeap) commitMatureAdmission() {
	if h.mature == nil || !h.mature.admission {
		return
	}
	for _, i := range h.mature.touched {
		h.journalMatureRegion(i)
		r := &h.mature.regions[i]
		r.live += r.reserved
		r.reserved = 0
		r.admissionTouched = false
	}
	h.mature.touched = h.mature.touched[:0]
	h.mature.admission = false
}

func (h *throughputHeap) cancelMatureAdmission() {
	if h.mature == nil {
		return
	}
	omitted := false
	for _, i := range h.mature.touched {
		h.journalMatureRegion(i)
		r := &h.mature.regions[i]
		omitted = omitted || r.reserved != 0
		r.admissionTouched = false
	}
	h.mature.touched = h.mature.touched[:0]
	h.mature.admission = false
	if omitted {
		panic("gc: mature cancellation omitted destination rollback")
	}
}

func (h *throughputHeap) matureIntersections(e handleEntry, visit func(uint32, *matureRegion, uint32)) {
	end := uint64(e.off) + uint64(h.handleAllocSize(e))
	for off := uint64(e.off); off < end; {
		i := off / uint64(h.pageBytes)
		next := min(end, (i+1)*uint64(h.pageBytes))
		visit(uint32(i), &h.mature.regions[i], uint32(next-off))
		off = next
	}
}

func (h *throughputHeap) prepareMature(e handleEntry) error {
	end := uint64(e.off) + uint64(h.handleAllocSize(e))
	if end > uint64(h.limit) {
		return errThroughputHeapExhausted
	}
	count := (end + uint64(h.pageBytes) - 1) / uint64(h.pageBytes)
	first := uint64(e.off) / uint64(h.pageBytes)
	needRegions := count > uint64(len(h.mature.regions))
	newTouched := 0
	if h.mature.admission {
		for i := first; i < count; i++ {
			if i >= uint64(len(h.mature.regions)) || !h.mature.regions[i].admissionTouched {
				newTouched++
			}
		}
	}
	needTouched := newTouched > cap(h.mature.touched)-len(h.mature.touched)
	if needRegions || needTouched {
		if err := injectFailure(h, failMatureMetadata); err != nil {
			return err
		}
	}
	if needRegions {
		// Geometric growth stays bounded by ceil(limit/pageBytes), including
		// a partially configured final page. Default mode allocates nothing.
		maximum := (uint64(h.limit) + uint64(h.pageBytes) - 1) / uint64(h.pageBytes)
		count = min(maximum, max(count, uint64(len(h.mature.regions))*2))
		regions := make([]matureRegion, int(count))
		copy(regions, h.mature.regions)
		h.mature.regions = regions
	}
	if needTouched {
		h.mature.touched = slices.Grow(h.mature.touched, newTouched)
	}
	h.matureIntersections(e, func(i uint32, r *matureRegion, n uint32) {
		h.journalMatureRegion(i)
		if uint64(r.live)+uint64(r.reserved)+uint64(n) > uint64(h.pageBytes) {
			panic("gc: overlapping mature reservation")
		}
		if h.mature.admission {
			if !r.admissionTouched {
				r.admissionTouched = true
				h.mature.touched = append(h.mature.touched, i)
			}
			r.reserved += n
		} else {
			r.live += n
		}
	})
	return nil
}

// prepareMatureRun reserves the same per-object physical occupancy as repeated
// prepareMature calls, but admits metadata for the contiguous run once. The
// caller has not changed allocator state yet, so refusal needs no AVL rollback.
func (h *throughputHeap) prepareMatureRun(run throughputAllocRun, size uint32, count int, sp spaceKind) error {
	last := handleEntry{off: run.off + uint32(count-1)*run.allocSize, size: size, class: run.class, space: sp}
	if run.lastAllocSize != run.allocSize {
		last.class |= handleAbsorbedTailBit
	}
	end := uint64(last.off) + uint64(h.handleAllocSize(last))
	if end > uint64(h.limit) {
		return errThroughputHeapExhausted
	}

	// Reproduce the final geometric region length of sequential preparation.
	regionCount := uint64(len(h.mature.regions))
	metadataGrowths := 0
	coveredRegions := end / uint64(h.pageBytes)
	if end%uint64(h.pageBytes) != 0 {
		coveredRegions++
	}
	if coveredRegions > regionCount {
		maximum := (uint64(h.limit) + uint64(h.pageBytes) - 1) / uint64(h.pageBytes)
		for i := 0; i < count; i++ {
			e := handleEntry{off: run.off + uint32(i)*run.allocSize, size: size, class: run.class, space: sp}
			if i == count-1 && run.lastAllocSize != run.allocSize {
				e.class |= handleAbsorbedTailBit
			}
			need := (uint64(e.off) + uint64(h.handleAllocSize(e)) + uint64(h.pageBytes) - 1) / uint64(h.pageBytes)
			if need > regionCount {
				regionCount = min(maximum, max(need, regionCount*2))
				metadataGrowths++
			}
		}
	}
	firstPage := uint64(run.off) / uint64(h.pageBytes)
	lastPage := (end - 1) / uint64(h.pageBytes)
	newTouched := 0
	if h.mature.admission {
		for i := firstPage; i <= lastPage; i++ {
			if i >= uint64(len(h.mature.regions)) || !h.mature.regions[i].admissionTouched {
				newTouched++
			}
		}
	}
	needRegions := regionCount > uint64(len(h.mature.regions))
	needTouched := newTouched > cap(h.mature.touched)-len(h.mature.touched)
	if needRegions || needTouched {
		// Preserve the sequential path's observable injection opportunities at
		// each geometric descriptor growth, while performing one allocation.
		for range max(1, metadataGrowths) {
			if err := injectFailure(h, failMatureMetadata); err != nil {
				return err
			}
		}
	}
	if needRegions {
		regions := make([]matureRegion, int(regionCount))
		copy(regions, h.mature.regions)
		h.mature.regions = regions
	}
	if needTouched {
		h.mature.touched = slices.Grow(h.mature.touched, newTouched)
	}

	// The physical objects, including an absorbed tail, exactly partition this
	// interval. Validate every page before publishing any batch reservation.
	for page := firstPage; page <= lastPage; page++ {
		start := max(uint64(run.off), page*uint64(h.pageBytes))
		stop := min(end, (page+1)*uint64(h.pageBytes))
		r := &h.mature.regions[page]
		if uint64(r.live)+uint64(r.reserved)+stop-start > uint64(h.pageBytes) {
			panic("gc: overlapping mature reservation")
		}
	}
	for page := firstPage; page <= lastPage; page++ {
		start := max(uint64(run.off), page*uint64(h.pageBytes))
		stop := min(end, (page+1)*uint64(h.pageBytes))
		n := uint32(stop - start)
		h.journalMatureRegion(uint32(page))
		r := &h.mature.regions[page]
		if h.mature.admission {
			if !r.admissionTouched {
				r.admissionTouched = true
				h.mature.touched = append(h.mature.touched, uint32(page))
			}
			r.reserved += n
		} else {
			r.live += n
		}
	}
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
	h.matureIntersections(e, func(i uint32, r *matureRegion, n uint32) {
		h.journalMatureRegion(i)
		if h.mature.admission {
			if r.reserved < n {
				panic("gc: mature reserved underflow")
			}
			r.reserved -= n
		} else {
			if r.live < n {
				panic("gc: mature live underflow")
			}
			r.live -= n
			if r.live == 0 {
				r.retire = true
			}
		}
	})
}

func (h *throughputHeap) releaseMatureRun(run throughputAllocRun, size uint32, count int, sp spaceKind) {
	for i := count - 1; i >= 0; i-- {
		e := handleEntry{off: run.off + uint32(i)*run.allocSize, size: size, class: run.class, space: sp}
		if i == count-1 && run.lastAllocSize != run.allocSize {
			e.class |= handleAbsorbedTailBit
		}
		h.releaseMature(e)
	}
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
			off = next
		}
	}
	for i, r := range h.mature.regions {
		if r.live != want[i].live || r.reserved != 0 {
			return errors.New("gc: mature descriptor occupancy mismatch")
		}
	}
	return nil
}
