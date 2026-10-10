//go:build linux && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func dependencyTestClosedForDiscovery(t *testing.T, members ...*dependencyMember) {
	t.Helper()
	for _, member := range members {
		owner := member.owner
		state := &instanceCloseState{}
		state.prepared.Store(true)
		state.completed.Store(true)
		state.terminalComplete.Store(true)
		owner.lifeMu.Lock()
		owner.closed = true
		owner.lifeMu.Unlock()
		owner.invocationState.Store(instanceInvocationClosed)
		owner.closeState.Store(state)
		t.Cleanup(func() {
			owner.lifeMu.Lock()
			owner.closed = false
			owner.finalizing = false
			owner.resourcesClosed = false
			owner.constructionActive = false
			owner.lifeMu.Unlock()
			owner.invocationState.Store(0)
			owner.closeState.Store(nil)
		})
	}
}

func dependencyTestInvocationLease(members ...*dependencyMember) {
	for _, member := range members {
		member.owner.invocationState.Store(1)
	}
}

func dependencyTestAcquireForFinalization(t *testing.T, g *dependencyGraph, member *dependencyMember) dependencyReceipt {
	t.Helper()
	receipt, err := g.acquirePending(member, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { receipt.release() })
	return receipt
}

func TestDependencyClosedComponentDiscovery(t *testing.T) {
	g := dependencyTestGraph(t, 4, 4)
	a := dependencyTestMember(t, g, false)
	b := dependencyTestMember(t, g, false)
	downstream := dependencyTestMember(t, g, false)
	disconnected := dependencyTestMember(t, g, false)
	dependencyTestInvocationLease(a, b)
	aToB := dependencyTestAcquire(t, g, b)
	bToA := dependencyTestAcquire(t, g, a)
	aToDownstream := dependencyTestAcquire(t, g, downstream)
	if typed, err := aToB.adopt(a, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	if typed, err := bToA.adopt(b, dependencyMemory); !typed || err != nil {
		t.Fatal(typed, err)
	}
	if typed, err := aToDownstream.adopt(a, dependencyTableStorage); !typed || err != nil {
		t.Fatal(typed, err)
	}
	dependencyTestClosedForDiscovery(t, a, b, downstream, disconnected)

	result := g.discoverClosedComponent(a, 4, 4)
	if result.status != dependencyComponentCandidate || len(result.members) != 2 || result.members[0] != a || result.members[1] != b || result.internalReceipts != 2 {
		t.Fatalf("closed component = %#v", result)
	}
	if result := g.discoverClosedComponent(disconnected, 4, 4); result.status != dependencyComponentBlocked || len(result.members) != 0 || result.internalReceipts != 0 {
		t.Fatalf("zero-edge singleton = %#v", result)
	}
	dependencyTestCounts(t, g, a, 1, 1, 0, 1)
	dependencyTestCounts(t, g, b, 1, 1, 0, 1)
	dependencyTestCounts(t, g, downstream, 1, 1, 0, 1)
}

func TestDependencyClosedComponentDiscoveryBlockers(t *testing.T) {
	newCycle := func(t *testing.T) (*dependencyGraph, *dependencyMember, *dependencyMember) {
		t.Helper()
		g := dependencyTestGraph(t, 3, 4)
		a := dependencyTestMember(t, g, false)
		b := dependencyTestMember(t, g, false)
		dependencyTestInvocationLease(a, b)
		aToB := dependencyTestAcquire(t, g, b)
		bToA := dependencyTestAcquire(t, g, a)
		if typed, err := aToB.adopt(a, dependencyFunction); !typed || err != nil {
			t.Fatal(typed, err)
		}
		if typed, err := bToA.adopt(b, dependencyMemory); !typed || err != nil {
			t.Fatal(typed, err)
		}
		dependencyTestClosedForDiscovery(t, a, b)
		return g, a, b
	}
	t.Run("outside typed incoming", func(t *testing.T) {
		g, a, _ := newCycle(t)
		outside := dependencyTestMember(t, g, false)
		dependencyTestInvocationLease(outside)
		receipt := dependencyTestAcquireForFinalization(t, g, a)
		if typed, err := receipt.adopt(outside, dependencyFrozenDescriptor); !typed || err != nil {
			t.Fatal(typed, err)
		}
		if result := g.discoverClosedComponent(a, 3, 4); result.status != dependencyComponentBlocked || result.members != nil {
			t.Fatalf("outside incoming = %#v", result)
		}
	})
	t.Run("pending external", func(t *testing.T) {
		g, a, _ := newCycle(t)
		_ = dependencyTestAcquireForFinalization(t, g, a)
		if result := g.discoverClosedComponent(a, 3, 4); result.status != dependencyComponentBlocked || result.members != nil {
			t.Fatalf("pending external = %#v", result)
		}
	})
	t.Run("anonymous physical root", func(t *testing.T) {
		g, a, _ := newCycle(t)
		if !a.owner.retainResourceRootMode(true) {
			t.Fatal("anonymous root rejected")
		}
		t.Cleanup(a.owner.releaseResourceRoot)
		if result := g.discoverClosedComponent(a, 3, 4); result.status != dependencyComponentBlocked || result.members != nil {
			t.Fatalf("anonymous root = %#v", result)
		}
	})
	t.Run("lifecycle", func(t *testing.T) {
		for _, mutate := range []func(*Instance){
			func(in *Instance) { in.closed = false },
			func(in *Instance) { in.finalizing = true },
			func(in *Instance) { in.resourcesClosed = true },
			func(in *Instance) { in.constructionActive = true },
			func(in *Instance) { in.invocationState.Store(instanceInvocationClosed | 1) },
			func(in *Instance) { in.closeState.Load().prepared.Store(false) },
			func(in *Instance) { in.closeState.Load().completed.Store(false) },
			func(in *Instance) { in.closeState.Load().terminalComplete.Store(false) },
		} {
			g, a, _ := newCycle(t)
			a.owner.lifeMu.Lock()
			mutate(a.owner)
			a.owner.lifeMu.Unlock()
			if result := g.discoverClosedComponent(a, 3, 4); result.status != dependencyComponentBlocked || result.members != nil {
				t.Fatalf("lifecycle blocker = %#v", result)
			}
		}
	})
}

func TestDependencyClosedComponentDiscoveryChargesReceiptHistory(t *testing.T) {
	g := dependencyTestGraph(t, 2, 4)
	a := dependencyTestMember(t, g, false)
	b := dependencyTestMember(t, g, false)
	dependencyTestInvocationLease(a, b)
	for _, endpoints := range [][2]*dependencyMember{{a, b}, {b, a}} {
		receipt := dependencyTestAcquire(t, g, endpoints[1])
		if typed, err := receipt.adopt(endpoints[0], dependencyFunction); !typed || err != nil {
			t.Fatal(typed, err)
		}
	}
	first := dependencyTestAcquire(t, g, a)
	second := dependencyTestAcquire(t, g, b)
	if !first.release() || !second.release() || len(g.records) != 4 {
		t.Fatal("failed to establish released receipt history")
	}
	dependencyTestClosedForDiscovery(t, a, b)
	if result := g.discoverClosedComponent(a, 2, 2); result.status != dependencyComponentIncomplete || result.members != nil {
		t.Fatalf("unbudgeted history = %#v", result)
	}
	if result := g.discoverClosedComponent(a, 2, 4); result.status != dependencyComponentCandidate || len(result.members) != 2 || result.internalReceipts != 2 {
		t.Fatalf("budgeted history = %#v", result)
	}
}

func TestDependencyClosedComponentDiscoverySelfLoopMultiplicity(t *testing.T) {
	g := dependencyTestGraph(t, 1, 2)
	member := dependencyTestMember(t, g, false)
	dependencyTestInvocationLease(member)
	for i := 0; i < 2; i++ {
		receipt := dependencyTestAcquire(t, g, member)
		if typed, err := receipt.adopt(member, dependencyFunction); !typed || err != nil {
			t.Fatal(typed, err)
		}
	}
	dependencyTestClosedForDiscovery(t, member)
	result := g.discoverClosedComponent(member, 1, 2)
	if result.status != dependencyComponentCandidate || len(result.members) != 1 || result.members[0] != member || result.internalReceipts != 2 {
		t.Fatalf("parallel self receipts = %#v", result)
	}
	dependencyTestCounts(t, g, member, 2, 2, 0, 2)
}

func TestDependencyClosedComponentDiscoveryReleasingBarrier(t *testing.T) {
	g := dependencyTestGraph(t, 2, 3)
	a := dependencyTestMember(t, g, false)
	b := dependencyTestMember(t, g, false)
	dependencyTestInvocationLease(a, b)
	for _, endpoints := range [][2]*dependencyMember{{a, b}, {b, a}} {
		receipt := dependencyTestAcquire(t, g, endpoints[1])
		if typed, err := receipt.adopt(endpoints[0], dependencyFunction); !typed || err != nil {
			t.Fatal(typed, err)
		}
	}
	external := dependencyTestAcquire(t, g, a)
	dependencyTestClosedForDiscovery(t, a, b)
	started, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	g.afterRelease = func(owner *Instance) {
		if owner == a.owner {
			once.Do(func() {
				close(started)
				<-proceed
			})
		}
	}
	type releaseResult struct {
		released bool
		panic    any
	}
	done := make(chan releaseResult, 1)
	go func() {
		result := releaseResult{}
		defer func() {
			result.panic = recover()
			done <- result
		}()
		result.released = external.release()
	}()
	var proceedOnce sync.Once
	unblock := func() { proceedOnce.Do(func() { close(proceed) }) }
	joined := false
	t.Cleanup(func() {
		unblock()
		if !joined {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("timed out joining releasing receipt worker")
			}
		}
	})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out entering releasing receipt callback")
	}
	if result := g.discoverClosedComponent(a, 2, 3); result.status != dependencyComponentBlocked || result.members != nil || result.internalReceipts != 0 {
		t.Fatalf("releasing external receipt = %#v", result)
	}
	unblock()
	var released releaseResult
	select {
	case released = <-done:
		joined = true
	case <-time.After(5 * time.Second):
		t.Fatal("timed out completing releasing receipt callback")
	}
	if released.panic != nil || !released.released {
		t.Fatal("releasing receipt lost its physical decrement")
	}
	result := g.discoverClosedComponent(a, 2, 3)
	if result.status != dependencyComponentCandidate || len(result.members) != 2 || result.internalReceipts != 2 {
		t.Fatalf("component after release = %#v", result)
	}
}

func TestDependencyClosedComponentDiscoveryBoundsAndAuthority(t *testing.T) {
	g := dependencyTestGraph(t, 3, 3)
	a := dependencyTestMember(t, g, false)
	b := dependencyTestMember(t, g, false)
	c := dependencyTestMember(t, g, false)
	dependencyTestInvocationLease(a, b, c)
	for _, endpoints := range [][2]*dependencyMember{{a, b}, {b, a}, {a, c}} {
		receipt := dependencyTestAcquire(t, g, endpoints[1])
		if typed, err := receipt.adopt(endpoints[0], dependencyContainerDescriptor); !typed || err != nil {
			t.Fatal(typed, err)
		}
	}
	dependencyTestClosedForDiscovery(t, a, b, c)
	for _, tc := range []struct {
		nodes, edges int
	}{{0, 3}, {3, 0}, {1, 3}, {3, 2}} {
		result := g.discoverClosedComponent(a, tc.nodes, tc.edges)
		want := dependencyComponentIncomplete
		if result.status != want || result.members != nil || result.internalReceipts != 0 {
			t.Fatalf("budgets %d/%d = %#v, want status %d", tc.nodes, tc.edges, result, want)
		}
	}
	foreign := dependencyTestGraph(t, 1, 1)
	foreignMember := dependencyTestMember(t, foreign, false)
	if result := g.discoverClosedComponent(foreignMember, 3, 3); result.status != dependencyComponentBlocked || result.members != nil {
		t.Fatalf("foreign authority = %#v", result)
	}
	dependencyTestCounts(t, g, a, 1, 1, 0, 1)
	dependencyTestCounts(t, g, b, 1, 1, 0, 1)
	dependencyTestCounts(t, g, c, 1, 1, 0, 1)
}

func dependencyTestRevalidationCycle(t *testing.T) (*dependencyGraph, *dependencyMember, *dependencyMember, dependencyReceipt) {
	t.Helper()
	g := dependencyTestGraph(t, 3, 4)
	a := dependencyTestMember(t, g, false)
	b := dependencyTestMember(t, g, false)
	downstream := dependencyTestMember(t, g, false)
	dependencyTestInvocationLease(a, b)
	for _, endpoints := range [][2]*dependencyMember{{a, b}, {b, a}} {
		receipt := dependencyTestAcquire(t, g, endpoints[1])
		if typed, err := receipt.adopt(endpoints[0], dependencyFunction); !typed || err != nil {
			t.Fatal(typed, err)
		}
	}
	outbound := dependencyTestAcquire(t, g, downstream)
	if typed, err := outbound.adopt(a, dependencyTableStorage); !typed || err != nil {
		t.Fatal(typed, err)
	}
	dependencyTestClosedForDiscovery(t, a, b)
	return g, a, downstream, outbound
}

func TestDependencyClosedComponentExactRevalidation(t *testing.T) {
	t.Run("unchanged", func(t *testing.T) {
		g, a, _, _ := dependencyTestRevalidationCycle(t)
		candidate := g.discoverClosedComponent(a, 3, 4)
		if candidate.status != dependencyComponentCandidate || !g.revalidateClosedComponent(candidate) {
			t.Fatalf("unchanged candidate did not revalidate: %#v", candidate)
		}
		foreign := dependencyTestGraph(t, 1, 1)
		if foreign.revalidateClosedComponent(candidate) {
			t.Fatal("foreign graph revalidated candidate")
		}
		malformed := candidate
		malformed.internalReceipts = malformed.receiptSlots + 1
		malformed.receiptIdentities = make([]uint64, malformed.internalReceipts)
		if g.revalidateClosedComponent(malformed) {
			t.Fatal("receipt cardinality above captured extent revalidated")
		}
		malformed = candidate
		malformed.members = append(append([]*dependencyMember{}, malformed.members...), a, a)
		malformed.memberIdentities = append(append([]uint64{}, malformed.memberIdentities...), a.identity, a.identity)
		malformed.memberOutgoing = append(append([]uint32{}, malformed.memberOutgoing...), 0, 0)
		if g.revalidateClosedComponent(malformed) {
			t.Fatal("member cardinality above graph cap revalidated")
		}
		candidate.receiptIdentities[0]++
		if g.revalidateClosedComponent(candidate) {
			t.Fatal("changed receipt identity revalidated")
		}
	})
	t.Run("outgoing topology", func(t *testing.T) {
		g, a, _, outbound := dependencyTestRevalidationCycle(t)
		candidate := g.discoverClosedComponent(a, 3, 4)
		if !g.revalidateClosedComponent(candidate) || !outbound.release() {
			t.Fatal("failed to establish outgoing release")
		}
		if g.revalidateClosedComponent(candidate) {
			t.Fatal("changed outgoing topology revalidated")
		}
	})
	t.Run("receipt extent", func(t *testing.T) {
		g, a, downstream, _ := dependencyTestRevalidationCycle(t)
		candidate := g.discoverClosedComponent(a, 3, 4)
		if !g.revalidateClosedComponent(candidate) {
			t.Fatal("initial candidate did not revalidate")
		}
		_ = dependencyTestAcquire(t, g, downstream)
		if g.revalidateClosedComponent(candidate) {
			t.Fatal("grown receipt extent revalidated")
		}
	})
	t.Run("root count", func(t *testing.T) {
		g, a, _, _ := dependencyTestRevalidationCycle(t)
		candidate := g.discoverClosedComponent(a, 3, 4)
		if !a.owner.retainResourceRootMode(true) {
			t.Fatal("failed to retain anonymous root")
		}
		t.Cleanup(a.owner.releaseResourceRoot)
		if g.revalidateClosedComponent(candidate) {
			t.Fatal("changed root count revalidated")
		}
	})
	t.Run("lifecycle", func(t *testing.T) {
		g, a, _, _ := dependencyTestRevalidationCycle(t)
		candidate := g.discoverClosedComponent(a, 3, 4)
		a.owner.lifeMu.Lock()
		a.owner.finalizing = true
		a.owner.lifeMu.Unlock()
		if g.revalidateClosedComponent(candidate) {
			t.Fatal("changed lifecycle revalidated")
		}
	})
	t.Run("noncandidate", func(t *testing.T) {
		g := dependencyTestGraph(t, 1, 1)
		member := dependencyTestMember(t, g, false)
		if g.revalidateClosedComponent(g.discoverClosedComponent(member, 1, 1)) {
			t.Fatal("blocked discovery revalidated")
		}
	})
}

func TestDependencyClosedComponentRevalidationOutgoingBarrier(t *testing.T) {
	g, a, downstream, outbound := dependencyTestRevalidationCycle(t)
	candidate := g.discoverClosedComponent(a, 3, 4)
	if !g.revalidateClosedComponent(candidate) {
		t.Fatal("initial candidate did not revalidate")
	}
	started, proceed := make(chan struct{}), make(chan struct{})
	var callbackOnce sync.Once
	g.afterRelease = func(owner *Instance) {
		if owner == downstream.owner {
			callbackOnce.Do(func() {
				close(started)
				<-proceed
			})
		}
	}
	type releaseResult struct {
		released bool
		panic    any
	}
	done := make(chan releaseResult, 1)
	go func() {
		result := releaseResult{}
		defer func() {
			result.panic = recover()
			done <- result
		}()
		result.released = outbound.release()
	}()
	var proceedOnce sync.Once
	unblock := func() { proceedOnce.Do(func() { close(proceed) }) }
	joined := false
	t.Cleanup(func() {
		unblock()
		if !joined {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("timed out joining outbound release worker")
			}
		}
	})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out entering outbound release callback")
	}
	if g.revalidateClosedComponent(candidate) {
		t.Fatal("in-flight outbound release revalidated stale candidate")
	}
	if result := g.discoverClosedComponent(a, 3, 4); result.status != dependencyComponentBlocked {
		t.Fatalf("in-flight outbound release discovered candidate: %#v", result)
	}
	unblock()
	var released releaseResult
	select {
	case released = <-done:
		joined = true
	case <-time.After(5 * time.Second):
		t.Fatal("timed out completing outbound release callback")
	}
	if released.panic != nil || !released.released {
		t.Fatalf("outbound release result = %+v", released)
	}
	if g.revalidateClosedComponent(candidate) {
		t.Fatal("completed outbound release revalidated stale candidate")
	}
	fresh := g.discoverClosedComponent(a, 3, 4)
	if fresh.status != dependencyComponentCandidate || !g.revalidateClosedComponent(fresh) {
		t.Fatalf("fresh component after outbound release = %#v", fresh)
	}
}

func TestDependencyClosedComponentClaimRollback(t *testing.T) {
	g := dependencyTestGraph(t, 1, 1)
	member := dependencyTestMember(t, g, false)
	dependencyTestInvocationLease(member)
	receipt := dependencyTestAcquire(t, g, member)
	if typed, err := receipt.adopt(member, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	dependencyTestClosedForDiscovery(t, member)
	candidate := g.discoverClosedComponent(member, 1, 1)
	claim, ok := g.claimClosedComponent(candidate)
	if !ok || claim == nil || !claim.active.Load() || !member.collecting.Load() {
		t.Fatal("failed to claim closed self-component")
	}
	t.Cleanup(func() { claim.release() })
	dependencyTestCounts(t, g, member, 2, 1, 0, 1)
	if duplicate, ok := g.claimClosedComponent(candidate); ok || duplicate != nil {
		t.Fatal("duplicate component claim succeeded")
	}
	if result := g.discoverClosedComponent(member, 1, 1); result.status != dependencyComponentBlocked {
		t.Fatalf("claimed component rediscovered: %#v", result)
	}
	if _, err := g.acquirePending(member, true); !errors.Is(err, errDependencyCollecting) {
		t.Fatalf("claimed target acquired another receipt: %v", err)
	}
	if !claim.release() || claim.release() || claim.active.Load() || member.collecting.Load() {
		t.Fatal("component claim did not roll back exactly once")
	}
	dependencyTestCounts(t, g, member, 1, 1, 0, 1)
	if !g.revalidateClosedComponent(candidate) {
		t.Fatal("rolled-back component did not restore exact candidate")
	}
}

func TestDependencyClosedComponentConcurrentClaimAndRollback(t *testing.T) {
	g := dependencyTestGraph(t, 1, 1)
	member := dependencyTestMember(t, g, false)
	dependencyTestInvocationLease(member)
	receipt := dependencyTestAcquire(t, g, member)
	if typed, err := receipt.adopt(member, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	dependencyTestClosedForDiscovery(t, member)
	candidate := g.discoverClosedComponent(member, 1, 1)
	start := make(chan struct{})
	type claimResult struct {
		claim *dependencyComponentClaim
		panic any
	}
	results := make(chan claimResult, 2)
	for i := 0; i < 2; i++ {
		go func() {
			result := claimResult{}
			defer func() {
				result.panic = recover()
				results <- result
			}()
			<-start
			result.claim, _ = g.claimClosedComponent(candidate)
		}()
	}
	close(start)
	var acquired []*dependencyComponentClaim
	for i := 0; i < 2; i++ {
		select {
		case result := <-results:
			if result.panic != nil {
				t.Fatalf("concurrent claim panic: %v", result.panic)
			}
			if result.claim != nil {
				acquired = append(acquired, result.claim)
				t.Cleanup(func() { result.claim.release() })
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out joining concurrent claim worker")
		}
	}
	if len(acquired) != 1 {
		t.Fatalf("concurrent successful claims = %d", len(acquired))
	}
	claim := acquired[0]
	type rollbackResult struct {
		released bool
		panic    any
	}
	released := make(chan rollbackResult, 2)
	for i := 0; i < 2; i++ {
		go func() {
			result := rollbackResult{}
			defer func() {
				result.panic = recover()
				released <- result
			}()
			result.released = claim.release()
		}()
	}
	var rollback [2]rollbackResult
	for i := range rollback {
		select {
		case rollback[i] = <-released:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out joining concurrent rollback worker")
		}
	}
	if rollback[0].panic != nil || rollback[1].panic != nil || rollback[0].released == rollback[1].released {
		t.Fatalf("concurrent claim releases = %+v", rollback)
	}
	dependencyTestCounts(t, g, member, 1, 1, 0, 1)
}

func TestDependencyClosedComponentRollbackContinuesFinalizationAfterPanic(t *testing.T) {
	g := dependencyTestGraph(t, 2, 2)
	a := &dependencyMember{owner: &Instance{c: &Compiled{}}}
	b := &dependencyMember{owner: &Instance{c: &Compiled{}}}
	if err := g.register(a); err != nil {
		t.Fatal(err)
	}
	if err := g.register(b); err != nil {
		t.Fatal(err)
	}
	dependencyTestInvocationLease(a, b)
	aToB := dependencyTestAcquire(t, g, b)
	bToA := dependencyTestAcquire(t, g, a)
	if typed, err := aToB.adopt(a, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	if typed, err := bToA.adopt(b, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	dependencyTestClosedForDiscovery(t, a, b)
	first := new(int)
	var visited []int
	a.owner.referenceLifetime().afterPhysicalRelease(func() { visited = append(visited, 0); panic(first) })
	b.owner.referenceLifetime().afterPhysicalRelease(func() { visited = append(visited, 1) })
	claim, ok := g.claimClosedComponent(g.discoverClosedComponent(a, 2, 2))
	if !ok {
		t.Fatal("component claim failed")
	}
	t.Cleanup(func() { claim.release() })
	if !aToB.release() || !bToA.release() {
		t.Fatal("failed to drain internal cycle receipts under guards")
	}
	value, panicked := cleanupTestPanic(func() { claim.release() })
	if !panicked || value != first || !reflect.DeepEqual(visited, []int{0, 1}) {
		t.Fatalf("rollback panic=%v/%t callbacks=%v", value, panicked, visited)
	}
	if a.owner.resourceRefs != 0 || b.owner.resourceRefs != 0 || !a.owner.resourcesClosed || !b.owner.resourcesClosed || a.collecting.Load() || b.collecting.Load() || claim.active.Load() {
		t.Fatal("component guards or finalization state remained after panic")
	}
}

func TestDependencyClosedComponentPhysicalClaimAndCompletion(t *testing.T) {
	g := dependencyTestGraph(t, 2, 2)
	a := dependencyTestMember(t, g, false)
	b := dependencyTestMember(t, g, false)
	dependencyTestInvocationLease(a, b)
	aToB := dependencyTestAcquire(t, g, b)
	bToA := dependencyTestAcquire(t, g, a)
	if typed, err := aToB.adopt(a, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	if typed, err := bToA.adopt(b, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	dependencyTestClosedForDiscovery(t, a, b)
	firstPanic, directPanic := new(int), new(int)
	var visited []int
	first := func() { visited = append(visited, 0); panic(firstPanic) }
	second := func() { visited = append(visited, 1) }
	a.owner.lifeMu.Lock()
	a.owner.finalizers = &instanceFinalizers{physical: first}
	a.owner.lifeMu.Unlock()
	b.owner.lifeMu.Lock()
	b.owner.finalizers = &instanceFinalizers{physical: second}
	b.owner.lifeMu.Unlock()

	release, ok := g.claimClosedComponentForPhysicalRelease(g.discoverClosedComponent(a, 2, 2))
	if !ok || release == nil || release.claim == nil || len(release.finalizers) != 2 {
		t.Fatal("failed to claim component for physical release")
	}
	if !a.owner.resourcesClosed || !b.owner.resourcesClosed || a.owner.finalizers != nil || b.owner.finalizers != nil || release.finalizers[0].physical == nil || release.finalizers[1].physical == nil {
		t.Fatal("physical claim did not atomically detach member finalizers")
	}
	dependencyTestCounts(t, g, a, 2, 1, 0, 1)
	dependencyTestCounts(t, g, b, 2, 1, 0, 1)
	if release.complete() {
		t.Fatal("physical claim completed with live internal receipts")
	}
	if _, panicked := cleanupTestPanic(func() { release.claim.release() }); !panicked || !release.claim.active.Load() {
		t.Fatal("irreversible physical claim allowed rollback or lost retry authority")
	}
	if value, panicked := cleanupTestPanic(func() {
		release.runPhysicalJournal(0, func(*resourceCleanup) { panic(directPanic) })
	}); !panicked || value != directPanic || release.physicalJournals[0].Load() != 0 {
		t.Fatal("direct journal abort did not preserve retry authority")
	}
	var cleanup resourceCleanup
	runningJournalBlockedCompletion := false
	cleanup.run(func() {
		release.runPhysicalJournal(0, func(memberCleanup *resourceCleanup) {
			memberCleanup.run(func() {
				if !aToB.release() {
					panic("failed to drain first internal receipt")
				}
			})
		})
	})
	cleanup.run(func() {
		if !release.runPhysicalJournal(1, func(memberCleanup *resourceCleanup) {
			memberCleanup.run(func() {
				if !bToA.release() {
					panic("failed to drain second internal receipt")
				}
			})
			dependencyTestCounts(t, g, a, 1, 0, 0, 0)
			dependencyTestCounts(t, g, b, 1, 0, 0, 0)
			if release.complete() {
				panic("physical claim completed while a journal was running")
			}
			runningJournalBlockedCompletion = true
		}) {
			panic("failed to run second physical journal")
		}
	})
	if !reflect.DeepEqual(visited, []int{0, 1}) || release.physicalJournals[0].Load() != 2 || release.physicalJournals[1].Load() != 2 {
		t.Fatalf("physical journal callbacks=%v states=%d/%d", visited, release.physicalJournals[0].Load(), release.physicalJournals[1].Load())
	}
	callbackReplayed := false
	if !runningJournalBlockedCompletion || release.runPhysicalJournal(0, func(*resourceCleanup) { callbackReplayed = true }) || callbackReplayed {
		t.Fatal("running/completed journal gate did not prevent completion or callback replay")
	}
	if release.finalizers[0] != nil || release.finalizers[1] != nil {
		t.Fatal("completed journals retained detached finalizers")
	}
	if err := g.unregister(a); !errors.Is(err, errDependencyState) {
		t.Fatalf("collecting member unregistered before guard completion: %v", err)
	}
	if !release.complete() || release.complete() {
		t.Fatal("physical claim did not complete exactly once")
	}
	dependencyTestCounts(t, g, a, 0, 0, 0, 0)
	dependencyTestCounts(t, g, b, 0, 0, 0, 0)
	if a.collecting.Load() || b.collecting.Load() || release.claim.active.Load() {
		t.Fatal("physical claim guards remained after completion")
	}
	if value, panicked := cleanupTestPanic(cleanup.finish); !panicked || value != firstPanic {
		t.Fatalf("physical journal panic = %v/%t", value, panicked)
	}
}
