//go:build !tinygo && !wago_target_tinygo

package gc

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"runtime"
	"sync"
	"sync/atomic"
)

const parallelCollectionSupported = true
const parallelMinObjects = 8192
const parallelArrayChunk = 1024
const parallelQueueLimit = 4096
const parallelPrivateLimit = 24

const (
	parallelTrace uint8 = iota
	parallelSweep
	parallelCopy
)

// Tasks with end == 0 scan an object; other tasks scan disjoint array ranges.
type traceTask struct{ handle, first, end uint32 }

type traceWorker struct {
	pool      *parallelCollector
	wake      chan *parallelPhase
	local     [parallelPrivateLimit]traceTask
	localLen  int
	live      []uint32
	scanned   uint64
	tasks     uint64
	index     int
	highWater uint32
	pendingAt uint32
}

// The mutator is stopped by the caller. Helpers only read heap/descriptor data
// and uniquely write claimed mark entries or admitted payload destinations.
// Root callbacks, handle publication, allocator updates, and card maintenance
// remain on the caller after the join barrier.
// A fresh allocation is the completion identity. Numeric sequence is diagnostic
// only; neither notification nor join compares truncated counters.
type parallelPhase struct {
	sequence uint64
	done     [8]bool
}

type parallelCollector struct {
	lastTraceWork uint64
	activity      ParallelActivity
	participants  int
	sequence      uint64
	current       *parallelPhase
	failed        atomic.Bool
	err           error // protected by mu until join
	poison        error // sticky for unexpected panics; close is the only teardown
	// Tests install hooks while the collector is stopped. Hooks must not mutate heap.
	hook             func(phase uint8, worker int)
	taskHook         func(phase uint8, worker int)
	collector        *Collector
	workers          []traceWorker
	joined           sync.WaitGroup
	stopped          sync.WaitGroup
	mu               sync.Mutex
	ready            *sync.Cond
	queue            []traceTask
	pending          []uint64 // one fixed bit per admitted handle; object tasks only
	overflowTasks    atomic.Int64
	idle             atomic.Int32
	claims           []uint32     // per-handle epoch; separate from the serial mark representation
	outstanding      atomic.Int64 // published tasks not yet completely scanned
	epoch            uint32
	nursery, capture bool
	phase            uint8
	plans            []plannedPromotion
}

func (c *Collector) drainParallel(capture, nursery bool) (uint64, bool) {
	if c.cfg.Workers <= 1 || runtime.GOMAXPROCS(0) <= 1 || len(c.markStack) == 0 || c.objectEdgeSource() != nil {
		return 0, false
	}
	objects := len(c.handles)
	if nursery {
		objects = len(c.nurseryHandles)
	}
	if objects < parallelMinObjects && !c.forceParallelPhase() {
		// A single large reference array can dominate tracing despite a tiny
		// handle table. Inspect only the already-admitted seeds in this case.
		largeArray := false
		for _, h := range c.markStack {
			r := makeObjRef(h)
			d, err := c.refDesc(r)
			if err == nil && d.ArrayElementsAreRefs() && c.header(r).Aux >= parallelMinObjects {
				largeArray = true
				break
			}
		}
		if !largeArray {
			return 0, false
		}
	}
	p := c.parallelPool()
	if len(p.claims) < len(c.handles) {
		p.claims = make([]uint32, len(c.handles))
	}
	words := (len(c.handles) + 63) / 64
	if len(p.pending) < words {
		p.pending = make([]uint64, words)
	} else {
		clear(p.pending)
	}
	p.overflowTasks.Store(0)
	p.epoch++
	if p.epoch == 0 {
		clear(p.claims)
		p.epoch = 1
	}
	p.nursery, p.capture = nursery, capture
	p.phase = parallelTrace
	p.selectParticipants(objects)
	for i := range p.workers {
		w := &p.workers[i]
		w.localLen = 0
		w.live = w.live[:0]
		w.scanned = 0
		w.tasks = 0
		w.highWater = 0
		w.pendingAt = 0
	}
	// Seeds were marked by the serial root/remembered scan. Claim all of them
	// before dispatch, so an edge back to a seed cannot scan it a second time.
	for i, h := range c.markStack {
		p.claims[h] = p.epoch
		c.markHighWater = max(c.markHighWater, h+1)
		p.workers[i%p.participants].enqueue(traceTask{handle: h})
	}
	c.markStack = c.markStack[:0]
	p.run()
	var scanned uint64
	for i := range p.workers {
		scanned += p.workers[i].scanned
		c.markHighWater = max(c.markHighWater, p.workers[i].highWater)
	}
	if capture {
		c.promotionScratch = c.promotionScratch[:0]
		// CollectMinor uses the nursery ledger when every young object survives.
		// Do not materialize a captured list that the planner will discard. Sparse
		// plans deliberately use nursery-ledger order, not worker discovery order:
		// stable handles preserve logical identity, but physical placement and the
		// object at an injected failure ordinal are not promised to be identical.
		if scanned < uint64(len(c.nurseryHandles)) {
			live := 0
			for _, h := range c.nurseryHandles {
				if c.mark[h] {
					live++
				}
			}
			if cap(c.promotionScratch) < live {
				c.promotionScratch = make([]plannedPromotion, 0, live)
			}
			for _, h := range c.nurseryHandles {
				if c.mark[h] {
					c.promotionScratch = append(c.promotionScratch, plannedPromotion{handle: h})
				}
			}
		}
	}
	if nursery {
		c.stats.MinorObjectsScanned += scanned
	}
	return scanned, true
}

func (c *Collector) parallelPool() *parallelCollector {
	p := c.parallel
	if p == nil {
		p = &parallelCollector{collector: c, queue: make([]traceTask, 0, parallelQueueLimit)}
		p.ready = sync.NewCond(&p.mu)
		p.workers = make([]traceWorker, min(c.cfg.Workers, runtime.GOMAXPROCS(0)))
		for i := range p.workers {
			w := &p.workers[i]
			w.pool = p
			w.index = i
			if i != 0 {
				w.wake = make(chan *parallelPhase)
				p.stopped.Add(1)
				go w.loop()
			}
		}
		c.parallel = p
	}
	return p
}

// Participant policy is independent of exact destination reservation: all spans
// are reserved by the coordinator before copy dispatch, even when only two run.
func (p *parallelCollector) selectParticipants(work int) {
	p.participants = min(len(p.workers), max(2, 1+(work-1)/parallelMinObjects))
	c := p.collector
	if !c.cfg.MatureEvacuation {
		return
	}
	// Source byte quanta are policy only. Reservation workers and run credit
	// remain fixed independently, including when history selects owner-only.
	available := len(p.workers)
	if p.hook != nil || p.taskHook != nil {
		p.participants = available
		return
	}
	m := c.throughput.mature
	if p.phase == parallelSweep {
		bytes := uint64(c.throughput.bump)
		p.participants = 1
		if bytes > 65536 {
			p.participants = min(available, 1+int(bytes/2097152))
		}
		return
	}
	if m.diagnostics.Kind == "major" && m.diagnostics.Pressure {
		p.participants = available
		return
	}
	var young, old uint64
	for _, e := range c.handles {
		if e.space == spaceFree {
			continue
		}
		if e.young() {
			young += uint64(e.size)
		} else {
			old += uint64(e.size)
		}
	}
	if m.diagnostics.Kind == "minor" && young < 65536 {
		p.participants = 1
		return
	}
	estimate := min(young, p.lastTraceWork)
	if m.diagnostics.Kind == "major" {
		estimate += old
	}
	p.participants = min(available, 1+int(estimate/262144))
	if m.diagnostics.Kind == "major" {
		p.participants = max(p.participants, min(available, 2))
	}
}

func (p *parallelCollector) fail(err error, poison bool) {
	p.mu.Lock()
	if p.err == nil {
		p.err = err
	}
	if poison && p.poison == nil {
		p.poison = err
	}
	p.failed.Store(true)
	p.ready.Broadcast()
	p.mu.Unlock()
}

type parallelInjectedFailure struct{}

func (p *parallelCollector) run() {
	if p.poison != nil {
		p.err = p.poison
		return
	}
	if p.participants == 0 {
		p.participants = len(p.workers)
	}
	p.sequence++ // wrap is harmless: fresh pointer identity, channel delivery, join
	phase := &parallelPhase{sequence: p.sequence}
	p.current = phase
	p.joined.Add(p.participants - 1)
	for i := 1; i < p.participants; i++ {
		p.workers[i].wake <- phase
	}
	p.workers[0].execute(phase)
	p.joined.Wait()
	// No phase storage, claims, plans, or queue can be reused before every peer
	// returns, including delayed peers after cancellation or contained panic.
	for i := 0; i < p.participants; i++ {
		if !phase.done[i] {
			p.fail(fmt.Errorf("gc: missing helper completion"), true)
		}
	}
	if p.phase == parallelTrace {
		p.lastTraceWork = 0
		for i, e := range p.collector.handles {
			if i < len(p.collector.mark) && p.collector.mark[i] {
				p.lastTraceWork += uint64(e.size)
			}
		}
	}
	switch p.phase {
	case parallelTrace:
		p.activity.Trace[p.participants-1]++
	case parallelSweep:
		p.activity.Sweep[p.participants-1]++
	case parallelCopy:
		p.activity.Copy[p.participants-1]++
	}
	p.queue = p.queue[:0]
	clear(p.pending)
	p.overflowTasks.Store(0)
	p.outstanding.Store(0)
	for i := range p.workers {
		p.workers[i].localLen = 0
	}
}

func (w *traceWorker) execute(phase *parallelPhase) {
	p := w.pool
	completed := false
	defer func() {
		failure := recover()
		if failure != nil {
			_, injected := failure.(parallelInjectedFailure)
			p.fail(fmt.Errorf("gc: helper phase %d worker %d failed: %v", p.phase, w.index, failure), !injected)
		}
		if !completed && failure == nil {
			p.fail(fmt.Errorf("gc: helper exited without completion"), true)
		}
		phase.done[w.index] = true // unique writer; WaitGroup joins before inspection
	}()
	if phase != p.current || phase.done[w.index] {
		p.fail(fmt.Errorf("gc: stale helper dispatch"), true)
		return
	}
	if p.hook != nil {
		p.hook(p.phase, w.index)
	}
	if !p.failed.Load() {
		w.runPhase()
	}
	completed = true
}

func (c *Collector) parallelError() error {
	if c.parallel != nil {
		return c.parallel.err
	}
	return nil
}
func (c *Collector) beginParallelCollection() error {
	if p := c.parallel; p != nil {
		if p.poison != nil {
			return p.poison
		}
		p.err = nil
		p.failed.Store(false)
	}
	return nil
}

func (w *traceWorker) runPhase() {
	switch w.pool.phase {
	case parallelSweep:
		w.sweep()
	case parallelCopy:
		w.copyPromotions()
	default:
		w.trace()
	}
}

// Sweep discovery is disjoint; allocator/card/free-list mutations are committed
// in handle order by the coordinator. Sparse indexed sweeps keep their existing
// path rather than paying for a dense parallel discovery pass.
func (c *Collector) sweepParallel() bool {
	if c.cfg.Workers <= 1 || runtime.GOMAXPROCS(0) <= 1 || (len(c.handles) < parallelMinObjects ||
		nativeCollectorFastPaths && len(c.majorBlocks) != 0 && len(c.freeHandles) >= len(c.handles)/2) && !c.forceParallelPhase() {
		return false
	}
	p := c.parallelPool()
	p.phase = parallelSweep
	p.selectParticipants(len(c.handles))
	p.run()
	if p.err != nil {
		return true
	}
	for i := 0; i < p.participants; i++ {
		for _, h := range p.workers[i].live {
			if c.handles[h].space == spaceOld || c.handles[h].space == spaceLarge {
				c.deferThroughputFree(h)
			} else {
				c.free(h)
			}
		}
	}
	c.compactNurseryHandles()
	return true
}

func (w *traceWorker) sweep() {
	p := w.pool
	c := p.collector
	start := len(c.handles) * w.index / p.participants
	end := len(c.handles) * (w.index + 1) / p.participants
	w.live = w.live[:0]
	for h := max(1, start); h < end && !p.failed.Load(); h++ {
		if p.taskHook != nil {
			p.taskHook(p.phase, w.index)
		}
		if c.handles[h].space != spaceFree && !c.mark[h] {
			w.live = append(w.live, uint32(h))
		}
	}
}

// Exact-span admission and precommit injection points precede copying.
// Contained helper failure leaves sources intact and aborts those reservations.
// Destinations and sources are disjoint; handles remain unchanged until join.
func (c *Collector) copyPromotionParallel(plans []plannedPromotion) bool {
	if c.cfg.Workers <= 1 || runtime.GOMAXPROCS(0) <= 1 || (len(plans) < parallelMinObjects && !c.forceParallelPhase()) {
		return false
	}
	var bytes uint64
	for _, plan := range plans {
		if plan.entry.space != spaceLarge {
			bytes += uint64(plan.entry.size)
			// Admission only needs a lower bound. Avoid walking the rest of a
			// large survivor plan before the copy and publication passes.
			if bytes >= 1<<20 {
				break
			}
		}
	}
	if bytes < 1<<20 && !c.forceParallelPhase() {
		return false
	}
	p := c.parallelPool()
	p.phase = parallelCopy
	p.selectParticipants(len(plans))
	p.plans = plans
	p.run()
	p.plans = nil
	return true
}

// Old-to-old uses the same immutable, disjoint prepared destinations. Sources
// resolve through unchanged handles until the join, with no nursery assumption.
func (c *Collector) copyMatureParallel(plans []plannedPromotion) bool {
	m := c.throughput.mature
	m.diagnostics.CopyWorkers = 1
	if c.cfg.Workers <= 1 || runtime.GOMAXPROCS(0) <= 1 || (len(plans) < parallelMinObjects && !c.forceParallelPhase()) {
		return false
	}
	p := c.parallelPool()
	p.phase = parallelCopy
	p.selectParticipants(len(plans))
	if m.diagnostics.Pressure {
		p.participants = len(p.workers)
	}
	m.diagnostics.CopyWorkers = p.participants
	p.plans = plans
	p.run()
	p.plans = nil
	return true
}

func (w *traceWorker) copyPromotions() {
	p := w.pool
	c := p.collector
	start := len(p.plans) * w.index / p.participants
	end := len(p.plans) * (w.index + 1) / p.participants
	for _, plan := range p.plans[start:end] {
		if p.failed.Load() {
			return
		}
		if plan.entry.space == spaceLarge {
			continue
		}
		var dst []byte
		if plan.entry.space == spaceNursery {
			dst = c.nursery[plan.entry.off : plan.entry.off+plan.entry.size]
		} else {
			dst = c.throughput.bytes(plan.entry)
		}
		copy(dst, c.bytes(makeObjRef(plan.handle)))
		if plan.entry.space == spaceOld {
			binary.LittleEndian.PutUint32(dst[12:16], binary.LittleEndian.Uint32(dst[12:16])|FlagOld)
		}
		if p.taskHook != nil {
			p.taskHook(p.phase, w.index)
		}
	}
}

func (c *Collector) closeParallel() {
	if p := c.parallel; p != nil {
		for i := 1; i < len(p.workers); i++ {
			close(p.workers[i].wake)
		}
		p.stopped.Wait()
		c.parallel = nil
	}
}

func (w *traceWorker) loop() {
	defer w.pool.stopped.Done()
	for phase := range w.wake {
		func() {
			defer w.pool.joined.Done()
			w.execute(phase)
		}()
	}
}

func (w *traceWorker) trace() {
	p := w.pool
	for {
		for w.localLen != 0 && !p.failed.Load() {
			last := w.localLen - 1
			task := w.local[last]
			w.localLen = last
			w.tasks++
			w.scan(task)
			if p.taskHook != nil {
				p.taskHook(p.phase, w.index)
			}
			// Publication is batched. Private chains do no queue locking or waking.
			if w.localLen >= 8 && p.idle.Load() != 0 {
				w.share()
			}
			if p.outstanding.Add(-1) == 0 {
				p.mu.Lock()
				p.ready.Broadcast()
				p.mu.Unlock()
			}
		}
		if task, ok := w.pop(); ok {
			w.local[0], w.localLen = task, 1
			continue
		}
		p.mu.Lock()
		for !p.failed.Load() && p.outstanding.Load() != 0 && len(p.queue) == 0 && p.overflowTasks.Load() == 0 {
			p.idle.Add(1)
			p.ready.Wait()
			p.idle.Add(-1)
		}
		done := p.failed.Load() || p.outstanding.Load() == 0
		p.mu.Unlock()
		if done {
			return
		}
	}
}

func (w *traceWorker) share() {
	p := w.pool
	p.mu.Lock()
	n := min(w.localLen/2, parallelQueueLimit-len(p.queue))
	if n != 0 {
		start := w.localLen - n
		p.queue = append(p.queue, w.local[start:w.localLen]...)
		w.localLen = start
		p.ready.Broadcast()
	}
	p.mu.Unlock()
}

func (p *parallelCollector) pendingVisible() bool {
	return p.overflowTasks.Load() != 0
}

func (w *traceWorker) enqueue(task traceTask) {
	p := w.pool
	p.outstanding.Add(1) // publication and termination are ordered after this increment
	if w.localLen < len(w.local) {
		w.local[w.localLen] = task
		w.localLen++
		return
	}
	p.mu.Lock()
	if len(p.queue) < parallelQueueLimit {
		p.queue = append(p.queue, task)
		p.ready.Signal()
		p.mu.Unlock()
		return
	}
	// Only uniquely claimed object work reaches this path. Array ranges use
	// enqueueArray and are scanned by their producer when the fixed queue is full.
	word, bit := task.handle/64, uint(task.handle%64)
	pending := &p.pending[word]
	mask := uint64(1) << bit
	for {
		old := atomic.LoadUint64(pending)
		if old&mask != 0 {
			p.mu.Unlock()
			panic("gc: duplicate parallel object overflow")
		}
		if atomic.CompareAndSwapUint64(pending, old, old|mask) {
			break
		}
	}
	p.overflowTasks.Add(1)
	p.ready.Broadcast()
	p.mu.Unlock()
}

// enqueueArray admits a continuation only when the fixed shared queue has
// room. Refusal does not create a task or change outstanding; the producer
// scans the range locally, matching Slipstream's bounded overflow protocol.
func (w *traceWorker) enqueueArray(task traceTask) bool {
	p := w.pool
	p.mu.Lock()
	if len(p.queue) == parallelQueueLimit {
		p.mu.Unlock()
		return false
	}
	p.outstanding.Add(1)
	p.queue = append(p.queue, task)
	p.ready.Signal()
	p.mu.Unlock()
	return true
}

func (w *traceWorker) pop() (traceTask, bool) {
	p := w.pool
	p.mu.Lock()
	if n := len(p.queue); n != 0 {
		task := p.queue[n-1]
		p.queue = p.queue[:n-1]
		p.mu.Unlock()
		return task, true
	}
	p.mu.Unlock()
	if p.overflowTasks.Load() == 0 {
		return traceTask{}, false
	}
	words := uint32(len(p.pending))
	for offset := uint32(0); offset < words; offset++ {
		index := (w.pendingAt + offset) % words
		wordBits := atomic.LoadUint64(&p.pending[index])
		for wordBits != 0 {
			bit := uint32(bits.TrailingZeros64(wordBits))
			mask := uint64(1) << bit
			if atomic.CompareAndSwapUint64(&p.pending[index], wordBits, wordBits&^mask) {
				p.overflowTasks.Add(-1)
				w.pendingAt = index
				return traceTask{handle: index*64 + bit}, true
			}
			wordBits = atomic.LoadUint64(&p.pending[index])
		}
	}
	w.pendingAt = 0
	return traceTask{}, false
}

func (w *traceWorker) mark(r Ref) {
	if !r.IsObj() {
		return
	}
	p := w.pool
	c := p.collector
	h := handleOf(r)
	if h == 0 || int(h) >= len(c.handles) {
		return
	}
	e := c.handles[h]
	if e.space == spaceFree || p.nursery && !e.young() {
		return
	}
	claim := &p.claims[h]
	previous := atomic.LoadUint32(claim)
	if previous == p.epoch || !atomic.CompareAndSwapUint32(claim, previous, p.epoch) {
		return
	}
	w.highWater = max(w.highWater, h+1)
	c.mark[h] = true
	w.enqueue(traceTask{handle: h})
}

func (w *traceWorker) scan(task traceTask) {
	c := w.pool.collector
	r := makeObjRef(task.handle)
	b := c.bytes(r)
	if task.end != 0 {
		count := binary.LittleEndian.Uint32(b[8:])
		for {
			for i := task.first; i < task.end; i++ {
				w.mark(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+i*4:])))
			}
			next := task.first + uint32(w.pool.participants*parallelArrayChunk)
			if next >= count {
				break
			}
			task.first, task.end = next, next+min(uint32(parallelArrayChunk), count-next)
			if w.enqueueArray(task) {
				break
			}
		}
		return
	}
	w.scanned++
	typeID := binary.LittleEndian.Uint32(b)
	if int(typeID) >= len(c.types) {
		return
	}
	d := c.types[typeID]
	if !d.HasRefs {
		return
	}
	if d.Kind == KindStruct {
		if nativeCollectorFastPaths && d.firstRef != 0 {
			for index := uint32(d.firstRef); index != 0; {
				field := d.Fields[index-1]
				w.mark(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+field.Offset:])))
				index = uint32(field.nextRef)
			}
		} else {
			for _, field := range d.Fields {
				if isCollectorRefKind(field.Kind) {
					w.mark(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+field.Offset:])))
				}
			}
		}
	} else if d.ArrayElementsAreRefs() {
		count := binary.LittleEndian.Uint32(b[8:])
		// Admit at most one continuation chain per participant. Each completed
		// chunk admits its successor. Queue refusal scans that chain locally, so
		// task storage is independent of array length and no range work is lost.
		for first, admitted := uint32(parallelArrayChunk), 0; first < count && admitted < w.pool.participants; first, admitted = first+parallelArrayChunk, admitted+1 {
			rangeTask := traceTask{handle: task.handle, first: first, end: first + min(uint32(parallelArrayChunk), count-first)}
			if !w.enqueueArray(rangeTask) {
				w.scan(rangeTask)
			}
		}
		for i := uint32(0); i < min(count, uint32(parallelArrayChunk)); i++ {
			w.mark(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+i*d.ElemSize:])))
		}
	}
}

func (c *Collector) parallelPoisonError() error {
	if c.parallel != nil {
		return c.parallel.poison
	}
	return nil
}

func (c *Collector) parallelActivity() ParallelActivity {
	if c.parallel == nil {
		return ParallelActivity{}
	}
	return c.parallel.activity
}

func (c *Collector) forceParallelPhase() bool {
	if p := c.parallel; p != nil && (p.hook != nil || p.taskHook != nil) {
		return true
	}
	m := c.throughput.mature
	return c.cfg.MatureEvacuation && m != nil && m.diagnostics.Kind == "major" && m.diagnostics.Pressure
}
