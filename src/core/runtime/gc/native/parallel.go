//go:build !tinygo && !wago_target_tinygo

package gc

import (
	"encoding/binary"
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
	pool    *parallelCollector
	wake    chan struct{}
	local   []traceTask
	live    []uint32
	scanned uint64
	tasks   uint64
	index   int
}

// The mutator is stopped by the caller. Helpers only read heap/descriptor data
// and uniquely write claimed mark entries or admitted payload destinations.
// Root callbacks, handle publication, allocator updates, and card maintenance
// remain on the caller after the join barrier.
type parallelCollector struct {
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
	for i := range p.workers {
		w := &p.workers[i]
		w.local = w.local[:0]
		w.live = w.live[:0]
		w.scanned = 0
		w.tasks = 0
	}
	// Seeds were marked by the serial root/remembered scan. Claim all of them
	// before dispatch, so an edge back to a seed cannot scan it a second time.
	for i, h := range c.markStack {
		p.claims[h] = p.epoch
		w := &p.workers[i%len(p.workers)]
		w.local = append(w.local, traceTask{handle: h})
	}
	c.markStack = c.markStack[:0]
	p.active = len(p.workers)
	p.run()
	var scanned uint64
	if capture {
		c.promotionScratch = c.promotionScratch[:0]
	}
	for i := range p.workers {
		w := &p.workers[i]
		scanned += w.scanned
		if capture {
			for _, h := range w.live {
				c.promotionScratch = append(c.promotionScratch, plannedPromotion{handle: h})
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
				w.wake = make(chan struct{})
				p.stopped.Add(1)
				go w.loop()
			}
		}
		c.parallel = p
	}
	return p
}

func (p *parallelCollector) run() {
	p.joined.Add(len(p.workers) - 1)
	for i := 1; i < len(p.workers); i++ {
		p.workers[i].wake <- struct{}{}
	}
	p.workers[0].runPhase()
	p.joined.Wait()
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
	p.run()
	for i := range p.workers {
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
	start := len(c.handles) * w.index / len(p.workers)
	end := len(c.handles) * (w.index + 1) / len(p.workers)
	w.live = w.live[:0]
	for h := max(1, start); h < end; h++ {
		if c.handles[h].space != spaceFree && !c.mark[h] {
			w.live = append(w.live, uint32(h))
		}
	}
}

// Admission and all injected failure points have completed before copying.
// Destinations and sources are disjoint; handles remain unchanged until join.
func (c *Collector) copyPromotionParallel(plans []plannedPromotion) bool {
	if c.cfg.Workers <= 1 || runtime.GOMAXPROCS(0) <= 1 || len(plans) < parallelMinObjects {
		return false
	}
	var bytes uint64
	for _, plan := range plans {
		if plan.entry.space != spaceLarge {
			bytes += uint64(plan.entry.size)
		}
	}
	if bytes < 1<<20 {
		return false
	}
	p := c.parallelPool()
	p.phase = parallelCopy
	p.plans = plans
	p.run()
	p.plans = nil
	return true
}

func (w *traceWorker) copyPromotions() {
	p := w.pool
	c := p.collector
	start := len(p.plans) * w.index / len(p.workers)
	end := len(p.plans) * (w.index + 1) / len(p.workers)
	for _, plan := range p.plans[start:end] {
		if plan.entry.space == spaceLarge {
			continue
		}
		src := c.handles[plan.handle]
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
		c.poisonYoungSource(src)
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
	for range w.wake {
		w.runPhase()
		w.pool.joined.Done()
	}
}

func (w *traceWorker) trace() {
	p := w.pool
	for {
		for len(w.local) != 0 {
			last := len(w.local) - 1
			task := w.local[last]
			w.local = w.local[:last]
			w.tasks++
			w.scan(task)
			// Publication is batched. Private chains do no queue locking or waking.
			if len(w.local) >= 32 && p.idle.Load() != 0 {
				w.share()
			}
		}
		p.mu.Lock()
		p.active--
		for len(p.queue) == 0 && p.active != 0 {
			p.idle.Add(1)
			p.ready.Wait()
			p.idle.Add(-1)
		}
		if len(p.queue) == 0 {
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
