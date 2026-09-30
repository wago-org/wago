//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestGCAdmissionOrderedCancellation(t *testing.T) {
	for _, dynamic := range []bool{false, true} {
		name := "static"
		if dynamic {
			name = "dynamic"
		}
		t.Run(name, func(t *testing.T) {
			var set gcInvocationDomainSet
			for i := 0; i < 6; i++ {
				domain := &gcStoreDomain{id: uint64(i + 1), collector: new(gc.Collector)}
				if i != 0 {
					domain.prev = set.at(i - 1)
					domain.prev.next = domain
				}
				set.add(domain)
			}
			in := &Instance{gc: set.at(0).collector}
			topology := &gcDomainTopology{first: set.at(0), last: set.at(5), n: 6}
			in.refStore = &referenceStore{gcDomains: topology, instances: map[*Instance]*referenceStoreInstance{
				in: {gcDomain: set.at(0), invocationDomains: &set, dynamicInvocationDomains: dynamic},
			}}
			if dynamic {
				in.executionFlags.Store(executionFlagDynamicGCDomain | executionFlagImportedGCDomain)
			}
			owner := newInvocationID()
			held := gcInvocationDomainView{local: set.at(4)}
			held.lock()
			held.claim(owner)
			defer func() { held.release(owner); held.unlock() }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				lease, err := in.lockGCInvocationContext(ctx, newInvocationID())
				if err == nil {
					lease.unlock()
				}
				done <- err
			}()
			awaitGCLifecycle(t, func() bool { return set.at(4).invocationMu.state.Load()&invocationGateWaiters != 0 })
			cancel()
			if err := <-done; err != context.Canceled {
				t.Fatalf("admission = %v", err)
			}
			if !held.ownedBy(owner) {
				t.Fatal("waiter changed the original owner")
			}
			if !topology.TryLock() {
				t.Fatal("waiter retained the topology read lease")
			}
			topology.Unlock()
			for i := 0; i < set.len(); i++ {
				if i == 4 {
					continue
				}
				domain := set.at(i)
				if domain.invocationMu.state.Load()&invocationGateHeld != 0 || domain.invocationOwner != 0 {
					t.Fatalf("domain %d remained occupied", i)
				}
			}
		})
	}
}

func TestGCAdmissionSameOwnerBorrowing(t *testing.T) {
	in := &Instance{gc: new(gc.Collector)}
	domain := &gcStoreDomain{collector: in.gc}
	in.refStore = &referenceStore{instances: map[*Instance]*referenceStoreInstance{in: {gcDomain: domain}}}
	owner := newInvocationID()
	held := in.lockGCInvocation(owner)
	defer held.unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	borrowed, err := in.lockGCInvocationContext(ctx, owner)
	if err != nil || borrowed.acquired {
		t.Fatalf("borrowed lease = %+v, %v", borrowed, err)
	}
	borrowed.unlock()
	if !in.ownsGCInvocation(owner) {
		t.Fatal("borrow released its owner's domain")
	}
	cancel()
	if _, err := in.lockGCInvocationContext(ctx, owner); err != context.Canceled {
		t.Fatalf("canceled borrow = %v", err)
	}
	if !in.ownsGCInvocation(owner) {
		t.Fatal("canceled borrow released its owner's domain")
	}
}

func TestGCTopologyAdmissionCancellation(t *testing.T) {
	for _, writer := range []bool{false, true} {
		name := "reader"
		if writer {
			name = "writer"
		}
		t.Run(name, func(t *testing.T) {
			var gate gcTopologyGate
			gate.Lock()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- gate.lockContext(ctx, writer) }()
			awaitGCLifecycle(t, func() bool { gate.mu.Lock(); defer gate.mu.Unlock(); return gate.changed != nil })
			cancel()
			if err := <-done; err != context.Canceled {
				t.Fatalf("topology admission = %v", err)
			}
			gate.mu.Lock()
			owned, pending, readers := gate.writer, gate.writers, gate.readers
			gate.mu.Unlock()
			gate.Unlock()
			if !owned || pending != 0 || readers != 0 {
				t.Fatalf("topology writer=%v pending=%d readers=%d", owned, pending, readers)
			}
			gate.RLock()
			if gate.TryLock() {
				t.Fatal("writer bypassed a reader")
			}
			gate.RUnlock()
			gate.Lock()
			if gate.TryLock() {
				t.Fatal("writer bypassed a writer")
			}
			gate.Unlock()
		})
	}
}

func TestGCAdmissionUncontendedAllocations(t *testing.T) {
	in := &Instance{gc: new(gc.Collector)}
	domain := &gcStoreDomain{collector: in.gc}
	in.refStore = &referenceStore{instances: map[*Instance]*referenceStoreInstance{in: {gcDomain: domain}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, ctx := range []context.Context{nil, context.Background(), ctx} {
		allocations := testing.AllocsPerRun(100, func() {
			lease, err := in.lockGCInvocationContext(ctx, newInvocationID())
			if err != nil {
				panic(err)
			}
			lease.unlock()
		})
		if allocations != 0 {
			t.Fatalf("uncontended GC admission allocations = %v", allocations)
		}
	}
}

func TestGCInvocationNoDomainFastPathPreservesDynamicTopology(t *testing.T) {
	plain := &Instance{refStore: &referenceStore{}}
	if lease := plain.lockGCInvocation(newInvocationID()); lease.acquired || lease.topology != nil {
		t.Fatalf("plain no-domain admission acquired a lease: %+v", lease)
	}

	dynamic := &Instance{refStore: &referenceStore{gcDomains: &gcDomainTopology{}}}
	dynamic.executionFlags.Store(executionFlagDynamicGCDomain)
	lease := dynamic.lockGCInvocation(newInvocationID())
	if !lease.acquired || !lease.dynamic || lease.topology != dynamic.refStore.gcDomains {
		lease.unlock()
		t.Fatalf("dynamic empty-domain admission lost topology lease: %+v", lease)
	}
	lease.unlock()
}

func BenchmarkGCInvocationAdmission(b *testing.B) {
	for _, dynamic := range []bool{false, true} {
		name := "static"
		if dynamic {
			name = "dynamic"
		}
		b.Run(name, func(b *testing.B) {
			in := &Instance{gc: new(gc.Collector)}
			domain := &gcStoreDomain{collector: in.gc}
			in.refStore = &referenceStore{
				gcDomains: &gcDomainTopology{first: domain, last: domain, n: 1},
				instances: map[*Instance]*referenceStoreInstance{in: {gcDomain: domain, dynamicInvocationDomains: dynamic}},
			}
			if dynamic {
				in.executionFlags.Store(executionFlagDynamicGCDomain | executionFlagImportedGCDomain)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				lease := in.lockGCInvocation(newInvocationID())
				lease.unlock()
			}
		})
	}
}

func TestGCTopologyCanceledWriterUnblocksReaders(t *testing.T) {
	var gate gcTopologyGate
	gate.RLock()
	defer gate.RUnlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writerDone := make(chan error, 1)
	go func() { writerDone <- gate.lockContext(ctx, true) }()
	awaitGCLifecycle(t, func() bool { gate.mu.Lock(); defer gate.mu.Unlock(); return gate.writers == 1 })
	readerDone := make(chan struct{})
	go func() {
		gate.RLock()
		gate.mu.Lock()
		readers := gate.readers
		gate.mu.Unlock()
		if readers != 2 {
			t.Errorf("compatible readers = %d, want 2", readers)
		}
		gate.RUnlock()
		close(readerDone)
	}()
	cancel()
	if err := <-writerDone; err != context.Canceled {
		t.Fatalf("writer cancellation = %v", err)
	}
	select {
	case <-readerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("canceled writer blocked compatible readers")
	}
}
