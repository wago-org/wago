package gc

import (
	"errors"
	"fmt"
	"math/bits"
	"slices"
)

const evacuationBlock = uint64(32768)
const evacuationLine = uint64(256)
const evacuationCandidateLimit = 65536
const evacuationRunLimit = 65536

// CollectionDiagnostics distinguishes a required collection failure from an
// optional evacuation refusal. Counts describe the last attempt, not timings.
type CollectionDiagnostics struct {
	Kind, Result, Evacuation                          string
	RequiredCause                                     error
	Pressure                                          bool
	Demand, Credit, Need, MovedBytes, MovedPhysical   uint64
	Candidates, Runs, ReservationWorkers, CopyWorkers int
}

func (c *Collector) CollectionDiagnostics() CollectionDiagnostics {
	if m := c.throughput.mature; m != nil {
		d := m.diagnostics
		d.Pressure = m.pressure
		return d
	}
	return CollectionDiagnostics{}
}
func (c *Collector) beginAttempt(kind string) {
	if m := c.throughput.mature; m != nil {
		m.diagnostics = CollectionDiagnostics{Kind: kind, Result: "running", Evacuation: "disabled", Pressure: m.pressure}
	}
}
func (c *Collector) endAttempt(err error) {
	if m := c.throughput.mature; m != nil {
		if err != nil {
			m.diagnostics.Result = "failed"
			if m.diagnostics.Evacuation == "moved" {
				m.diagnostics.Result = "committed-maintenance"
			}
			if m.diagnostics.RequiredCause == nil {
				m.diagnostics.RequiredCause = err
			}
			m.pressure = true
		} else {
			m.diagnostics.Result = "success"
		}
	}
}

// Root sources retain their existing panic contract, but unwinding is never
// recorded as a successful admitted attempt or allowed to consume pressure.
func (c *Collector) finishAttempt(result *error) {
	if r := recover(); r != nil {
		c.endAttempt(fmt.Errorf("gc: collection unwound: %v", r))
		panic(r)
	}
	c.endAttempt(*result)
}

type evacuationOverlay struct {
	lines   [2]uint64
	parent  int
	blocked bool
	count   int
}
type matureMove struct {
	handle              uint32
	before, destination handleEntry
}
type matureSelection struct {
	moves      []matureMove
	excluded   []bool
	demand     uint64
	largest    uint32
	considered bool
}

// Selection uses all physical starts before marking, including dead objects.
// Cross-block objects join components; one dense or LOS block excludes all of it.
func (c *Collector) selectMatureEvacuation() (matureSelection, error) {
	return c.selectMatureEvacuationBounded(evacuationCandidateLimit)
}
func (c *Collector) selectMatureEvacuationBounded(candidateLimit int) (matureSelection, error) {
	var out matureSelection
	if candidateLimit < 1 || candidateLimit > evacuationCandidateLimit {
		return out, errors.New("gc: invalid candidate limit")
	}
	m := c.throughput.mature
	if !c.cfg.MatureEvacuation || m == nil {
		return out, nil
	}
	m.diagnostics.Evacuation = "no-pressure"
	if !m.pressure {
		return out, nil
	}
	out.considered = true
	h := &c.throughput
	// Wago's old and LOS objects share a dynamically committed address range,
	// unlike the reference's fixed old-space block table. Prove the complete
	// active range before allocating its temporary overlay. Each source is
	// validated in the occupancy pass before indexing; scratch writes made
	// before a later invalid source are private and can be discarded. The
	// uncommitted configured tail is still usable by destination admission: an
	// excluded index beyond this overlay is deliberately treated as unselected.
	if h.bump > h.limit || uint64(h.bump) > uint64(len(h.mem)) {
		return out, errors.New("gc: invalid evacuation heap span")
	}
	activeBlocks := int((uint64(h.bump) + evacuationBlock - 1) / evacuationBlock)
	overlay := make([]evacuationOverlay, activeBlocks)
	for i := range overlay {
		overlay[i].parent = i
	}
	find := func(i int) int {
		for overlay[i].parent != i {
			i = overlay[i].parent
		}
		return i
	}
	for _, e := range c.handles {
		if e.space != spaceOld && e.space != spaceLarge {
			continue
		}
		n := h.handleAllocSize(e)
		end := uint64(e.off) + uint64(n)
		if n == 0 || end > uint64(h.bump) || end > uint64(len(h.mem)) || end > uint64(h.limit) {
			return out, errors.New("gc: invalid evacuation source extent")
		}
		first, last := int(uint64(e.off)/evacuationBlock), int((end-1)/evacuationBlock)
		for b := first; b <= last; b++ {
			lo := max(uint64(e.off), uint64(b)*evacuationBlock)
			hi := min(end, uint64(b+1)*evacuationBlock)
			for line := lo / evacuationLine; line <= (hi-1)/evacuationLine; line++ {
				k := line % 128
				overlay[b].lines[k/64] |= uint64(1) << (k % 64)
			}
			if e.space == spaceLarge || uint64(n) > evacuationBlock {
				overlay[b].blocked = true
			}
			if e.space == spaceOld && b > first {
				overlay[find(b)].parent = find(first)
			}
		}
		if e.space == spaceOld {
			overlay[first].count++
		}
	}
	// Aggregate sparse status and counts only after every union is known.
	counts := make([]int, len(overlay))
	bad := make([]bool, len(overlay))
	for i, b := range overlay {
		r := find(i)
		count := bits.OnesCount64(b.lines[0]) + bits.OnesCount64(b.lines[1])
		counts[r] += b.count
		bad[r] = bad[r] || b.blocked || count == 0 || count > 32
	}
	selected := make([]bool, len(overlay))
	count := 0
	// cursor is a logical rotating seed, not a configured-block ordinal. When
	// the active span shrinks its old value is reduced modulo the new span; this
	// can change the next selected/failed component relative to the former
	// configured-limit modulus, while preserving bounded rotation and safety.
	start := int(m.cursor) % max(1, len(overlay))
	budget := false
	for step := 0; step < len(overlay); step++ {
		i := (start + step) % len(overlay)
		if find(i) != i || bad[i] || counts[i] == 0 {
			continue
		}
		if counts[i] > candidateLimit-count {
			budget = true
			continue
		}
		selected[i] = true
		count += counts[i]
	}
	m.cursor = uint32((start + 1) % max(1, len(overlay)))
	out.excluded = make([]bool, len(overlay))
	for i := range overlay {
		out.excluded[i] = selected[find(i)]
	}
	out.moves = make([]matureMove, 0, count)
	for i, e := range c.handles {
		if e.space == spaceOld && out.excluded[uint64(e.off)/evacuationBlock] {
			n := h.handleAllocSize(e)
			out.demand += uint64(n)
			out.largest = max(out.largest, n)
			out.moves = append(out.moves, matureMove{handle: uint32(i), before: e})
		}
	}
	m.diagnostics.Candidates = len(out.moves)
	m.diagnostics.Demand = out.demand
	m.diagnostics.Evacuation = "no-candidates"
	if budget {
		m.diagnostics.Evacuation = "budget"
	}
	return out, nil
}

// planEvacuationRuns is pure: integer credit and exact source exclusion precede
// allocator mutation. Every run fits one quantum; final partial blocks never
// receive synthetic capacity. Configured reservation participants do not shrink
// when the active phase set shrinks.
func planEvacuationRuns(free []throughputFreeSpan, excluded []bool, demand uint64, largest uint32, workers, capRuns int) ([]throughputFreeSpan, uint64, uint64, error) {
	if demand == 0 {
		return nil, 0, 0, nil
	}
	if workers < 1 || workers > 8 || uint64(largest) > evacuationBlock || capRuns < 1 || capRuns > evacuationRunLimit || demand > ^uint64(0)-uint64(workers)*evacuationBlock {
		return nil, 0, 0, errors.New("gc: invalid run demand")
	}
	need := demand + uint64(workers)*evacuationBlock
	discount := uint64(max(uint32(16), largest))
	credit := uint64(0)
	runs := make([]throughputFreeSpan, 0, min(capRuns, 16))
	var previousEnd uint64
	for _, span := range free {
		end := uint64(span.off) + uint64(span.size)
		if span.off%16 != 0 || span.size%16 != 0 || end > uint64(^uint32(0)) || uint64(span.off) < previousEnd {
			return nil, credit, need, errors.New("gc: invalid free interval")
		}
		previousEnd = end
		for off := uint64(span.off); off < end; {
			b := off / evacuationBlock
			next := min(end, (b+1)*evacuationBlock)
			n := next - off
			if (b >= uint64(len(excluded)) || !excluded[b]) && n > discount {
				if len(runs) == capRuns {
					return nil, credit, need, errors.New("gc: run metadata limit")
				}
				if len(runs) == cap(runs) {
					grown := make([]throughputFreeSpan, len(runs), min(capRuns, max(16, cap(runs)*2)))
					copy(grown, runs)
					runs = grown
				}
				runs = append(runs, throughputFreeSpan{uint32(off), uint32(n)})
				credit += n - discount
				if credit >= need {
					return runs, credit, need, nil
				}
			}
			off = next
		}
	}
	return nil, credit, need, errThroughputHeapExhausted
}

// Exact reservations never absorb an unrepresented tail. The transaction
// journals only overwritten AVL nodes; slice headers restore any arena, debt,
// or backing growth.
func (h *throughputHeap) beginEvacuationTransaction() *evacuationTransaction {
	if h.mature.transaction != nil {
		panic("gc: nested evacuation transaction")
	}
	tx := &evacuationTransaction{heap: *h, mature: *h.mature, active: true}
	tx.mature.transaction = nil
	h.mature.transaction = tx
	return tx
}

func (h *throughputHeap) rollbackEvacuation(tx *evacuationTransaction) {
	if h.mature.transaction != tx || !tx.active {
		panic("gc: invalid evacuation rollback")
	}
	tx.active = false
	for i := len(tx.spanNodes) - 1; i >= 0; i-- {
		u := tx.spanNodes[i]
		*u.target = u.node
	}
	// Diagnostics and pressure describe this attempt, even after rollback.
	d, p, cur, poisoned := h.mature.diagnostics, h.mature.pressure, h.mature.cursor, h.mature.poison
	*h = tx.heap
	*h.mature = tx.mature
	h.mature.diagnostics, h.mature.pressure, h.mature.cursor = d, p, cur
	h.mature.poison = poisoned
}

func (h *throughputHeap) commitEvacuation(tx *evacuationTransaction) {
	if h.mature.transaction != tx || !tx.active {
		panic("gc: invalid evacuation commit")
	}
	tx.active = false
	h.mature.transaction = nil
}
func rawExtent(s throughputFreeSpan) handleEntry {
	return handleEntry{off: s.off, size: s.size, space: spaceOld, class: uint16(len(throughputClassSizes))}
}

// runClaimPlanner mirrors the collector's shared old-run claim discipline.
// Planning is serial, so nextRun needs no atomic operation; a run remains owned
// by the worker that claimed it until that worker exhausts it.
type runClaimPlanner struct {
	runs      []throughputFreeSpan
	workerRun []int
	nextRun   int
}

func newRunClaimPlanner(runs []throughputFreeSpan, workers int) runClaimPlanner {
	p := runClaimPlanner{
		runs:      runs,
		workerRun: make([]int, workers),
	}
	for i := range p.workerRun {
		p.workerRun[i] = -1
	}
	return p
}

func (p *runClaimPlanner) destination(worker int, size uint32) (uint32, bool) {
	r := p.workerRun[worker]
	if r < 0 || p.runs[r].size < size {
		if p.nextRun >= len(p.runs) {
			return 0, false
		}
		r = p.nextRun
		p.nextRun++
		if p.runs[r].size < size {
			return 0, false
		}
		p.workerRun[worker] = r
	}
	destination := p.runs[r].off
	p.runs[r].off += size
	p.runs[r].size -= size
	return destination, true
}

func (p *runClaimPlanner) releaseTails(h *throughputHeap) error {
	for _, run := range p.runs {
		if run.size != 0 {
			if err := h.free(rawExtent(run)); err != nil {
				return err
			}
		}
	}
	return nil
}
func (h *throughputHeap) reserveEvacuationRun(s throughputFreeSpan) error {
	end := uint64(s.off) + uint64(s.size)
	if end > uint64(h.limit) {
		return errThroughputHeapExhausted
	}
	if s.off >= h.bump {
		old := h.bump
		if err := h.growBackingIfNeeded(uint32(end)); err != nil {
			return err
		}
		h.bump = uint32(end)
		if s.off > old {
			if err := h.insertFreeSpan(throughputFreeSpan{old, s.off - old}); err != nil {
				return err
			}
		}
	} else {
		pred, _ := h.spanNeighbors(s.off + 1)
		if pred == throughputNoSlot {
			panic("gc: missing admitted run")
		}
		node := h.spanNodes[pred]
		if node.off > s.off || uint64(node.off)+uint64(node.size) < end {
			panic("gc: admitted run changed")
		}
		slot, ok := h.removeFreeSpan(node.off)
		if !ok {
			panic("gc: admitted run disappeared")
		}
		h.releaseSpanNode(slot)
		if node.off < s.off {
			if err := h.insertFreeSpan(throughputFreeSpan{node.off, s.off - node.off}); err != nil {
				return err
			}
		}
		if end < uint64(node.off)+uint64(node.size) {
			if err := h.insertFreeSpan(throughputFreeSpan{uint32(end), node.off + node.size - uint32(end)}); err != nil {
				return err
			}
		}
	}
	return nil
}
func (h *throughputHeap) growBackingIfNeeded(end uint32) error {
	if end <= uint32(len(h.mem)) {
		return nil
	}
	n, err := throughputReservationLen(uint64(end), h.pageBytes, h.limit)
	if err != nil {
		return err
	}
	return h.growBacking(n)
}

func (c *Collector) evacuateMature(s matureSelection) (err error) {
	if !s.considered {
		return nil
	}
	h := &c.throughput
	m := h.mature
	m.pressure = false
	if len(s.moves) == 0 {
		return nil
	}
	// Selection deliberately remains conservative because it runs before
	// marking: every object in a selected source block keeps that block out of
	// the destination set. Marking is complete here, so compact the existing
	// candidate storage in place and admit only the objects that will move.
	// Dead sources retain their original ownership for the ordinary sweep.
	live := s.moves[:0]
	s.demand, s.largest = 0, 0
	for _, move := range s.moves {
		if !c.mark[move.handle] {
			continue
		}
		n := h.handleAllocSize(move.before)
		s.demand += uint64(n)
		s.largest = max(s.largest, n)
		live = append(live, move)
	}
	s.moves = live
	m.diagnostics.Demand = s.demand
	if len(s.moves) == 0 {
		m.diagnostics.Evacuation = "no-live"
		return nil
	}
	m.diagnostics.ReservationWorkers = max(1, c.cfg.Workers)
	if err = h.sweepAllPending(); err != nil {
		m.diagnostics.Evacuation = "metadata"
		return nil
	}
	free := h.freeSpans()
	if h.bump < h.limit {
		free = append(free, throughputFreeSpan{h.bump, (h.limit - h.bump) &^ 15})
	}
	runs, credit, need, admit := planEvacuationRuns(free, s.excluded, s.demand, s.largest, max(1, c.cfg.Workers), evacuationRunLimit)
	m.diagnostics.Credit, m.diagnostics.Need, m.diagnostics.Runs = credit, need, len(runs)
	if admit != nil {
		m.diagnostics.Evacuation = "credit"
		return nil
	}
	transaction := h.beginEvacuationTransaction()
	committed := false
	defer func() {
		if r := recover(); r != nil {
			m.poison = fmt.Errorf("gc: destructive evacuation failure: %v", r)
			err = m.poison
		}
		if !committed {
			h.rollbackEvacuation(transaction)
		}
	}()
	h.beginMatureAdmission(false)
	for _, run := range runs {
		if err = h.reserveEvacuationRun(run); err != nil {
			m.diagnostics.Evacuation = "metadata"
			return nil
		}
	}
	// Deterministic per-participant streams share the serial run-claim cursor.
	workers := max(1, c.cfg.Workers)
	claims := newRunClaimPlanner(runs, workers)
	plans := make([]plannedPromotion, 0, len(s.moves))
	for i, move := range s.moves {
		n := h.handleAllocSize(move.before)
		w := i % workers
		off, ok := claims.destination(w, n)
		if !ok {
			m.diagnostics.Evacuation = "packing"
			return nil
		}
		dst := move.before
		dst.off = off
		// Keep the original class/tail representation: exact physical bytes were reserved.
		move.destination = dst
		s.moves[i] = move
		plans = append(plans, plannedPromotion{handle: move.handle, entry: dst})
	}
	if err = claims.releaseTails(h); err != nil {
		m.diagnostics.Evacuation = "metadata"
		return nil
	}
	for _, move := range s.moves {
		n := h.handleAllocSize(move.destination)
		clear(h.mem[move.destination.off+move.destination.size : move.destination.off+n])
	}
	// All source debt storage exists before the commit barrier.
	h.pendingFree = slices.Grow(h.pendingFree, len(s.moves))
	if err = injectFailure(c, failEvacuationCopy); err != nil {
		m.diagnostics.Evacuation = "copy-abort"
		return err
	}
	used := c.copyMatureParallel(plans)
	if err = c.parallelError(); err != nil {
		m.diagnostics.Evacuation = "copy-abort"
		return err
	}
	if !used {
		for _, move := range s.moves {
			copy(h.bytes(move.destination), h.bytes(move.before))
			if err = injectFailure(c, failEvacuationCopy); err != nil {
				m.diagnostics.Evacuation = "copy-abort"
				return err
			}
		}
	}
	if err = injectFailure(c, failEvacuationCommit); err != nil {
		m.diagnostics.Evacuation = "copy-abort"
		return err
	}
	// No fallible operation, callback, or allocator insertion beyond this barrier.
	committed = true
	h.commitEvacuation(transaction)
	h.commitMatureAdmission()
	for _, move := range s.moves {
		n := h.handleAllocSize(move.before)
		c.handles[move.handle] = move.destination
		h.pendingFree = append(h.pendingFree, throughputFreeSpan{move.before.off, n})
		h.pendingBytes += uint64(n)
		if c.cfg.PoisonFreed {
			for i := range h.bytes(move.before) {
				h.bytes(move.before)[i] = 0xdd
			}
		}
		m.diagnostics.MovedBytes += uint64(move.before.size)
		m.diagnostics.MovedPhysical += uint64(n)
	}
	c.lastCardBounds = objectCardBounds{}
	m.diagnostics.Evacuation = "moved"
	return nil
}

// promoteWithRuns is the required counterpart of optional mature admission.
// Oversized physical requests retain the independently checked exact-span path;
// no oversized object is justified using the bounded-quantum credit proof.
func (c *Collector) promotionRunEligible(plans []plannedPromotion) bool {
	if !c.cfg.MatureEvacuation {
		return false
	}
	for _, p := range plans {
		if p.entry.space == spaceFree && uint64(c.throughput.promotionAllocSize(c.handles[p.handle].size)) > evacuationBlock {
			return false
		}
	}
	return true
}
func (c *Collector) promoteWithRuns(plans []plannedPromotion, toSpace uint8, toBump uint32) (copied, promoted uint64, finished bool, err error) {
	h := &c.throughput
	m := h.mature
	defer func() {
		if err != nil {
			m.pressure = true
			clear(plans)
			c.promotionScratch = plans[:0]
		}
	}()
	if err = h.sweepAllPending(); err != nil {
		return
	}
	var demand uint64
	var largest uint32
	for _, p := range plans {
		if p.entry.space == spaceFree {
			n := h.promotionAllocSize(c.handles[p.handle].size)
			demand += uint64(n)
			largest = max(largest, n)
		}
	}
	workers := max(1, c.cfg.Workers)
	if demand < 65536 {
		workers = 1
	}
	free := h.freeSpans()
	if h.bump < h.limit {
		free = append(free, throughputFreeSpan{h.bump, (h.limit - h.bump) &^ 15})
	}
	runs, credit, need, err := planEvacuationRuns(free, nil, demand, largest, workers, evacuationRunLimit)
	m.diagnostics.Demand, m.diagnostics.Credit, m.diagnostics.Need = demand, credit, need
	m.diagnostics.Runs, m.diagnostics.ReservationWorkers = len(runs), workers
	if err != nil {
		return 0, 0, false, err
	}
	transaction := h.beginEvacuationTransaction()
	committed := false
	defer func() {
		if r := recover(); r != nil {
			m.poison = fmt.Errorf("gc: destructive run promotion failure: %v", r)
			err = m.poison
		}
		if !committed {
			h.rollbackEvacuation(transaction)
		}
	}()
	h.beginMatureAdmission(true)
	for _, r := range runs {
		if err = h.reserveEvacuationRun(r); err != nil {
			return
		}
	}
	claims := newRunClaimPlanner(runs, workers)
	index := 0
	for i := range plans {
		if plans[i].entry.space == spaceFree {
			src := c.handles[plans[i].handle]
			n := h.promotionAllocSize(src.size)
			w := index % workers
			off, ok := claims.destination(w, n)
			if !ok {
				err = errors.New("gc: promotion run packing refused")
				return
			}
			index++
			class := uint16(len(throughputClassSizes))
			if n <= h.classLimit {
				class = uint16(h.classFor(n))
			}
			plans[i].entry = handleEntry{off: off, size: src.size, space: spaceOld, class: class}
		}
		if err = injectFailure(c, failPromotionDestination); err != nil {
			return
		}
	}
	if err = claims.releaseTails(h); err != nil {
		return
	}
	for range plans {
		if err = injectFailure(c, failPromotionCommit); err != nil {
			return
		}
	}
	parallelCopied := c.copyPromotionParallel(plans)
	if err = c.parallelError(); err != nil {
		return
	}
	committed = true
	h.commitEvacuation(transaction)
	h.commitMatureAdmission()
	return c.commitPromotionPlans(plans, toSpace, toBump, parallelCopied)
}

// ParallelActivity records joined phases by their actual participant count.
// Index zero is one participant, index seven is eight. It has no timing data.
type ParallelActivity struct{ Trace, Sweep, Copy [8]uint64 }

func (c *Collector) ParallelActivity() ParallelActivity { return c.parallelActivity() }
