//go:build !tinygo && !wago_target_tinygo

package gc

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

const parallelCollectionSupported = true
const parallelMinObjects = 8192
const parallelArrayChunk = 1024
const parallelQueueLimit = 4096

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
	local     []traceTask
	live      []uint32
	scanned   uint64
	tasks     uint64
	index     int
	highWater uint32
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
	hook     func(phase uint8, worker int)
	taskHook func(phase uint8, worker int)

	collector        *Collector
	workers          []traceWorker
	joined           sync.WaitGroup
	stopped          sync.WaitGroup
	mu               sync.Mutex
	ready            *sync.Cond
	queue            []traceTask
	active           int // workers that can still publish work, including private stacks
	idle             atomic.Int32
	claims           []uint32 // per-handle epoch; separate from the serial mark representation
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
	if objects < parallelMinObjects {
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
		w.local = w.local[:0]
		w.live = w.live[:0]
		w.scanned = 0
		w.tasks = 0
		w.highWater = 0
	}
	// Seeds were marked by the serial root/remembered scan. Claim all of them
	// before dispatch, so an edge back to a seed cannot scan it a second time.
	for i, h := range c.markStack {
		p.claims[h] = p.epoch
		c.markHighWater = max(c.markHighWater, h+1)
		w := &p.workers[i%p.participants]
		w.local = append(w.local, traceTask{handle: h})
	}
	c.markStack = c.markStack[:0]
	p.active = p.participants
	p.run()
	var scanned uint64
	for i := range p.workers {
		scanned += p.workers[i].scanned
		c.markHighWater = max(c.markHighWater, p.workers[i].highWater)
	}
	if capture {
		c.promotionScratch = c.promotionScratch[:0]
		// CollectMinor uses the nursery ledger when every young object survives.
		// Do not materialize a captured list that the planner will discard.
		if scanned < uint64(len(c.nurseryHandles)) {
			for i := range p.workers {
				for _, h := range p.workers[i].live {
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
	p.active = 0
	for i := range p.workers {
		p.workers[i].local = p.workers[i].local[:0]
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
	if c.cfg.Workers <= 1 || runtime.GOMAXPROCS(0) <= 1 || len(c.handles) < parallelMinObjects ||
		nativeCollectorFastPaths && len(c.majorBlocks) != 0 && len(c.freeHandles) >= len(c.handles)/2 {
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
	if c.cfg.Workers <= 1 || runtime.GOMAXPROCS(0) <= 1 || len(plans) < parallelMinObjects {
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
	if bytes < 1<<20 {
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
	if c.cfg.Workers <= 1 || runtime.GOMAXPROCS(0) <= 1 || len(plans) < parallelMinObjects {
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
		for len(w.local) != 0 && !p.failed.Load() {
			last := len(w.local) - 1
			task := w.local[last]
			w.local = w.local[:last]
			w.tasks++
			w.scan(task)
			if p.taskHook != nil {
				p.taskHook(p.phase, w.index)
			}
			// Publication is batched. Private chains do no queue locking or waking.
			if len(w.local) >= 32 && p.idle.Load() != 0 {
				w.share()
			}
		}
		p.mu.Lock()
		if p.failed.Load() {
			p.mu.Unlock()
			return
		}
		p.active--
		for len(p.queue) == 0 && p.active != 0 && !p.failed.Load() {
			p.idle.Add(1)
			p.ready.Wait()
			p.idle.Add(-1)
		}
		if len(p.queue) == 0 || p.failed.Load() {
			p.ready.Broadcast()
			p.mu.Unlock()
			return
		}
		n := min(32, len(p.queue))
		start := len(p.queue) - n
		w.local = append(w.local, p.queue[start:]...)
		p.queue = p.queue[:start]
		p.active++
		p.mu.Unlock()
	}
}

func (w *traceWorker) share() {
	p := w.pool
	p.mu.Lock()
	n := min(len(w.local)/2, parallelQueueLimit-len(p.queue))
	if n != 0 {
		start := len(w.local) - n
		p.queue = append(p.queue, w.local[start:]...)
		w.local = w.local[:start]
		p.ready.Broadcast()
	}
	p.mu.Unlock()
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
	w.local = append(w.local, traceTask{handle: h})
}

func (w *traceWorker) scan(task traceTask) {
	c := w.pool.collector
	r := makeObjRef(task.handle)
	b := c.bytes(r)
	if task.end != 0 {
		for i := task.first; i < task.end; i++ {
			w.mark(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+i*4:])))
		}
		return
	}
	w.scanned++
	if w.pool.capture {
		w.live = append(w.live, task.handle)
	}
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
		// Reference payloads have a four-byte physical stride, for collector-managed refs.
		for first := uint32(parallelArrayChunk); first < count; {
			end := first + min(uint32(parallelArrayChunk), count-first)
			w.local = append(w.local, traceTask{handle: task.handle, first: first, end: end})
			first = end
		}
		if len(w.local) != 0 {
			w.share()
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
