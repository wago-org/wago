//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

// Independent RR3 retirement re-review adversaries (rr3-retirement-recheck).
// Archived probe: copied into an immutable 5847df30f export to execute. It uses
// the new retirement state, so it does not build at earlier pins.

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type rr3xFix struct {
	s       *referenceStore
	in      *Instance
	domains []*gcStoreDomain
	closes  []*atomic.Int32
}

// n registered publisher domains (refs=1) and one conversion-only instance.
func rr3xNewFix(t *testing.T, n int) *rr3xFix {
	t.Helper()
	f := &rr3xFix{s: newReferenceStore(false)}
	top := f.s.ensureGCTopology()
	for i := 0; i < n; i++ {
		c, err := gc.NewCollector(gc.Config{NurseryBytes: 4096}, nil)
		if err != nil {
			t.Fatal(err)
		}
		d := &gcStoreDomain{id: newGCDomainIdentity(), collector: c, refs: 1, conversionPublishers: 1}
		top.appendDomainLocked(d)
		var closes atomic.Int32
		c.AddReclaimObserver(func(r gc.Ref) {
			if r == gc.Null() {
				closes.Add(1)
			}
		})
		f.domains = append(f.domains, d)
		f.closes = append(f.closes, &closes)
	}
	f.in = &Instance{refStore: f.s}
	f.s.instances = map[*Instance]*referenceStoreInstance{f.in: {dynamicInvocationDomains: true, conversionOnlyInvocationDomains: true}}
	f.in.executionFlags.Store(executionFlagDynamicGCDomain | executionFlagImportedGCDomain)
	return f
}

func (f *rr3xFix) dropRefs(ds ...*gcStoreDomain) {
	top := f.s.lockGCDomainTopology()
	f.s.mu.Lock()
	for _, d := range ds {
		d.refs = 0
	}
	f.s.mu.Unlock()
	top.Unlock()
}

func (f *rr3xFix) idle(t *testing.T) {
	t.Helper()
	f.s.mu.Lock()
	r := f.s.gcDomains.retirement
	var ch chan struct{}
	if r != nil {
		ch = r.idle
	}
	f.s.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("STOP retirement never reached idle (lost wakeup or leak)")
	}
}

func (f *rr3xFix) totalCloses() (n int32) {
	for _, c := range f.closes {
		n += c.Load()
	}
	return
}

// Uncontended last-pin release finishes synchronously on the caller. Its Close
// callbacks run with no store/topology/domain/native lock, and a request made
// from inside the callback is drained by the same driver.
func TestRR3XSyncCloseCallbackUnlockedAndReentrant(t *testing.T) {
	f := rr3xNewFix(t, 2)
	d, d2 := f.domains[0], f.domains[1]
	var bad atomic.Value
	var inCallback atomic.Bool
	d.collector.AddReclaimObserver(func(r gc.Ref) {
		if r != gc.Null() {
			return
		}
		inCallback.Store(true)
		if !f.s.mu.TryLock() {
			bad.Store("store mutex held")
		} else {
			f.s.mu.Unlock()
		}
		if !f.s.gcDomains.TryLock() {
			bad.Store("topology gate held")
		} else {
			f.s.gcDomains.Unlock()
		}
		for _, x := range f.domains {
			if x.invocationMu.state.Load()&invocationGateHeld != 0 {
				bad.Store("domain gate held")
			}
		}
		if !nativeExecutionMu.TryLock() {
			bad.Store("native mutex held")
		} else {
			nativeExecutionMu.Unlock()
		}
		// Reentrant request from the finalizer: d2 becomes a candidate now.
		f.s.mu.Lock()
		d2.refs = 0
		start := f.s.queueInvocationRetirementLocked([]*gcStoreDomain{d2}, false)
		f.s.mu.Unlock()
		if start {
			bad.Store("reentrant request started a second driver")
		}
	})
	lease := f.in.lockGCInvocation(newInvocationID())
	f.dropRefs(d)
	done := make(chan struct{})
	go func() { lease.unlock(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("STOP uncontended release blocked (self-deadlock in synchronous callback)")
	}
	if !inCallback.Load() {
		t.Fatal("uncontended release did not retire synchronously")
	}
	f.idle(t)
	if v := bad.Load(); v != nil {
		t.Fatalf("callback: %v", v)
	}
	if f.closes[0].Load() != 1 || f.closes[1].Load() != 1 {
		t.Fatalf("closes d=%d d2=%d, want 1/1", f.closes[0].Load(), f.closes[1].Load())
	}
}

// 64 last pins released behind a live reader, plus 16x8 concurrent duplicate
// requests. One worker at most, no writer ticket, reader admission continues,
// a real queued writer is not overtaken and is not starved, every collector
// closes exactly once.
func TestRR3XCoalescedBoundedWorkerBehindReaderAndQueuedWriter(t *testing.T) {
	const n = 64
	f := rr3xNewFix(t, n)
	lease := f.in.lockGCInvocation(newInvocationID())
	f.dropRefs(f.domains...)
	top := f.s.gcDomains
	top.RLock()
	base := runtime.NumGoroutine()
	done := make(chan struct{})
	go func() { lease.unlock(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		top.RUnlock()
		t.Fatal("STOP last-pin release waited behind reader")
	}
	var wg sync.WaitGroup
	for round := 0; round < 16; round++ {
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				f.s.mu.Lock()
				start := f.s.queueInvocationRetirementLocked(f.domains, false)
				f.s.mu.Unlock()
				if start {
					f.s.driveInvocationRetirement()
				}
			}()
		}
	}
	wg.Wait()
	time.Sleep(20 * time.Millisecond)
	if g := runtime.NumGoroutine() - base; g > 2 {
		top.RUnlock()
		t.Fatalf("STOP %d extra goroutines while retirement is pending (unbounded workers)", g)
	}
	top.mu.Lock()
	writers, writer := top.writers, top.writer
	top.mu.Unlock()
	if writers != 0 || writer {
		top.RUnlock()
		t.Fatal("STOP pending retirement holds or reserves the writer")
	}
	if !top.TryRLock() {
		top.RUnlock()
		t.Fatal("STOP reader admission blocked by pending retirement")
	}
	top.RUnlock()
	if f.totalCloses() != 0 {
		top.RUnlock()
		t.Fatal("STOP collector closed under a live reader")
	}
	// A real queued writer, then the last reader leaves.
	wrote := make(chan int32, 1)
	go func() { top.Lock(); wrote <- f.totalCloses(); time.Sleep(10 * time.Millisecond); top.Unlock() }()
	awaitGCLifecycle(t, func() bool { top.mu.Lock(); defer top.mu.Unlock(); return top.writers == 1 })
	top.RUnlock()
	select {
	case <-wrote:
	case <-time.After(3 * time.Second):
		t.Fatal("STOP queued writer starved by retirement")
	}
	f.idle(t)
	for i, c := range f.closes {
		if c.Load() != 1 {
			t.Fatalf("domain %d closed %d times", i, c.Load())
		}
	}
	f.s.mu.Lock()
	left := top.n
	f.s.mu.Unlock()
	if left != 0 {
		t.Fatalf("STOP %d domains leaked in topology", left)
	}
}

// Randomized race between the final reader's RUnlock and the retirement
// worker's subscribe/try-lock. Every iteration must reach idle and close once.
func TestRR3XNoLostWakeupStress(t *testing.T) {
	for i := 0; i < 300; i++ {
		f := rr3xNewFix(t, 1)
		lease := f.in.lockGCInvocation(newInvocationID())
		f.dropRefs(f.domains[0])
		top := f.s.gcDomains
		top.RLock()
		var start sync.WaitGroup
		start.Add(2)
		go func() { start.Done(); start.Wait(); lease.unlock() }()
		go func() { start.Done(); start.Wait(); runtime.Gosched(); top.RUnlock() }()
		deadline := time.Now().Add(5 * time.Second)
		for f.closes[0].Load() == 0 {
			if time.Now().After(deadline) {
				t.Fatalf("STOP iteration %d lost the retirement wakeup", i)
			}
			runtime.Gosched()
		}
		f.idle(t)
		if f.closes[0].Load() != 1 {
			t.Fatalf("iteration %d closed %d times", i, f.closes[0].Load())
		}
	}
}

// Store close with a live pin: terminal release waits for the last unpin, the
// unpin does not wait for a reader, and each collector closes exactly once
// (the candidate via unlink, a still-registered domain via terminal release).
func TestRR3XTerminalCloseRuntimeBehindReader(t *testing.T) {
	f := rr3xNewFix(t, 2)
	d := f.domains[0]
	lease := f.in.lockGCInvocation(newInvocationID())
	f.dropRefs(d)
	f.s.closeRuntime()
	f.s.mu.Lock()
	early := f.s.gcDomains.n != 2
	f.s.mu.Unlock()
	if early || f.totalCloses() != 0 {
		t.Fatal("STOP terminal release ran with a live invocation snapshot")
	}
	top := f.s.gcDomains
	top.RLock()
	done := make(chan struct{})
	go func() { lease.unlock(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		top.RUnlock()
		t.Fatal("STOP terminal last unpin waited for writer")
	}
	f.s.mu.Lock()
	pending := f.s.gcDomains.retirement != nil && f.s.gcDomains.retirement.terminal
	f.s.mu.Unlock()
	if !pending || f.totalCloses() != 0 {
		top.RUnlock()
		t.Fatal("STOP terminal request lost or ran under reader")
	}
	top.RUnlock()
	f.idle(t)
	for i, c := range f.closes {
		if c.Load() != 1 {
			t.Fatalf("domain %d closed %d times", i, c.Load())
		}
	}
	// A late duplicate terminal/candidate request is a no-op (no double Close).
	f.s.mu.Lock()
	start := f.s.queueInvocationRetirementLocked(f.domains, true)
	f.s.mu.Unlock()
	if start {
		f.s.driveInvocationRetirement()
	}
	f.idle(t)
	if f.totalCloses() != 2 {
		t.Fatalf("STOP duplicate terminal request closed again: %d", f.totalCloses())
	}
}

// Borrowed-root authority: a foreign owner never borrows; a suspended root is
// not borrowed (fresh admission instead); after root release the owner admits
// afresh; a covered static callee borrows; a closed (unregistered) callee fails
// closed without dropping root authority.
func TestRR3XBorrowedRootStaleAndForeign(t *testing.T) {
	f := rr3xNewFix(t, 1)
	d := f.domains[0]
	root := f.in.lockGCInvocation(newInvocationID())
	target := &Instance{refStore: f.s}
	target.executionFlags.Store(executionFlagDynamicGCDomain | executionFlagImportedGCDomain)
	f.s.mu.Lock()
	f.s.instances[target] = &referenceStoreInstance{dynamicInvocationDomains: true, conversionOnlyInvocationDomains: true}
	f.s.mu.Unlock()

	// Foreign owner: must not borrow; it waits on the root-held gate.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	foreign, err := target.lockGCInvocationContext(ctx, newInvocationID())
	cancel()
	if err == nil {
		foreign.unlock()
		t.Fatal("STOP foreign owner accepted root authority")
	}
	if !root.domains.ownedBy(root.owner) {
		t.Fatal("STOP foreign attempt dropped root authority")
	}
	// Same owner, active root: borrows without acquiring.
	b, err := target.lockGCInvocationContext(context.Background(), root.owner)
	if err != nil || b.acquired {
		t.Fatalf("valid nested borrow: err=%v acquired=%t", err, b.acquired)
	}
	b.unlock()
	if !root.domains.ownedBy(root.owner) {
		t.Fatal("STOP nested unlock released root authority")
	}
	// Static callee whose own domain is covered: borrows.
	static := &Instance{refStore: f.s}
	f.s.mu.Lock()
	f.s.instances[static] = &referenceStoreInstance{gcDomain: d}
	f.s.mu.Unlock()
	if sb, err := static.lockGCInvocationContext(context.Background(), root.owner); err != nil || sb.acquired {
		t.Fatalf("covered static nested borrow: err=%v acquired=%t", err, sb.acquired)
	}
	// Unregistered callee: fails closed, root intact.
	gone := &Instance{refStore: f.s}
	gone.executionFlags.Store(executionFlagDynamicGCDomain | executionFlagImportedGCDomain)
	if _, err := gone.lockGCInvocationContext(context.Background(), root.owner); err == nil {
		t.Fatal("STOP unregistered callee borrowed root authority")
	}
	if !root.domains.ownedBy(root.owner) {
		t.Fatal("STOP rejection dropped root authority")
	}
	// Suspended root: not borrowed; fresh admission acquires its own snapshot.
	susp := f.in.suspendGCInvocation(root.owner)
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	fresh, err := target.lockGCInvocationContext(ctx, root.owner)
	cancel()
	if err != nil || !fresh.acquired || fresh.snapshot == nil {
		t.Fatalf("STOP callback reentry did not admit afresh: err=%v", err)
	}
	fresh.unlock()
	susp.resume()
	if !root.domains.ownedBy(root.owner) {
		t.Fatal("STOP resume lost root authority")
	}
	root.unlock()
	// Released root: same owner admits afresh, nothing stale is borrowed.
	again, err := target.lockGCInvocationContext(context.Background(), root.owner)
	if err != nil || !again.acquired {
		t.Fatalf("STOP stale root borrowed after release: err=%v acquired=%t", err, again.acquired)
	}
	again.unlock()
	f.idle(t)
	f.s.mu.Lock()
	clean := len(f.s.gcDomains.invocations) == 0 && d.claims == 0 && d.refs == 1
	f.s.mu.Unlock()
	if !clean || f.totalCloses() != 0 {
		t.Fatal("STOP borrow paths leaked pins or retired a registered domain")
	}
	d.collector.Close()
}
