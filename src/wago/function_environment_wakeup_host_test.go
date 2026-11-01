//go:build linux && amd64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func r9WakeupOwner(t *testing.T, shift uint8) (*Instance, *managedFunctionGraph, *Compiled) {
	return r9WakeupOwnerInStore(t, shift, newReferenceStore(false))
}

func r9WakeupOwnerInStore(t *testing.T, shift uint8, store *referenceStore) (*Instance, *managedFunctionGraph, *Compiled) {
	t.Helper()
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x70, 1}, []byte{0x60, 0, 0})),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("unused", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x0b}))))
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), source)
	if err != nil {
		t.Fatal(err)
	}
	in, err := instantiateCore(c, InstantiateOptions{store: store, GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, NurseryRegionShift: shift, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	domain := in.lockGCCollector()
	state := in.managedFunctionCollector(domain)
	ref, err := state.newManagedFunctionEnv(in, gc.EmptyRoots{})
	if err != nil {
		unlockGCCollector(domain)
		t.Fatal(err)
	}
	rooted := false
	for slot, value := range state.hostEnvSlots {
		got, rootErr := in.gc.CheckedGlobalSlot(slot)
		if value == ref && rootErr == nil && got == ref {
			rooted = true
		}
	}
	unlockGCCollector(domain)
	if !rooted || !in.hasPhysicalResources() {
		t.Fatal("STOP missing pre-contention checked owner root")
	}
	return in, state.graph, c
}

func r9RetryIdle(t *testing.T, g *managedFunctionGraph) <-chan struct{} {
	t.Helper()
	g.hostRetry.mu.Lock()
	defer g.hostRetry.mu.Unlock()
	if !g.hostRetry.running || g.hostRetry.idle == nil {
		t.Fatal("contended graph has no retry driver")
	}
	return g.hostRetry.idle
}

func r9WaitRetry(t *testing.T, idle <-chan struct{}) {
	t.Helper()
	select {
	case <-idle:
	case <-time.After(5 * time.Second):
		t.Fatal("retry driver did not exit after contention ended")
	}
}

func TestR9OwnershipWakeupContention(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		for _, blocker := range []string{"native", "topology", "domain", "revoked-domain", "driver", "foreign-runtime"} {
			t.Run(fmt.Sprintf("region=%d/%s", shift, blocker), func(t *testing.T) {
				for round := 0; round < 4; round++ {
					in, g, c := r9WakeupOwner(t, shift)
					store := in.refStore
					domain := in.gcInvocationDomain()
					var lock, unlock func()
					var other *Instance
					var otherCode *Compiled
					switch blocker {
					case "native":
						lock, unlock = nativeExecutionMu.Lock, nativeExecutionMu.Unlock
					case "topology":
						topology := store.ensureGCTopology()
						lock, unlock = topology.Lock, topology.Unlock
					case "domain":
						lock, unlock = domain.invocationMu.Lock, domain.invocationMu.Unlock
					case "revoked-domain":
						domain.invocationMu.revokeFast()
						lock, unlock = domain.invocationMu.Lock, domain.invocationMu.Unlock
					case "driver":
						lock, unlock = g.collecting.Lock, g.collecting.Unlock
					case "foreign-runtime":
						other, _, otherCode = r9WakeupOwner(t, shift)
						if other.refStore == store {
							t.Fatal("cross-Runtime control aliases source store")
						}
						lock = func() {
							mu := other.acquireInstanceNativeStateForHostAccess()
							if mu != &nativeExecutionMu {
								mu.Unlock()
								t.Fatal("control did not lease shared native mutex")
							}
							unlock = mu.Unlock
						}
					}
					released := make(chan error, 1)
					var calls atomic.Int32
					in.referenceLifetime().afterPhysicalRelease(func() {
						calls.Add(1)
						// Finalizers must run after all collection locks have been released.
						locks := []*sync.Mutex{&nativeExecutionMu, &g.mu, &g.collecting, &domain.mu}
						for _, mu := range locks {
							if !mu.TryLock() {
								released <- fmt.Errorf("callback under collection lock")
								return
							}
							mu.Unlock()
						}
						topology := store.ensureGCTopology()
						if !topology.TryLock() {
							released <- fmt.Errorf("callback under topology lease")
							return
						}
						topology.Unlock()
						released <- nil
					})
					// Logical Close itself publishes lifetime under the topology
					// writer. Close under native contention first, then switch the
					// blocker to topology before allowing the retry to progress.
					if blocker == "topology" {
						nativeExecutionMu.Lock()
					} else {
						lock()
					}
					if err := in.Close(); err != nil {
						if blocker == "topology" {
							nativeExecutionMu.Unlock()
						} else {
							unlock()
						}
						t.Fatal(err)
					}
					if blocker == "topology" {
						lock()
						nativeExecutionMu.Unlock()
					}
					if !in.hasPhysicalResources() || calls.Load() != 0 {
						unlock()
						t.Fatal("STOP owner released while contention retained floating root")
					}
					idle := r9RetryIdle(t, g)
					for request := 0; request < 8; request++ {
						g.pendingCollection.Store(true)
						g.drainAfterGate(0)
						if got := r9RetryIdle(t, g); got != idle {
							unlock()
							t.Fatal("coalesced contention started a second driver")
						}
					}
					unlock()
					select {
					case err := <-released:
						if err != nil {
							t.Fatal(err)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("owner was not released after contention without another invocation")
					}
					r9WaitRetry(t, idle)
					if in.hasPhysicalResources() || calls.Load() != 1 {
						t.Fatal("STOP physical release was not exact")
					}
					g.hostRetry.mu.Lock()
					running, pending := g.hostRetry.running, g.hostRetry.pending.present()
					g.hostRetry.mu.Unlock()
					if running || pending {
						t.Fatal("finished retry retained worker or queued work")
					}
					if _, ok := managedFunctionGraphs.Load(store); ok {
						t.Fatal("closed owner retained graph")
					}
					store.closeRuntime()
					c.Close()
					if other != nil {
						if !other.hasPhysicalResources() {
							t.Fatal("STOP unrelated Runtime owner was released")
						}
						other.Close()
						other.refStore.closeRuntime()
						otherCode.Close()
					}
				}
			})
		}
	}
}

func TestR9OwnershipWakeupActiveBoundary(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			in, g, c := r9WakeupOwner(t, shift)
			released := make(chan struct{})
			in.referenceLifetime().afterPhysicalRelease(func() { close(released) })
			if err := in.beginInvocation(); err != nil {
				t.Fatal(err)
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			g.hostRetry.mu.Lock()
			running := g.hostRetry.running
			g.hostRetry.mu.Unlock()
			if running || !in.hasPhysicalResources() {
				t.Fatal("STOP active invocation scheduled a quiescent retry or lost its owner")
			}
			nativeExecutionMu.Lock()
			in.endInvocation()
			if !in.hasPhysicalResources() {
				nativeExecutionMu.Unlock()
				t.Fatal("STOP owner lost before blocked outer boundary")
			}
			idle := r9RetryIdle(t, g)
			nativeExecutionMu.Unlock()
			select {
			case <-released:
			case <-time.After(5 * time.Second):
				t.Fatal("outer boundary did not release after contention")
			}
			r9WaitRetry(t, idle)
			in.refStore.closeRuntime()
			c.Close()
		})
	}
}

// Queue while a retry is in a physical callback, then queue again after that
// worker's idle notification. Both crossings must retain an independent wakeup.
func TestR9OwnershipWakeupExitCrossing(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			first, g, firstCode := r9WakeupOwner(t, shift)
			store := first.refStore
			anchor, _, anchorCode := r9WakeupOwnerInStore(t, shift, store)
			entered, leave := make(chan struct{}), make(chan struct{})
			first.referenceLifetime().afterPhysicalRelease(func() { close(entered); <-leave })
			nativeExecutionMu.Lock()
			if err := first.Close(); err != nil {
				nativeExecutionMu.Unlock()
				t.Fatal(err)
			}
			idle := r9RetryIdle(t, g)
			nativeExecutionMu.Unlock()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("retry callback not reached")
			}
			second, otherGraph, secondCode := r9WakeupOwnerInStore(t, shift, store)
			if otherGraph != g {
				t.Fatal("store graph changed during retry callback")
			}
			secondReleased := make(chan struct{})
			second.referenceLifetime().afterPhysicalRelease(func() { close(secondReleased) })
			nativeExecutionMu.Lock()
			if err := second.Close(); err != nil {
				nativeExecutionMu.Unlock()
				t.Fatal(err)
			}
			if !second.hasPhysicalResources() {
				nativeExecutionMu.Unlock()
				t.Fatal("STOP second owner lost before retry crossing")
			}
			if r9RetryIdle(t, g) != idle {
				nativeExecutionMu.Unlock()
				t.Fatal("callback crossing spawned another driver")
			}
			nativeExecutionMu.Unlock()
			close(leave)
			select {
			case <-secondReleased:
			case <-time.After(5 * time.Second):
				t.Fatal("callback crossing lost pending request")
			}
			r9WaitRetry(t, idle)
			// A request after the empty-queue/exit linearization starts a new driver.
			third, _, thirdCode := r9WakeupOwnerInStore(t, shift, store)
			thirdReleased := make(chan struct{})
			third.referenceLifetime().afterPhysicalRelease(func() { close(thirdReleased) })
			nativeExecutionMu.Lock()
			if err := third.Close(); err != nil {
				nativeExecutionMu.Unlock()
				t.Fatal(err)
			}
			nextIdle := r9RetryIdle(t, g)
			if nextIdle == idle {
				nativeExecutionMu.Unlock()
				t.Fatal("new request reused completed driver")
			}
			nativeExecutionMu.Unlock()
			select {
			case <-thirdReleased:
			case <-time.After(5 * time.Second):
				t.Fatal("request after worker exit was lost")
			}
			r9WaitRetry(t, nextIdle)
			if first.hasPhysicalResources() || second.hasPhysicalResources() || third.hasPhysicalResources() {
				t.Fatal("closed retry owner retained")
			}
			if !anchor.hasPhysicalResources() {
				t.Fatal("STOP open anchor was released")
			}
			anchor.Close()
			store.closeRuntime()
			firstCode.Close()
			secondCode.Close()
			thirdCode.Close()
			anchorCode.Close()
			if _, ok := managedFunctionGraphs.Load(store); ok {
				t.Fatal("retry crossing leaked store graph")
			}
		})
	}
}

func TestR9OwnershipWakeupProviderAndStoreClose(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		for _, closeStore := range []bool{false, true} {
			t.Run(fmt.Sprintf("region=%d/store-close=%t", shift, closeStore), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
				providerSource := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 0, 0})),
					wasmtest.Section(3, wasmtest.Vec([]byte{1})),
					wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("slot", 3, 0))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x0b}))))
				pc, err := Compile(cfg, providerSource)
				if err != nil {
					t.Fatal(err)
				}
				cc, err := Compile(cfg, r9OwnershipNestedModule(false, true, true))
				if err != nil {
					t.Fatal(err)
				}
				store := newReferenceStore(false)
				opts := InstantiateOptions{store: store, GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096, NurseryRegionShift: shift, VerifyAfterCollect: true}}
				provider, err := instantiateCore(pc, opts)
				if err != nil {
					t.Fatal(err)
				}
				slot, err := provider.ExportedGlobalObject("slot")
				if err != nil {
					t.Fatal(err)
				}
				var producer *Instance
				opts.Imports = testImports("p.slot", slot, "env.probe", slotHostFunc(func(_ HostModule, _ []uint64, _ []uint64) {
					store.mu.Lock()
					for owner, entry := range store.instances {
						if owner != provider && !entry.resourcesReleased {
							producer = owner
							break
						}
					}
					store.mu.Unlock()
				}))
				failed, startErr := instantiateCore(cc, opts)
				if failed != nil || startErr == nil || producer == nil {
					t.Fatalf("failed-start admission: %v", startErr)
				}
				if !producer.hasPhysicalResources() || !provider.hasPhysicalResources() {
					t.Fatal("STOP published cycle lacks physical owners")
				}
				value, ok := managedFunctionGraphs.Load(store)
				if !ok {
					t.Fatal("missing published owner graph")
				}
				graph := value.(*managedFunctionGraph)
				released := make(chan string, 2)
				producer.referenceLifetime().afterPhysicalRelease(func() { released <- "producer" })
				provider.referenceLifetime().afterPhysicalRelease(func() { released <- "provider" })
				nativeExecutionMu.Lock()
				if err := provider.Close(); err != nil {
					nativeExecutionMu.Unlock()
					t.Fatal(err)
				}
				if !producer.hasPhysicalResources() || !provider.hasPhysicalResources() {
					nativeExecutionMu.Unlock()
					t.Fatal("STOP contended provider close lost owner before collection")
				}
				idle := r9RetryIdle(t, graph)
				nativeExecutionMu.Unlock()
				// Store retirement can legitimately consume the remaining roots.
				// Race it with the queued retry only after releasing our blocker;
				// subsequent checks inspect callbacks and Go lifetime metadata.
				if closeStore {
					store.closeRuntime()
				}
				seen := map[string]bool{}
				for len(seen) < 2 {
					select {
					case owner := <-released:
						if seen[owner] {
							t.Fatal("duplicate physical release")
						}
						seen[owner] = true
					case <-time.After(5 * time.Second):
						t.Fatal("provider/store close did not release cycle without another invocation")
					}
				}
				r9WaitRetry(t, idle)
				if producer.hasPhysicalResources() || provider.hasPhysicalResources() {
					t.Fatal("cycle retained physical ownership")
				}
				if _, ok := managedFunctionGraphs.Load(store); ok {
					t.Fatal("closed cycle retained graph")
				}
				store.closeRuntime()
				cc.Close()
				pc.Close()
			})
		}
	}
}

func TestR9OwnershipWakeupConcurrentRequests(t *testing.T) {
	in, g, c := r9WakeupOwner(t, 6)
	for round := 0; round < 16; round++ {
		if !in.hasPhysicalResources() {
			t.Fatal("STOP open owner lost before requests")
		}
		nativeExecutionMu.Lock()
		g.pendingCollection.Store(true)
		g.drainAfterGate(0)
		_ = r9RetryIdle(t, g)
		start := make(chan struct{})
		var requests sync.WaitGroup
		for n := 0; n < 8; n++ {
			requests.Add(1)
			go func() { defer requests.Done(); <-start; g.pendingCollection.Store(true); g.drainAfterGate(0) }()
		}
		nativeExecutionMu.Unlock()
		close(start)
		requests.Wait()
		g.hostRetry.mu.Lock()
		idle, running := g.hostRetry.idle, g.hostRetry.running
		g.hostRetry.mu.Unlock()
		if running {
			r9WaitRetry(t, idle)
		}
		if g.pendingCollection.Load() {
			t.Fatal("worker exit lost a concurrent pending request")
		}
		g.hostRetry.mu.Lock()
		running, pending := g.hostRetry.running, g.hostRetry.pending.present()
		g.hostRetry.mu.Unlock()
		if running || pending {
			t.Fatal("coalesced requests leaked a driver")
		}
	}
	if !in.hasPhysicalResources() {
		t.Fatal("STOP open owner released during coalesced requests")
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	in.refStore.closeRuntime()
	c.Close()
	if _, ok := managedFunctionGraphs.Load(in.refStore); ok {
		t.Fatal("concurrent requests leaked graph")
	}
}
