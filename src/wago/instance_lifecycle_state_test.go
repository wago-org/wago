package wago

import (
	"strings"
	"sync"
	"testing"
)

func TestReferenceStoreCloseAccountingOrderIndependent(t *testing.T) {
	cases := []struct {
		name string
		run  func(*referenceStore, *Instance)
	}{
		{
			name: "logical then physical",
			run: func(store *referenceStore, in *Instance) {
				store.instanceClosed(in)
				store.instanceClosed(in)
				store.resourceOwnerReleased(in)
				store.resourceOwnerReleased(in)
			},
		},
		{
			name: "physical then logical",
			run: func(store *referenceStore, in *Instance) {
				store.resourceOwnerReleased(in)
				store.resourceOwnerReleased(in)
				store.instanceClosed(in)
				store.instanceClosed(in)
			},
		},
		{
			name: "concurrent",
			run: func(store *referenceStore, in *Instance) {
				start := make(chan struct{})
				var wg sync.WaitGroup
				wg.Add(2)
				go func() { defer wg.Done(); <-start; store.instanceClosed(in) }()
				go func() { defer wg.Done(); <-start; store.resourceOwnerReleased(in) }()
				close(start)
				wg.Wait()
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newReferenceStore(false)
			in := &Instance{}
			if err := store.registerInstance(in); err != nil {
				t.Fatalf("registerInstance: %v", err)
			}
			owner := &Instance{resourcesClosed: true, resourceRefs: 1}
			entry := &funcrefTokenEntry{token: 1, owner: owner, descriptor: 1}
			store.byToken = map[uint64]*funcrefTokenEntry{1: entry}
			store.byIdentity = map[funcrefIdentity]*funcrefTokenEntry{{descriptor: 1}: entry}
			store.closeRuntime() // token cleanup must wait for both instance transitions
			if len(store.byToken) != 1 {
				t.Fatal("runtime close released entries while a logical instance remained")
			}

			tc.run(store, in)
			if store.liveInstances != 0 {
				t.Fatalf("liveInstances = %d, want 0", store.liveInstances)
			}
			if len(store.instances) != 0 {
				t.Fatalf("instance membership remains after both transitions: %#v", store.instances)
			}
			if len(store.byToken) != 0 || len(store.byIdentity) != 0 {
				t.Fatal("runtime-close token cleanup did not run after the final transition")
			}
			if owner.resourceRefs != 0 {
				t.Fatalf("released token owner roots = %d, want 0", owner.resourceRefs)
			}
		})
	}
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
