//go:build !tinygo && !wago_target_tinygo

package gc

import (
	"sync"
	"testing"
)

// Installed only in immutable PR1020/foundation snapshots, whose worker types
// do not exist in the serial main/port. No fixture-dependent skips.
type r9ProtocolUnlock struct {
	mu         *sync.Mutex
	registered chan struct{}
	once       sync.Once
}

func (l *r9ProtocolUnlock) Lock() { l.mu.Lock() }
func (l *r9ProtocolUnlock) Unlock() {
	// Cond.Wait registered its notification ticket before invoking Unlock.
	l.once.Do(func() { close(l.registered) })
	l.mu.Unlock()
}

func TestR9ProtocolConditionPublication(t *testing.T) {
	for _, epoch := range []uint32{0xfffffffe, 0xffffffff, 0} {
		p := &parallelCollector{collector: &Collector{}, epoch: epoch}
		p.outstanding.Store(1)
		registered := make(chan struct{})
		p.ready = sync.NewCond(&r9ProtocolUnlock{mu: &p.mu, registered: registered})
		w := &traceWorker{pool: p}
		done := make(chan struct{})
		go func() { w.trace(); close(done) }()
		<-registered
		p.mu.Lock()
		// Force the last producer to finish at the registered-wait boundary.
		// Neither the condition predicate nor this notification uses epoch.
		p.outstanding.Store(0)
		p.ready.Broadcast()
		p.mu.Unlock()
		<-done
	}
}

func TestR9ProtocolStopBeforeWorkerReceive(t *testing.T) {
	for _, epoch := range []uint32{0xfffffffe, 0xffffffff, 0} {
		c := &Collector{}
		p := &parallelCollector{collector: c, epoch: epoch, workers: make([]traceWorker, 2)}
		c.parallel = p
		w := &p.workers[1]
		w.pool = p
		w.wake = make(chan *parallelPhase)
		p.stopped.Add(1)
		done := make(chan struct{})
		go func() { c.closeParallel(); close(done) }()
		// Receive proves the production close happened before the helper loop
		// starts; closed channels retain STOP without a transient notification.
		if _, ok := <-w.wake; ok {
			t.Fatal("unexpected work ticket")
		}
		go w.loop()
		<-done
		if c.parallel != nil {
			t.Fatal("pool retained after joined stop")
		}
	}
}

func TestR9ProtocolPhaseBeforeWorkerReceive(t *testing.T) {
	c := &Collector{}
	p := &parallelCollector{collector: c, phase: parallelSweep, workers: make([]traceWorker, 2), epoch: 0xfffffffe}
	c.parallel = p
	for i := range p.workers {
		p.workers[i].pool = p
		p.workers[i].index = i
	}
	p.workers[1].wake = make(chan *parallelPhase)
	p.stopped.Add(1)
	done := make(chan struct{})
	go func() { p.run(); close(done) }()
	go p.workers[1].loop()
	<-done
	c.closeParallel()
}
