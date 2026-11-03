//go:build (linux || (darwin && arm64)) && !riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"sync"
	"testing"
	"time"
)

func managedBoundaryReturns(call func()) bool {
	done := make(chan struct{})
	go func() {
		call()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(time.Second):
		return false
	}
}

func TestInactiveManagedOwnershipBoundariesAvoidRegistries(t *testing.T) {
	in := &Instance{}
	state := &instancePluginState{}

	managedFunctionMembers.mu.Lock()
	returned := managedBoundaryReturns(func() { managedFunctionBindInvocationState(in, state) })
	managedFunctionMembers.mu.Unlock()
	if !returned {
		t.Fatal("inactive invocation bind entered the member registry")
	}

	in.lifeMu.Lock()
	managedFunctionGraphs.mu.Lock()
	returned = managedBoundaryReturns(func() { managedFunctionAfterLifetimeBoundary(in) })
	managedFunctionGraphs.mu.Unlock()
	in.lifeMu.Unlock()
	if !returned {
		t.Fatal("inactive lifetime exit entered lifetime or graph state")
	}
}

func TestManagedOwnershipParticipationPublicationAndRetirement(t *testing.T) {
	store := &referenceStore{}
	in := &Instance{refStore: store}
	state := &instancePluginState{}
	in.pluginState.Store(state)
	managedFunctionBindInvocationState(in, state)
	if _, ok := managedFunctionInvocationStates.Load(state); ok {
		t.Fatal("inactive bind published an invocation graph")
	}
	g := &managedFunctionGraph{
		store:   store,
		members: make(map[*Instance]*managedFunctionMember),
	}

	g.mu.Lock()
	member := g.registerMemberLocked(in, true)
	g.mu.Unlock()
	t.Cleanup(func() {
		if in.executionFlags.Load()&executionFlagManagedOwnership != 0 {
			g.mu.Lock()
			g.removeMemberLocked(member)
			g.mu.Unlock()
		}
	})

	if in.executionFlags.Load()&executionFlagManagedOwnership == 0 {
		t.Fatal("registration did not publish managed ownership participation")
	}
	if value, ok := managedFunctionInvocationStates.Load(state); !ok || value != g {
		t.Fatal("registration did not publish invocation graph")
	}

	g.mu.Lock()
	g.removeMemberLocked(member)
	g.mu.Unlock()
	if in.executionFlags.Load()&executionFlagManagedOwnership != 0 {
		t.Fatal("retirement retained instance participation")
	}
	if _, ok := managedFunctionMembers.Load(in); ok {
		t.Fatal("retirement retained member publication")
	}
	if _, ok := managedFunctionInvocationStates.Load(state); ok {
		t.Fatal("retirement retained invocation graph publication")
	}
}

func TestManagedOwnershipBindLoadedBeforeRetirementDoesNotRepublish(t *testing.T) {
	store := &referenceStore{}
	in := &Instance{refStore: store}
	state := &instancePluginState{}
	in.pluginState.Store(state)
	g := &managedFunctionGraph{store: store, members: make(map[*Instance]*managedFunctionMember)}

	g.mu.Lock()
	member := g.registerMemberLocked(in, true)
	g.mu.Unlock()
	managedFunctionInvocationStates.Delete(state)

	// Reproduce the bind/retirement interleaving after bind has loaded its
	// member but before it publishes the invocation-state graph.
	g.mu.Lock()
	g.removeMemberLocked(member)
	g.mu.Unlock()
	managedFunctionBindMemberInvocationState(in, state, member)

	if _, ok := managedFunctionInvocationStates.Load(state); ok {
		t.Fatal("bind republished invocation graph after member retirement")
	}
}

func TestManagedOwnershipLateRegistrationConcurrentBind(t *testing.T) {
	for range 100 {
		store := &referenceStore{}
		in := &Instance{refStore: store}
		state := &instancePluginState{}
		in.pluginState.Store(state)
		g := &managedFunctionGraph{store: store, members: make(map[*Instance]*managedFunctionMember)}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			managedFunctionBindInvocationState(in, state)
		}()
		close(start)
		g.mu.Lock()
		member := g.registerMemberLocked(in, true)
		g.mu.Unlock()
		wg.Wait()
		if in.executionFlags.Load()&executionFlagManagedOwnership == 0 {
			t.Fatal("late registration was not visible to invocation state")
		}
		if value, ok := managedFunctionInvocationStates.Load(state); !ok || value != g {
			t.Fatal("late registration lost the invocation graph")
		}
		g.mu.Lock()
		g.removeMemberLocked(member)
		g.mu.Unlock()
	}
}
