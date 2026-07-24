package wago

import (
	"strings"
	"sync"
	"testing"
)

func newReferenceStoreStateTest(t *testing.T) (*referenceStore, *Instance, *Instance) {
	t.Helper()
	store := newReferenceStore(false)
	in := &Instance{}
	if err := store.registerInstance(in); err != nil {
		t.Fatalf("registerInstance: %v", err)
	}
	owner := &Instance{resourcesClosed: true, resourceRefs: 1}
	entry := &funcrefTokenEntry{token: 1, owner: owner, descriptor: 1}
	store.byToken = map[uint64]*funcrefTokenEntry{1: entry}
	store.byIdentity = map[funcrefIdentity]*funcrefTokenEntry{{descriptor: 1}: entry}
	return store, in, owner
}

func assertReferenceStoreStateFinal(t *testing.T, store *referenceStore, owner *Instance) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.liveInstances != 0 {
		t.Fatalf("liveInstances = %d, want 0", store.liveInstances)
	}
	if len(store.instances) != 0 {
		t.Fatalf("stale instance membership remains: %#v", store.instances)
	}
	if len(store.byToken) != 0 || len(store.byIdentity) != 0 {
		t.Fatal("token maps remain after every release condition completed")
	}
	if owner.resourceRefs != 0 {
		t.Fatalf("released token owner roots = %d, want 0", owner.resourceRefs)
	}
}

func TestReferenceStoreCloseAccountingOrderIndependent(t *testing.T) {
	type transition struct {
		name string
		run  func(*referenceStore, *Instance)
	}
	logical := transition{"logical", (*referenceStore).instanceClosed}
	quiesced := transition{"quiesced", (*referenceStore).instanceQuiesced}
	physical := transition{"physical", (*referenceStore).resourceOwnerReleased}
	orders := [][]transition{
		{logical, quiesced, physical},
		{logical, physical, quiesced},
		{physical, logical, quiesced},
		{physical, quiesced, logical},
		{quiesced, logical, physical},
		{quiesced, physical, logical},
	}
	for _, runtimeFirst := range []bool{true, false} {
		for _, order := range orders {
			name := order[0].name + "-" + order[1].name + "-" + order[2].name
			if runtimeFirst {
				name = "runtime-first/" + name
			} else {
				name = "runtime-last/" + name
			}
			t.Run(name, func(t *testing.T) {
				store, in, owner := newReferenceStoreStateTest(t)
				if runtimeFirst {
					store.closeRuntime()
				}
				seenLogical, seenQuiesced := false, false
				for _, step := range order {
					step.run(store, in)
					step.run(store, in) // every notification is idempotent
					seenLogical = seenLogical || step.name == "logical"
					seenQuiesced = seenQuiesced || step.name == "quiesced"
					store.mu.Lock()
					tokens := len(store.byToken)
					live := store.liveInstances
					store.mu.Unlock()
					if runtimeFirst && (!seenLogical || !seenQuiesced) && tokens != 1 {
						t.Fatalf("tokens released before logical close and quiescence after %s", step.name)
					}
					if live > 1 {
						t.Fatalf("liveInstances underflow/overflow = %d", live)
					}
				}
				if !runtimeFirst {
					store.mu.Lock()
					if len(store.byToken) != 1 {
						t.Fatal("tokens released before Runtime.Close")
					}
					store.mu.Unlock()
					store.closeRuntime()
				}
				assertReferenceStoreStateFinal(t, store, owner)
			})
		}
	}
}

func TestReferenceStoreAbortRegisteredInstanceIsTerminalAndIdempotent(t *testing.T) {
	store, in, owner := newReferenceStoreStateTest(t)
	store.closeRuntime()
	store.abortRegisteredInstance(in)
	store.abortRegisteredInstance(in)
	assertReferenceStoreStateFinal(t, store, owner)
}

func TestReferenceStoreCloseAccountingConcurrentNotifications(t *testing.T) {
	store, in, owner := newReferenceStoreStateTest(t)
	store.closeRuntime()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, notify := range []func(){
		func() { store.instanceClosed(in) },
		func() { store.instanceQuiesced(in) },
		func() { store.resourceOwnerReleased(in) },
		func() { store.instanceClosed(in) },
		func() { store.instanceQuiesced(in) },
		func() { store.resourceOwnerReleased(in) },
	} {
		wg.Add(1)
		go func(fn func()) {
			defer wg.Done()
			<-start
			fn()
		}(notify)
	}
	close(start)
	wg.Wait()
	assertReferenceStoreStateFinal(t, store, owner)
}

func TestInvocationLeaseStateMachine(t *testing.T) {
	t.Run("entry then close publication", func(t *testing.T) {
		in := &Instance{resourcesClosed: true}
		if err := in.beginInvocation(); err != nil {
			t.Fatalf("beginInvocation: %v", err)
		}
		if got := in.invocationState.Load(); got != 1 {
			t.Fatalf("state after entry = %#x, want 1", got)
		}
		if previous := in.closeInvocationEntry(); previous != 1 {
			t.Fatalf("close previous state = %#x, want 1", previous)
		}
		if got := in.invocationState.Load(); got != instanceInvocationClosed|1 {
			t.Fatalf("published close state = %#x, want CLOSED|1", got)
		}
	})

	t.Run("rejected post-close entry does not mutate count", func(t *testing.T) {
		in := &Instance{resourcesClosed: true}
		in.invocationState.Store(instanceInvocationClosed | 1)
		if err := in.beginInvocation(); err == nil || !strings.Contains(err.Error(), "closed") {
			t.Fatalf("beginInvocation error = %v, want closed", err)
		}
		if got := in.invocationState.Load(); got != instanceInvocationClosed|1 {
			t.Fatalf("rejected entry changed state to %#x, want CLOSED|1", got)
		}
		in.endInvocation()
		if got := in.invocationState.Load(); got != instanceInvocationClosed {
			t.Fatalf("final invocation release state = %#x, want CLOSED", got)
		}
	})

	t.Run("count overflow is rejected without publishing close", func(t *testing.T) {
		in := &Instance{resourcesClosed: true}
		in.invocationState.Store(instanceInvocationCount)
		if err := in.beginInvocation(); err == nil || !strings.Contains(err.Error(), "too many") {
			t.Fatalf("beginInvocation overflow error = %v, want explicit limit", err)
		}
		if got := in.invocationState.Load(); got != instanceInvocationCount {
			t.Fatalf("overflow rejection changed state to %#x, want %#x", got, instanceInvocationCount)
		}
	})
}
