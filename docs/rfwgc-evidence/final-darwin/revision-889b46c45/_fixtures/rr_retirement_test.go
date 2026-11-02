//go:build darwin && arm64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func rrRetirementFixture(t *testing.T) (*referenceStore, *Instance, *gcStoreDomain) {
	t.Helper()
	c, e := gc.NewCollector(gc.Config{NurseryBytes: 4096}, nil)
	if e != nil {
		t.Fatal(e)
	}
	s := newReferenceStore(false)
	top := s.ensureGCTopology()
	d := &gcStoreDomain{id: newGCDomainIdentity(), collector: c, refs: 1, conversionPublishers: 1}
	top.appendDomainLocked(d)
	in := &Instance{refStore: s}
	s.instances = map[*Instance]*referenceStoreInstance{in: {dynamicInvocationDomains: true, conversionOnlyInvocationDomains: true}}
	in.executionFlags.Store(executionFlagDynamicGCDomain | executionFlagImportedGCDomain)
	return s, in, d
}
func rrRetirementIdle(s *referenceStore) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gcDomains.retirement.idle
}
func rrAwaitRetirement(t *testing.T, idle <-chan struct{}) {
	t.Helper()
	select {
	case <-idle:
	case <-time.After(3 * time.Second):
		t.Fatal("retirement worker lost progress")
	}
}
func rrDropRef(s *referenceStore, d *gcStoreDomain) {
	top := s.lockGCDomainTopology()
	s.mu.Lock()
	d.refs = 0
	s.mu.Unlock()
	top.Unlock()
}
func TestRRRetirementLastPinAndRevival(t *testing.T) {
	for _, revive := range []string{"none", "claim", "registration", "store-close"} {
		t.Run(revive, func(t *testing.T) {
			s, in, d := rrRetirementFixture(t)
			var closed atomic.Int32
			d.collector.AddReclaimObserver(func(r gc.Ref) {
				if r == gc.Null() {
					closed.Add(1)
				}
			})
			lease := in.lockGCInvocation(newInvocationID())
			rrDropRef(s, d)
			top := s.gcDomains
			if revive == "store-close" {
				s.closeRuntime()
			}
			top.RLock()
			done := make(chan struct{})
			go func() { lease.unlock(); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Second):
				top.RUnlock()
				t.Fatal("last pin waited for writer")
			}
			idle := rrRetirementIdle(s)
			s.mu.Lock()
			valid := top.domainForCollectorLocked(d.collector) == d && d.claims == 0 && len(top.invocations) == 0 && top.retirement.running
			s.mu.Unlock()
			if !valid || closed.Load() != 0 {
				top.RUnlock()
				t.Fatal("STOP queued retirement lost registered live collector")
			}
			top.mu.Lock()
			writers := top.writers
			top.mu.Unlock()
			if writers != 0 {
				top.RUnlock()
				t.Fatal("retirement reserved writer priority")
			}
			var next gcInvocationLease
			if revive == "claim" {
				next = in.lockGCInvocation(newInvocationID())
			}
			if revive == "registration" {
				s.mu.Lock()
				d.refs++
				s.mu.Unlock()
			}
			for i := 0; i < 32; i++ {
				s.mu.Lock()
				start := s.queueInvocationRetirementLocked([]*gcStoreDomain{d}, false)
				same := top.retirement.idle == idle
				s.mu.Unlock()
				if start || !same {
					top.RUnlock()
					t.Fatal("retirement failed to coalesce")
				}
			}
			top.RUnlock()
			rrAwaitRetirement(t, idle)
			if revive == "claim" || revive == "registration" {
				if closed.Load() != 0 {
					t.Fatal("STOP revived collector closed")
				}
				if revive == "registration" {
					next = in.lockGCInvocation(newInvocationID())
					rrDropRef(s, d)
				}
				next.unlock()
				rrAwaitRetirement(t, rrRetirementIdle(s))
			}
			s.mu.Lock()
			retired := top.domainForCollectorLocked(d.collector) == nil && !top.retirement.running && len(top.retirement.pending) == 0
			s.mu.Unlock()
			if !retired || closed.Load() != 1 {
				t.Fatalf("retirement=%t closes=%d", retired, closed.Load())
			}
		})
	}
}

func TestRRRetirementCallbackAndExitWakeups(t *testing.T) {
	s, in, d := rrRetirementFixture(t)
	second, e := gc.NewCollector(gc.Config{NurseryBytes: 4096}, nil)
	if e != nil {
		t.Fatal(e)
	}
	d2 := &gcStoreDomain{id: newGCDomainIdentity(), collector: second}
	s.gcDomains.appendDomainLocked(d2)
	entered, release := make(chan struct{}), make(chan struct{})
	var closed atomic.Int32
	d.collector.AddReclaimObserver(func(r gc.Ref) {
		if r != gc.Null() {
			return
		}
		closed.Add(1)
		// Collector finalizers execute outside every retirement/execution lock.
		if !s.mu.TryLock() {
			t.Error("callback holds store mutex")
		} else {
			s.mu.Unlock()
		}
		if !s.gcDomains.TryLock() {
			t.Error("callback holds topology gate")
		} else {
			s.gcDomains.Unlock()
		}
		if !d.mu.TryLock() {
			t.Error("callback holds domain mutex")
		} else {
			d.mu.Unlock()
		}
		if !nativeExecutionMu.TryLock() {
			t.Error("callback holds native mutex")
		} else {
			nativeExecutionMu.Unlock()
		}
		close(entered)
		<-release
	})
	d2.collector.AddReclaimObserver(func(r gc.Ref) {
		if r == gc.Null() {
			closed.Add(1)
		}
	})
	lease := in.lockGCInvocation(newInvocationID())
	rrDropRef(s, d)
	s.gcDomains.RLock()
	lease.unlock()
	idle := rrRetirementIdle(s)
	s.gcDomains.RUnlock()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("callback not reached")
	}
	s.mu.Lock()
	start := s.queueInvocationRetirementLocked([]*gcStoreDomain{d2}, false)
	same := s.gcDomains.retirement.idle == idle
	s.mu.Unlock()
	if start || !same {
		t.Fatal("callback request started second worker")
	}
	close(release)
	rrAwaitRetirement(t, idle)
	if closed.Load() != 2 {
		t.Fatal("callback request lost")
	}
	// Race new requests with worker exit; duplicate/stale candidates cannot close twice.
	for round := 0; round < 16; round++ {
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.mu.Lock()
				start := s.queueInvocationRetirementLocked([]*gcStoreDomain{d, d2}, false)
				s.mu.Unlock()
				if start {
					s.driveInvocationRetirement()
				}
			}()
		}
		wg.Wait()
		rrAwaitRetirement(t, rrRetirementIdle(s))
	}
	if closed.Load() != 2 {
		t.Fatal("stale candidate closed twice")
	}
}

func TestRRRetirementBorrowedRootLatePublisher(t *testing.T) {
	s, root, d := rrRetirementFixture(t)
	lease := root.lockGCInvocation(newInvocationID())
	defer func() { lease.unlock(); d.collector.Close() }()
	target := &Instance{refStore: s}
	target.executionFlags.Store(executionFlagDynamicGCDomain | executionFlagImportedGCDomain)
	s.mu.Lock()
	s.instances[target] = &referenceStoreInstance{dynamicInvocationDomains: true, conversionOnlyInvocationDomains: true}
	s.mu.Unlock()
	c, e := gc.NewCollector(gc.Config{NurseryBytes: 4096}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	top := s.lockGCDomainTopology()
	s.mu.Lock()
	top.appendDomainLocked(&gcStoreDomain{id: newGCDomainIdentity(), collector: c, refs: 1, conversionPublishers: 1})
	s.mu.Unlock()
	top.Unlock()
	// A contended topology writer must not be acquired or waited on by a nested
	// entry already owning root domains. A wider view fails before any gate wait.
	top.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	borrowed, err := target.lockGCInvocationContext(ctx, lease.owner)
	top.Unlock()
	if err == nil {
		borrowed.unlock()
		t.Fatal("nested root widened authority")
	}
	if ctx.Err() != nil {
		t.Fatal("nested partial owner waited instead of rejecting")
	}
	if !lease.domains.ownedBy(lease.owner) {
		t.Fatal("nested rejection dropped root authority")
	}
}

func TestRRRetirementCanceledPartialLastPins(t *testing.T) {
	s, in, d := rrRetirementFixture(t)
	c, e := gc.NewCollector(gc.Config{NurseryBytes: 4096}, nil)
	if e != nil {
		t.Fatal(e)
	}
	d2 := &gcStoreDomain{id: newGCDomainIdentity(), collector: c, refs: 1, conversionPublishers: 1}
	s.gcDomains.appendDomainLocked(d2)
	var closes atomic.Int32
	for _, d := range []*gcStoreDomain{d, d2} {
		d.collector.AddReclaimObserver(func(r gc.Ref) {
			if r == gc.Null() {
				closes.Add(1)
			}
		})
	}
	d2.invocationMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		lease, e := in.lockGCInvocationContext(ctx, newInvocationID())
		if e == nil {
			lease.unlock()
		}
		done <- e
	}()
	awaitGCLifecycle(t, func() bool { return d2.invocationMu.state.Load()&invocationGateWaiters != 0 })
	top := s.lockGCDomainTopology()
	s.mu.Lock()
	d.refs = 0
	d2.refs = 0
	s.mu.Unlock()
	top.Unlock()
	top.RLock()
	cancel()
	select {
	case e = <-done:
	case <-time.After(time.Second):
		top.RUnlock()
		d2.invocationMu.Unlock()
		t.Fatal("partial cancellation waited for retirement writer")
	}
	if e != context.Canceled {
		t.Fatalf("cancel=%v", e)
	}
	if d.invocationMu.state.Load()&invocationGateHeld != 0 {
		t.Fatal("partial domain gate leaked")
	}
	s.mu.Lock()
	valid := d.claims == 0 && d2.claims == 0 && len(top.invocations) == 0 && len(top.retirement.pending) == 2
	s.mu.Unlock()
	if !valid || closes.Load() != 0 {
		t.Fatal("STOP partial pin unwind lost pending lifetime")
	}
	idle := rrRetirementIdle(s)
	d2.invocationMu.Unlock()
	top.RUnlock()
	rrAwaitRetirement(t, idle)
	if closes.Load() != 2 {
		t.Fatal("partial canceled pins were not retired")
	}
}

func TestRRRetirementCallbackRefreshBehindReader(t *testing.T) {
	s, in, d := rrRetirementFixture(t)
	var closes atomic.Int32
	d.collector.AddReclaimObserver(func(r gc.Ref) {
		if r == gc.Null() {
			closes.Add(1)
		}
	})
	lease := in.lockGCInvocation(newInvocationID())
	suspension := in.suspendGCInvocation(lease.owner)
	top := s.lockGCDomainTopology()
	s.mu.Lock()
	d.refs = 0
	d.conversionPublishers = 0
	s.mu.Unlock()
	top.Unlock()
	top.RLock()
	done := make(chan struct{})
	go func() { suspension.resume(); lease.unlock(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		top.RUnlock()
		t.Fatal("callback refresh waited for old-pin retirement")
	}
	s.mu.Lock()
	valid := len(top.invocations) == 0 && d.claims == 0 && top.domainForCollectorLocked(d.collector) == d
	s.mu.Unlock()
	if !valid || closes.Load() != 0 {
		t.Fatal("STOP callback refresh lifetime accounting")
	}
	idle := rrRetirementIdle(s)
	top.RUnlock()
	rrAwaitRetirement(t, idle)
	if closes.Load() != 1 {
		t.Fatal("callback old domain not retired")
	}
}
