//go:build ((linux && !riscv64) || (darwin && arm64)) && !tinygo && !wago_target_tinygo

package wago

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
)

func TestR9H2ConditionalSlotExplicitDomain(t *testing.T) {
	store := newReferenceStore(false)
	graph := &managedFunctionGraph{store: store, members: make(map[*Instance]*managedFunctionMember), collectors: make(map[*gc.Collector]*managedFunctionCollector)}
	owner := &Instance{refStore: store}
	var states []*managedFunctionCollector
	for i := 0; i < 2; i++ {
		desc, err := gc.NewStructDesc(0, nil)
		if err != nil {
			t.Fatal(err)
		}
		collector, err := gc.NewCollector(gc.Config{}, []gc.TypeDesc{desc})
		if err != nil {
			t.Fatal(err)
		}
		defer collector.Close()
		ref, err := collector.NewStructDefaultWithRoots(0, gc.EmptyRoots{})
		if err != nil {
			t.Fatal(err)
		}
		slot, err := collector.NewCheckedTableSlot(ref)
		if err != nil {
			t.Fatal(err)
		}
		state := &managedFunctionCollector{graph: graph, collector: collector, globalOwners: make(map[uint32]*managedFunctionMember), tableOwners: make(map[uint32]*managedFunctionMember)}
		graph.collectors[collector] = state
		if err := state.bindPersistentSlot(owner, gc.SlotTable, slot); err != nil {
			t.Fatal(err)
		}
		// Repeat binding is idempotent. Equal slot indexes in different collectors
		// remain distinct receipts, even though the owner itself has no collector.
		if err := state.bindPersistentSlot(owner, gc.SlotTable, slot); err != nil {
			t.Fatal(err)
		}
		if err := state.bindPersistentSlot(&Instance{refStore: newReferenceStore(false)}, gc.SlotTable, slot); err == nil {
			t.Fatal("cross-Runtime slot accepted")
		}
		states = append(states, state)
	}
	member := graph.members[owner]
	defer func() { graph.mu.Lock(); graph.removeMemberLocked(member); graph.mu.Unlock() }()
	if owner.gc != nil || len(member.slots) != 2 {
		t.Fatal("STOP explicit domain receipts absent")
	}
	mark := &managedFunctionMark{graph: graph, epoch: graph.epoch.Load(), byCollector: make(map[*gc.Collector]*managedFunctionMarkDomain), live: map[*managedFunctionMember]bool{member: true}, owners: []*managedFunctionMember{member}, edges: make(map[*managedFunctionMember][]*managedFunctionMember)}
	for _, state := range states {
		d := &managedFunctionMarkDomain{state: state, mark: mark}
		mark.domains = append(mark.domains, d)
		mark.byCollector[state.collector] = d
	}
	done := false
	for steps := 0; steps < 100 && !done; steps++ {
		var err error
		_, done, err = mark.step(1)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !done {
		t.Fatal("bounded mark did not finish")
	}
	for _, d := range mark.domains {
		if len(d.seen) == 0 || d.seen[0] == 0 {
			t.Fatal("STOP owning container did not seed its explicit collector")
		}
	}
	// With no live container, conditional slots must not seed either heap.
	dead := &managedFunctionMark{graph: graph, epoch: graph.epoch.Load(), byCollector: make(map[*gc.Collector]*managedFunctionMarkDomain), live: make(map[*managedFunctionMember]bool)}
	for _, state := range states {
		d := &managedFunctionMarkDomain{state: state, mark: dead}
		dead.domains = append(dead.domains, d)
		dead.byCollector[state.collector] = d
	}
	for steps := 0; steps < 100; steps++ {
		_, done, err := dead.step(1)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	if !dead.complete {
		t.Fatal("dead mark did not finish")
	}
	for _, d := range dead.domains {
		if len(d.seen) != 0 {
			t.Fatal("conditional slot became an unconditional root")
		}
	}
}

// Collector close can overlap an unrelated topology reader. Releasing the last
// Env then queues a physical finalizer. Retiring the last collector must not
// erase that journal before the mandatory post-gate drain can consume it.
func TestR9H2RetiredCollectorPreservesDeferredFinalizer(t *testing.T) {
	store := newReferenceStore(false)
	graph := managedFunctionGraphFor(store)
	owner := &Instance{refStore: store}
	graph.mu.Lock()
	member := graph.registerMemberLocked(owner, false)
	member.finalizationPending = true
	graph.pendingFinalizers.Add(1)
	graph.mu.Unlock()
	collector, err := gc.NewCollector(gc.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer collector.Close()
	state := &managedFunctionCollector{graph: graph, collector: collector, closed: true}
	graph.collectors[collector] = state
	managedFunctionCollectors.Store(collector, state)
	topology := store.ensureGCTopology()
	topology.RLock()
	drainManagedFunctionReclaims(collector)
	graph.mu.Lock()
	retained := !graph.retired && graph.members[owner] == member && member.finalizationPending && graph.pendingFinalizers.Load() == 1
	graph.mu.Unlock()
	topology.RUnlock()
	if !retained {
		t.Fatal("STOP collector retirement erased deferred finalizer")
	}
	graph.drainAfterGate(0)
	if graph.pendingFinalizers.Load() != 0 {
		t.Fatal("post-gate finalizer not consumed")
	}
	if _, ok := managedFunctionGraphs.Load(store); ok {
		t.Fatal("empty drained graph did not retire")
	}
}
