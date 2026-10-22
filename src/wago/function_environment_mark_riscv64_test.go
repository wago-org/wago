//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"runtime"
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestRV64ManagedFunctionJointRejectsHighHandleBeforeBitmapGrowth(t *testing.T) {
	in, _ := ownedExceptionTestInstance(t, gc.ProfileTiny, nil)
	state := in.managedFunctionCollector(nil)
	mark := state.graph.newMark()
	domain := mark.byCollector[in.gc]
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	domain.VisitObjectRef(gc.Ref(0xfffffffe))
	runtime.ReadMemStats(&after)
	if mark.err == nil || len(domain.seen) != 0 || cap(domain.seen) != 0 {
		t.Fatal("invalid high handle grew the joint bitmap")
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
		t.Fatalf("invalid high handle allocated %d bytes", allocated)
	}
}

// The synthetic owners have no native close journal. Install the exact terminal
// lifecycle facts that a real completed Close publishes before conditional roots.
func managedFunctionTestClose(in *Instance) {
	in.closed = true
	in.invocationState.Store(instanceInvocationClosed)
	state := &instanceCloseState{}
	state.prepared.Store(true)
	state.completed.Store(true)
	state.terminalComplete.Store(true)
	in.closeState.Store(state)
}

func managedFunctionFinishMark(t *testing.T, m *managedFunctionMark, budget uint32) {
	t.Helper()
	for steps := 0; steps < 100000; steps++ {
		used, done, err := m.step(budget)
		if err != nil {
			t.Fatal(err)
		}
		if used > budget {
			t.Fatalf("mark work=%d exceeds budget=%d", used, budget)
		}
		if done {
			return
		}
	}
	t.Fatal("joint mark did not reach a fixed point")
}

func TestRV64ManagedFunctionJointClosedCycle(t *testing.T) {
	for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
		t.Run(map[gc.Profile]string{gc.ProfileThroughput: "throughput", gc.ProfileTiny: "tiny"}[profile], func(t *testing.T) {
			a, _ := ownedExceptionTestInstance(t, profile, nil)
			b, _ := ownedExceptionTestInstance(t, profile, nil)
			b.refStore = a.refStore
			sa, sb := a.managedFunctionCollector(nil), b.managedFunctionCollector(nil)
			ea, err := sa.newManagedFunctionEnv(b, gc.EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			eb, err := sb.newManagedFunctionEnv(a, gc.EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			// The same compact handle value can denote a different producer in each
			// collector. No edge is interpreted in the other collector's directory.
			oa, err := a.gc.NewStructDefaultWithRoots(0, managedFunctionEnvRoots{gc.Root(ea)})
			if err != nil {
				t.Fatal(err)
			}
			ob, err := b.gc.NewStructDefaultWithRoots(0, managedFunctionEnvRoots{gc.Root(eb)})
			if err != nil {
				t.Fatal(err)
			}
			sa.shadows[oa] = managedFunctionEdge{ref: ea, count: 1}
			a.gc.WriteBarrierObject(oa, ea)
			sb.shadows[ob] = managedFunctionEdge{ref: eb, count: 1}
			b.gc.WriteBarrierObject(ob, eb)
			ga, gb := a.gc.NewGlobalSlot(oa), b.gc.NewTableSlot(ob)
			if err := sa.bindPersistentSlot(a, gc.SlotGlobal, ga); err != nil {
				t.Fatal(err)
			}
			if err := sb.bindPersistentSlot(b, gc.SlotTable, gb); err != nil {
				t.Fatal(err)
			}
			public := a.gc.NewGlobalSlot(oa)
			managedFunctionTestClose(a)
			managedFunctionTestClose(b)
			m := sa.graph.newMark()
			managedFunctionFinishMark(t, m, 1)
			if len(m.live) != 2 || len(m.domains) != 2 {
				t.Fatalf("reachable owners=%d domains=%d", len(m.live), len(m.domains))
			}
			if err := m.collect(); err != nil {
				t.Fatal(err)
			}
			a.invocationState.Store(0)
			drainManagedFunctionReclaims(a.gc)
			a.invocationState.Store(instanceInvocationClosed)
			b.invocationState.Store(0)
			drainManagedFunctionReclaims(b.gc)
			b.invocationState.Store(instanceInvocationClosed)
			if len(sa.envs) != 1 || len(sb.envs) != 1 || a.resourceRefs != 1 || b.resourceRefs != 1 {
				t.Fatal("publicly reachable cross-domain cycle was reclaimed")
			}
			if err := a.gc.SetGlobalSlot(public, gc.Null()); err != nil {
				t.Fatal(err)
			}
			m = sa.graph.newMark()
			managedFunctionFinishMark(t, m, 1)
			if len(m.live) != 0 {
				t.Fatalf("closed cycle has %d strong owners", len(m.live))
			}
			if err := m.collect(); err != nil {
				t.Fatal(err)
			}
			a.invocationState.Store(0)
			drainManagedFunctionReclaims(a.gc)
			a.invocationState.Store(instanceInvocationClosed)
			b.invocationState.Store(0)
			drainManagedFunctionReclaims(b.gc)
			b.invocationState.Store(instanceInvocationClosed)
			if len(sa.envs) != 0 || len(sb.envs) != 0 || len(sa.shadows) != 0 || len(sb.shadows) != 0 || a.resourceRefs != 0 || b.resourceRefs != 0 {
				t.Fatal("closed cross-domain cycle retained objects or physical roots")
			}
			if a.gc.GlobalSlot(ga) != gc.Null() || b.gc.TableSlot(gb) != gc.Null() {
				t.Fatal("collected owner retained invalid persistent slot")
			}
		})
	}
}

func TestRV64ManagedFunctionJointMarkRejectsChangedRoots(t *testing.T) {
	in, _ := ownedExceptionTestInstance(t, gc.ProfileTiny, nil)
	state := in.managedFunctionCollector(nil)
	if _, err := state.newManagedFunctionEnv(in, gc.EmptyRoots{}); err != nil {
		t.Fatal(err)
	}
	managedFunctionTestClose(in)
	mark := state.graph.newMark()
	managedFunctionFinishMark(t, mark, 1)
	if !in.retainResourceRootForFinalization() {
		t.Fatal("could not acquire external root")
	}
	defer func() { in.invocationState.Store(0); in.releaseResourceRoot() }()
	if err := mark.collect(); !errors.Is(err, errManagedFunctionMarkChanged) {
		t.Fatalf("stale commit err=%v", err)
	}
	if _, _, err := mark.step(1); !errors.Is(err, errManagedFunctionMarkChanged) {
		t.Fatalf("stale step err=%v", err)
	}
	if len(state.envs) != 1 {
		t.Fatal("stale mark reclaimed a live Env")
	}
}
