//go:build linux && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

// Synthetic owners use real Instance root and construction methods, but never
// pretend to own native mappings. Tests leave these owners logically open so
// zero-root cleanup does not enter an unrelated compiled-instance teardown.
func dependencyTestGraph(t *testing.T, nodes, receipts int) *dependencyGraph {
	t.Helper()
	g, err := newDependencyGraph(nodes, receipts)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func dependencyTestMember(t *testing.T, g *dependencyGraph, lease bool) *dependencyMember {
	t.Helper()
	m := &dependencyMember{owner: &Instance{}}
	if lease {
		m.owner.beginConstruction(nil)
		t.Cleanup(func() {
			m.owner.invocationState.Store(0)
			m.owner.endConstruction()
		})
	}
	if err := g.register(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func dependencyTestAcquire(t *testing.T, g *dependencyGraph, m *dependencyMember) dependencyReceipt {
	t.Helper()
	r, err := g.acquirePending(m, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.release() })
	return r
}

func dependencyTestRefs(owner *Instance) int {
	owner.lifeMu.Lock()
	defer owner.lifeMu.Unlock()
	return owner.resourceRefs
}

func dependencyTestCounts(t *testing.T, g *dependencyGraph, m *dependencyMember, refs, receipts, external, incoming int) {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	m.owner.lifeMu.Lock()
	defer m.owner.lifeMu.Unlock()
	if m.owner.resourceRefs != refs || m.receipts != receipts || m.external != external || m.incoming != incoming {
		t.Fatalf("physical/receipt/external/incoming = %d/%d/%d/%d, want %d/%d/%d/%d", m.owner.resourceRefs, m.receipts, m.external, m.incoming, refs, receipts, external, incoming)
	}
}

func TestDependencyRegistrationAuthorityAndNonwrap(t *testing.T) {
	for _, pair := range [][2]int{{0, 1}, {1, 0}, {-1, 1}, {1, -1}, {4097, 1}, {1, 4097}} {
		if g, err := newDependencyGraph(pair[0], pair[1]); g != nil || !errors.Is(err, errDependencyCapacity) {
			t.Fatalf("invalid capacities %v: graph %p, error %v", pair, g, err)
		}
	}
	g := dependencyTestGraph(t, 1, 1)
	if g.owners != nil || g.records != nil || g.free != nil {
		t.Fatal("empty graph allocated membership or receipt storage")
	}
	if err := g.register(nil); !errors.Is(err, errDependencyMember) {
		t.Fatal(err)
	}
	if err := g.register(&dependencyMember{}); !errors.Is(err, errDependencyMember) {
		t.Fatal(err)
	}
	for _, owner := range []*Instance{{closed: true}, {finalizing: true}, {resourcesClosed: true}} {
		member := &dependencyMember{owner: owner}
		if err := g.register(member); !errors.Is(err, errDependencyClosed) || member.graph.Load() != nil {
			t.Fatalf("closed owner registered: %v", err)
		}
	}
	gate := &Instance{}
	gate.invocationState.Store(instanceInvocationClosed)
	if err := g.register(&dependencyMember{owner: gate}); !errors.Is(err, errDependencyClosed) {
		t.Fatal(err)
	}
	m := dependencyTestMember(t, g, false)
	if err := g.register(m); !errors.Is(err, errDependencyMember) {
		t.Fatalf("duplicate registration: %v", err)
	}
	if err := g.register(&dependencyMember{owner: m.owner}); !errors.Is(err, errDependencyMember) {
		t.Fatalf("duplicate owner registered: %v", err)
	}
	if err := g.register(&dependencyMember{owner: &Instance{}}); !errors.Is(err, errDependencyCapacity) {
		t.Fatalf("node cap: %v", err)
	}
	foreign := dependencyTestGraph(t, 1, 1)
	if err := foreign.register(m); !errors.Is(err, errDependencyMember) {
		t.Fatalf("same authoritative member joined second graph: %v", err)
	}
	if _, err := foreign.acquirePending(m, false); !errors.Is(err, errDependencyMember) || dependencyTestRefs(m.owner) != 0 {
		t.Fatalf("foreign acquisition: %v", err)
	}
	if err := g.unregister(m); !errors.Is(err, errDependencyState) {
		t.Fatalf("live owner unregistered: %v", err)
	}
	m.owner.lifeMu.Lock()
	m.owner.resourcesClosed = true
	m.owner.lifeMu.Unlock()
	if err := g.unregister(m); err != nil {
		t.Fatal(err)
	}
	if _, err := g.acquirePending(m, false); !errors.Is(err, errDependencyMember) {
		t.Fatalf("stale member acquired: %v", err)
	}
	if err := g.register(m); !errors.Is(err, errDependencyMember) {
		t.Fatalf("retired identity reused: %v", err)
	}
	replacement := dependencyTestMember(t, g, false)
	if replacement.identity == m.identity {
		t.Fatal("node identity reused after unregister")
	}
	last := dependencyTestGraph(t, 2, 1)
	last.nextIdentity = ^uint64(0) // bounded cold seam: the actual nonwrap branch
	max := dependencyTestMember(t, last, false)
	if max.identity != ^uint64(0) || last.nextIdentity != 0 {
		t.Fatal("last node identity wrapped")
	}
	if err := last.register(&dependencyMember{owner: &Instance{}}); !errors.Is(err, errDependencyIdentity) {
		t.Fatalf("exhausted node identity admitted: %v", err)
	}
}

func TestDependencyPendingExactAcquisitionAndFailureOwnership(t *testing.T) {
	g := dependencyTestGraph(t, 3, 2)
	target := dependencyTestMember(t, g, false)
	a := dependencyTestMember(t, g, true)
	b := dependencyTestMember(t, g, true)
	first := dependencyTestAcquire(t, g, target)
	second := dependencyTestAcquire(t, g, target)
	if first.identity == second.identity {
		t.Fatal("independent acquisitions share an identity")
	}
	dependencyTestCounts(t, g, target, 2, 2, 2, 0)
	if _, err := g.acquirePending(target, false); !errors.Is(err, errDependencyCapacity) {
		t.Fatalf("capacity acquired an unrecorded physical root: %v", err)
	}
	dependencyTestCounts(t, g, target, 2, 2, 2, 0)
	if _, err := first.adopt(a, 0); !errors.Is(err, errDependencyKind) {
		t.Fatal(err)
	}
	a.owner.invocationState.Store(instanceInvocationClosed)
	if _, err := first.adopt(a, dependencyFunction); !errors.Is(err, errDependencyClosed) {
		t.Fatal(err)
	}
	a.owner.invocationState.Store(0)
	if typed, err := first.adopt(a, dependencyFunction); !typed || err != nil {
		t.Fatalf("adoption: %v %v", typed, err)
	}
	if typed, err := second.adopt(b, dependencyMemory); !typed || err != nil {
		t.Fatalf("second adoption: %v %v", typed, err)
	}
	dependencyTestCounts(t, g, target, 2, 2, 0, 2)
	alias := first
	if _, err := alias.adopt(b, dependencyGlobalCell); !errors.Is(err, errDependencyState) {
		t.Fatalf("alias adopted twice: %v", err)
	}
	if !alias.release() || first.release() {
		t.Fatal("receipt aliases did not share one physical decrement")
	}
	dependencyTestCounts(t, g, target, 1, 1, 0, 1)
	if a.outgoing.Load() != 0 || b.outgoing.Load() != 1 {
		t.Fatal("release changed another receipt's consumer")
	}
	if !second.release() || second.release() {
		t.Fatal("second receipt did not release exactly once")
	}
	dependencyTestCounts(t, g, target, 0, 0, 0, 0)
	if (dependencyReceipt{}).release() {
		t.Fatal("zero handle released a root")
	}
}

func TestDependencyConsumerLifetimeAndOrderedEndpointOperations(t *testing.T) {
	g := dependencyTestGraph(t, 4, 2)
	low := dependencyTestMember(t, g, true)
	high := dependencyTestMember(t, g, true)
	noLease := dependencyTestMember(t, g, false)
	r := dependencyTestAcquire(t, g, high)
	if _, err := r.adopt(noLease, dependencyFunction); !errors.Is(err, errDependencyLease) {
		t.Fatalf("consumer without lifetime admitted: %v", err)
	}
	for _, mutate := range []func(*Instance){
		func(in *Instance) { in.closed = true },
		func(in *Instance) { in.finalizing = true },
		func(in *Instance) { in.resourcesClosed = true },
	} {
		low.owner.lifeMu.Lock()
		mutate(low.owner)
		low.owner.lifeMu.Unlock()
		_, err := r.adopt(low, dependencyFunction)
		low.owner.lifeMu.Lock()
		low.owner.closed, low.owner.finalizing, low.owner.resourcesClosed = false, false, false
		low.owner.lifeMu.Unlock()
		if !errors.Is(err, errDependencyClosed) {
			t.Fatalf("closed/finalizing consumer admitted: %v", err)
		}
	}
	if _, err := r.adopt(&dependencyMember{owner: &Instance{}}, dependencyFunction); !errors.Is(err, errDependencyMember) {
		t.Fatal(err)
	}
	if typed, err := r.adopt(low, dependencyFunction); !typed || err != nil {
		t.Fatalf("descending endpoint IDs failed: %v %v", typed, err)
	}
	if typed, err := r.transfer(high, dependencyFrozenDescriptor); !typed || err != nil {
		t.Fatalf("self-edge transfer failed: %v %v", typed, err)
	}
	if low.outgoing.Load() != 0 || high.outgoing.Load() != 1 || dependencyTestRefs(high.owner) != 1 {
		t.Fatal("transfer retained a new root or lost its old source")
	}
	if typed, err := r.transfer(low, dependencyTableStorage); !typed || err != nil {
		t.Fatalf("self to descending endpoint transfer failed: %v %v", typed, err)
	}
	noLease.owner.invocationState.Store(1) // actual invocation lifetime count
	if typed, err := r.transfer(noLease, dependencyContainerDescriptor); !typed || err != nil {
		t.Fatalf("invocation lifetime transfer: %v %v", typed, err)
	}
	noLease.owner.invocationState.Store(0)
	if !r.release() || noLease.outgoing.Load() != 0 {
		t.Fatal("committed receipt did not drain independently of ended lease")
	}
	dependencyTestCounts(t, g, high, 0, 0, 0, 0)
}

func TestDependencyCrossGraphExternalAdoptionAndTransfer(t *testing.T) {
	g := dependencyTestGraph(t, 2, 2)
	other := dependencyTestGraph(t, 1, 1)
	target := dependencyTestMember(t, g, false)
	local := dependencyTestMember(t, g, true)
	foreign := dependencyTestMember(t, other, true)
	r := dependencyTestAcquire(t, g, target)
	if typed, err := r.adopt(foreign, dependencyGlobalCell); typed || err != nil {
		t.Fatalf("foreign initial adoption must remain external: %v %v", typed, err)
	}
	dependencyTestCounts(t, g, target, 1, 1, 1, 0)
	if foreign.outgoing.Load() != 1 {
		t.Fatal("external commit lost exact consumer ownership")
	}
	foreign.owner.lifeMu.Lock()
	foreign.owner.resourcesClosed = true // test unregister guard, not a real teardown
	foreign.owner.lifeMu.Unlock()
	if err := other.unregister(foreign); !errors.Is(err, errDependencyState) {
		t.Fatalf("foreign member retired with outgoing external receipt: %v", err)
	}
	foreign.owner.lifeMu.Lock()
	foreign.owner.resourcesClosed = false
	foreign.owner.lifeMu.Unlock()
	if _, err := r.transfer(foreign, dependencyMemory); !errors.Is(err, errDependencyGraph) {
		t.Fatalf("cross-graph transfer changed ownership: %v", err)
	}
	dependencyTestCounts(t, g, target, 1, 1, 1, 0)
	if typed, err := r.transfer(local, dependencyMemory); !typed || err != nil {
		t.Fatalf("external to local transfer: %v %v", typed, err)
	}
	if foreign.outgoing.Load() != 0 || local.outgoing.Load() != 1 {
		t.Fatal("external transfer did not move one exact receipt")
	}
	dependencyTestCounts(t, g, target, 1, 1, 0, 1)
	if _, err := r.transfer(foreign, dependencyMemory); !errors.Is(err, errDependencyGraph) {
		t.Fatal(err)
	}
	r.release()
	dependencyTestCounts(t, g, target, 0, 0, 0, 0)
}

func TestDependencyClosedTargetPendingAdmission(t *testing.T) {
	g := dependencyTestGraph(t, 1, 3)
	target := dependencyTestMember(t, g, false)
	target.owner.invocationState.Store(instanceInvocationClosed)
	if _, err := g.acquirePending(target, false); !errors.Is(err, errDependencyClosed) {
		t.Fatal(err)
	}
	if target.owner.importState.Load() != nil || dependencyTestRefs(target.owner) != 0 {
		t.Fatal("rejected physical acquisition allocated a sidecar or root")
	}
	r, err := g.acquirePending(target, true)
	if err != nil {
		t.Fatalf("existing finalization retain mode not preserved: %v", err)
	}
	if !r.release() {
		t.Fatal("closed target pending root not releasable")
	}
	target.owner.lifeMu.Lock()
	target.owner.resourcesClosed = true
	target.owner.lifeMu.Unlock()
	if _, err := g.acquirePending(target, true); !errors.Is(err, errDependencyClosed) {
		t.Fatalf("physically dead target acquired finalization root: %v", err)
	}
	dependencyTestCounts(t, g, target, 0, 0, 0, 0)
	if err := g.unregister(target); err != nil {
		t.Fatal(err)
	}
}

func TestDependencyReceiptGenerationAndOutgoingOverflow(t *testing.T) {
	g := dependencyTestGraph(t, 2, 1)
	target := dependencyTestMember(t, g, false)
	consumer := dependencyTestMember(t, g, true)
	first := dependencyTestAcquire(t, g, target)
	first.release()
	second := dependencyTestAcquire(t, g, target)
	if first.identity == second.identity || first.release() {
		t.Fatal("recycled slot accepted stale release")
	}
	if _, err := first.adopt(consumer, dependencyFunction); !errors.Is(err, errDependencyIdentity) {
		t.Fatalf("stale adoption: %v", err)
	}
	consumer.outgoing.Store(^uint32(0))
	if _, err := second.adopt(consumer, dependencyFunction); !errors.Is(err, errDependencyCapacity) {
		t.Fatalf("outgoing count wrapped: %v", err)
	}
	consumer.outgoing.Store(0)
	dependencyTestCounts(t, g, target, 1, 1, 1, 0)
	second.release()
	g.mu.Lock()
	g.records[0].generation = maxDependencyReceiptGeneration - 1
	g.mu.Unlock()
	last := dependencyTestAcquire(t, g, target)
	if last.identity>>dependencyReceiptSlotBits != maxDependencyReceiptGeneration {
		t.Fatal("last receipt generation not used exactly")
	}
	last.release()
	if _, err := g.acquirePending(target, false); !errors.Is(err, errDependencyCapacity) {
		t.Fatalf("exhausted slot reused or wrapped: %v", err)
	}
	if first.release() || second.release() || last.release() {
		t.Fatal("exhausted identities acquired duplicate release")
	}
	dependencyTestCounts(t, g, target, 0, 0, 0, 0)
}

func TestDependencyReleaseReentrantAndPanicExactlyOnce(t *testing.T) {
	t.Run("reentrant", func(t *testing.T) {
		g := dependencyTestGraph(t, 3, 2)
		outer := dependencyTestMember(t, g, false)
		inner := dependencyTestMember(t, g, false)
		consumer := dependencyTestMember(t, g, true)
		r := dependencyTestAcquire(t, g, outer)
		if _, err := r.adopt(consumer, dependencyFunction); err != nil {
			t.Fatal(err)
		}
		g.afterRelease = func(owner *Instance) {
			if owner != outer.owner {
				return
			}
			// Real decrement precedes this seam; graph/life locks are released.
			dependencyTestCounts(t, g, outer, 0, 1, 1, 0)
			if r.release() {
				t.Fatal("reentrant alias released a consumed decrement")
			}
			child, err := g.acquirePending(inner, false)
			if err != nil {
				t.Fatal(err)
			}
			if child.identity&(maxDependencyPrimitiveCapacity-1) == r.identity&(maxDependencyPrimitiveCapacity-1) {
				t.Fatal("releasing slot recycled during cleanup")
			}
			if _, err := child.adopt(consumer, dependencyMemory); err != nil {
				child.release()
				t.Fatal(err)
			}
			child.release()
		}
		if !r.release() {
			t.Fatal("outer release lost ownership")
		}
		dependencyTestCounts(t, g, outer, 0, 0, 0, 0)
		dependencyTestCounts(t, g, inner, 0, 0, 0, 0)
		if consumer.outgoing.Load() != 0 || g.live != 0 {
			t.Fatal("recursive cleanup left receipt ownership")
		}
	})
	t.Run("panic", func(t *testing.T) {
		g := dependencyTestGraph(t, 1, 1)
		target := dependencyTestMember(t, g, false)
		r := dependencyTestAcquire(t, g, target)
		marker := &struct{ label string }{"cleanup panic"}
		g.afterRelease = func(*Instance) {
			dependencyTestCounts(t, g, target, 0, 1, 1, 0)
			if _, err := g.acquirePending(target, false); !errors.Is(err, errDependencyCapacity) {
				t.Fatalf("releasing capacity reclaimed before callback completion: %v", err)
			}
			panic(marker)
		}
		func() {
			defer func() {
				if got := recover(); got != marker {
					t.Fatalf("cleanup panic identity = %v", got)
				}
			}()
			r.release()
		}()
		if r.release() || g.live != 0 {
			t.Fatal("panic retry decremented a consumed root")
		}
		dependencyTestCounts(t, g, target, 0, 0, 0, 0)
	})
}

func TestDependencyConcurrentOperationsBalancePhysicalRoots(t *testing.T) {
	const workers, iterations = 16, 100
	g := dependencyTestGraph(t, 3, workers)
	target := dependencyTestMember(t, g, false)
	a := dependencyTestMember(t, g, true)
	b := dependencyTestMember(t, g, true)
	var wg sync.WaitGroup
	failures := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				r, err := g.acquirePending(target, false)
				if err != nil {
					failures <- fmt.Errorf("worker %d acquire: %w", worker, err)
					return
				}
				if typed, err := r.adopt(a, dependencyFunction); !typed || err != nil {
					r.release()
					failures <- fmt.Errorf("worker %d adopt: typed %v: %v", worker, typed, err)
					return
				}
				if typed, err := r.transfer(b, dependencyMemory); !typed || err != nil {
					r.release()
					failures <- fmt.Errorf("worker %d transfer: typed %v: %v", worker, typed, err)
					return
				}
				if !r.release() || r.release() {
					failures <- fmt.Errorf("worker %d duplicate release", worker)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	dependencyTestCounts(t, g, target, 0, 0, 0, 0)
	if a.outgoing.Load() != 0 || b.outgoing.Load() != 0 || g.live != 0 {
		t.Fatal("concurrent outgoing ownership did not drain")
	}
}

func TestDependencyConcurrentAliasesAndRegistration(t *testing.T) {
	t.Run("receipt-alias", func(t *testing.T) {
		g := dependencyTestGraph(t, 2, 1)
		target := dependencyTestMember(t, g, false)
		consumer := dependencyTestMember(t, g, true)
		r := dependencyTestAcquire(t, g, target)
		ready := make(chan struct{})
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() {
				<-ready
				_, err := r.adopt(consumer, dependencyFunction)
				results <- err
			}()
		}
		close(ready)
		first, second := <-results, <-results
		if (first == nil) == (second == nil) || first != nil && !errors.Is(first, errDependencyState) || second != nil && !errors.Is(second, errDependencyState) {
			t.Fatalf("alias adoption results: %v, %v", first, second)
		}
		dependencyTestCounts(t, g, target, 1, 1, 0, 1)
		released := make(chan bool, 2)
		for i := 0; i < 2; i++ {
			go func() { released <- r.release() }()
		}
		if first, second := <-released, <-released; first == second {
			t.Fatalf("alias releases: %v, %v", first, second)
		}
		dependencyTestCounts(t, g, target, 0, 0, 0, 0)
	})
	t.Run("authoritative-member", func(t *testing.T) {
		g := dependencyTestGraph(t, 1, 1)
		other := dependencyTestGraph(t, 1, 1)
		member := &dependencyMember{owner: &Instance{}}
		ready := make(chan struct{})
		results := make(chan error, 2)
		for _, graph := range []*dependencyGraph{g, other} {
			go func(graph *dependencyGraph) {
				<-ready
				results <- graph.register(member)
			}(graph)
		}
		close(ready)
		first, second := <-results, <-results
		if (first == nil) == (second == nil) || first != nil && !errors.Is(first, errDependencyMember) || second != nil && !errors.Is(second, errDependencyMember) {
			t.Fatalf("registration results: %v, %v", first, second)
		}
		winner := member.graph.Load()
		if winner != g && winner != other || !member.active.Load() || member.identity == 0 {
			t.Fatal("concurrent registration did not publish one authoritative member")
		}
		loser := g
		if winner == g {
			loser = other
		}
		if len(loser.owners) != 0 || loser.nextIdentity != 1 {
			t.Fatal("failed registration consumed another graph identity")
		}
		dependencyTestCounts(t, winner, member, 0, 0, 0, 0)
	})
}

func TestDependencyPartialBatchRollbackAndFailedTransfer(t *testing.T) {
	g := dependencyTestGraph(t, 3, 3)
	target := dependencyTestMember(t, g, false)
	consumer := dependencyTestMember(t, g, true)
	closed := dependencyTestMember(t, g, true)
	all := []dependencyReceipt{
		dependencyTestAcquire(t, g, target),
		dependencyTestAcquire(t, g, target),
		dependencyTestAcquire(t, g, target),
	}
	if _, err := all[0].adopt(consumer, dependencyFunction); err != nil {
		t.Fatal(err)
	}
	closed.owner.invocationState.Store(instanceInvocationClosed)
	if _, err := all[1].adopt(closed, dependencyGlobalCell); !errors.Is(err, errDependencyClosed) {
		t.Fatal(err)
	}
	if _, err := all[0].transfer(closed, dependencyMemory); !errors.Is(err, errDependencyClosed) {
		t.Fatal(err)
	}
	dependencyTestCounts(t, g, target, 3, 3, 2, 1)
	if consumer.outgoing.Load() != 1 || closed.outgoing.Load() != 0 {
		t.Fatal("failed transfer modified original consumer ownership")
	}
	g.mu.Lock()
	committed, _ := g.recordLocked(all[0].identity)
	preserved := committed != nil && committed.consumer == consumer && committed.kind == dependencyFunction
	g.mu.Unlock()
	if !preserved {
		t.Fatal("failed transfer modified source or dependency kind")
	}
	// One committed and two pending roots share rollback without recounting.
	for _, r := range all {
		if !r.release() {
			t.Fatal("partial batch lost a rollback receipt")
		}
	}
	dependencyTestCounts(t, g, target, 0, 0, 0, 0)
	if consumer.outgoing.Load() != 0 || g.live != 0 {
		t.Fatal("partial batch rollback did not drain")
	}
	// Physical overflow rejection is independent of graph metadata capacity.
	overflow := dependencyTestMember(t, dependencyTestGraph(t, 1, 1), false)
	overflow.owner.lifeMu.Lock()
	overflow.owner.resourceRefs = maxInt()
	overflow.owner.lifeMu.Unlock()
	if _, err := overflow.graph.Load().acquirePending(overflow, false); !errors.Is(err, errDependencyClosed) {
		t.Fatalf("physical root overflow admitted: %v", err)
	}
	if overflow.owner.importState.Load() != nil || dependencyTestRefs(overflow.owner) != maxInt() {
		t.Fatal("physical overflow allocated or decremented an unrelated root")
	}
}

func TestDependencyWarmReuseNoAllocations(t *testing.T) {
	g := dependencyTestGraph(t, 3, 1)
	target := dependencyTestMember(t, g, false)
	a := dependencyTestMember(t, g, true)
	b := dependencyTestMember(t, g, true)
	cycle := func() {
		r, err := g.acquirePending(target, false)
		if err != nil {
			panic(err)
		}
		if _, err := r.adopt(a, dependencyFunction); err != nil {
			r.release()
			panic(err)
		}
		if _, err := r.transfer(b, dependencyFrozenDescriptor); err != nil {
			r.release()
			panic(err)
		}
		if !r.release() {
			panic("lost release identity")
		}
	}
	cycle() // cold graph slot/free list and the actual target root sidecar
	record := &g.records[0]
	if got := testing.AllocsPerRun(50, cycle); got != 0 {
		t.Fatalf("warmed receipt retain/adopt/transfer/release allocations = %g", got)
	}
	if &g.records[0] != record || len(g.records) != 1 || g.live != 0 {
		t.Fatal("warm slot was not reused")
	}
	dependencyTestCounts(t, g, target, 0, 0, 0, 0)
}

func TestDependencyExactMaximumReceiptSlot(t *testing.T) {
	g := dependencyTestGraph(t, 1, maxDependencyPrimitiveCapacity)
	target := dependencyTestMember(t, g, false)
	all := make([]dependencyReceipt, maxDependencyPrimitiveCapacity)
	defer func() {
		for _, r := range all {
			r.release()
		}
	}()
	for i := range all {
		r, err := g.acquirePending(target, false)
		if err != nil {
			t.Fatal(err)
		}
		all[i] = r
	}
	if slot := all[len(all)-1].identity & (maxDependencyPrimitiveCapacity - 1); slot != 4095 {
		t.Fatalf("last slot = %d", slot)
	}
	if _, err := g.acquirePending(target, false); !errors.Is(err, errDependencyCapacity) {
		t.Fatal(err)
	}
	dependencyTestCounts(t, g, target, 4096, 4096, 4096, 0)
	for _, r := range all {
		if !r.release() {
			t.Fatal("full-capacity receipt lost release ownership")
		}
	}
	dependencyTestCounts(t, g, target, 0, 0, 0, 0)
}
