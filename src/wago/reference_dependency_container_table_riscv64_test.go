//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"testing"
	"time"
)

func rv64TableDescriptorTestRecord(t *testing.T, table *Table, producer *Instance, graph *dependencyGraph) (*retainedInstanceRoot, *rv64TableDescriptorDependency, tableDescriptorDependencyBatch) {
	t.Helper()
	if !producer.retainResourceRoot() {
		t.Fatal("descriptor bootstrap")
	}
	state := &retainedInstanceRoot{precise: true}
	var batch tableDescriptorDependencyBatch
	table.mu.Lock()
	if table.retained == nil {
		table.retained = make(map[*Instance]*retainedInstanceRoot)
	}
	table.retained[producer] = state
	railshotRecordTableDescriptorDependencyLocked(table, producer, state, &batch)
	row := table.owner.descriptorDependencies.rows[state]
	row.graph = graph
	table.mu.Unlock()
	return state, row, batch
}

func rv64TableDescriptorTestRetire(table *Table, producer *Instance, state *retainedInstanceRoot) tableDescriptorDependencyBatch {
	var batch tableDescriptorDependencyBatch
	table.mu.Lock()
	if table.retained[producer] == state {
		delete(table.retained, producer)
	}
	railshotRetireTableDescriptorDependencyLocked(table, producer, state, &batch)
	table.mu.Unlock()
	return batch
}

func TestRV64TableDescriptorPendingLifecycle(t *testing.T) {
	producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
	graph, err := newRV64FunctionDependencyGraph(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	table := &Table{owner: &tableOwner{elementType: ValFuncRef}}
	state, row, publication := rv64TableDescriptorTestRecord(t, table, producer, graph)
	railshotFlushTableDescriptorDependencies(&publication)
	if dependencyTestRefs(producer) != 1 || row.phase != rv64TableDescriptorPending || row.receipt.graph != graph || row.receiptIdentity == 0 {
		t.Fatal("table descriptor publication did not exchange its bootstrap")
	}
	graph.mu.Lock()
	record, _ := graph.recordLocked(row.receiptIdentity)
	pending := record != nil && record.state == dependencyPending && record.target.owner == producer && record.consumer == nil && record.kind == 0
	graph.mu.Unlock()
	if !pending {
		t.Fatal("table descriptor receipt was not Pending/external")
	}
	retirement := rv64TableDescriptorTestRetire(table, producer, state)
	railshotFlushTableDescriptorDependencies(&retirement)
	if dependencyTestRefs(producer) != 0 || len(table.owner.descriptorDependencies.rows) != 0 || graph.live != 0 || row.phase != rv64TableDescriptorReleased {
		t.Fatal("exact table descriptor row did not drain")
	}
}

func TestRV64TableDescriptorDelayedPublicationCannotReviveReplacement(t *testing.T) {
	producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
	graph, _ := newRV64FunctionDependencyGraph(1, 2)
	table := &Table{owner: &tableOwner{elementType: ValFuncRef}}
	oldState, oldRow, oldPublication := rv64TableDescriptorTestRecord(t, table, producer, graph)
	oldRetirement := rv64TableDescriptorTestRetire(table, producer, oldState)
	newState, newRow, newPublication := rv64TableDescriptorTestRecord(t, table, producer, graph)
	railshotFlushTableDescriptorDependencies(&oldRetirement)
	railshotFlushTableDescriptorDependencies(&oldPublication)
	railshotFlushTableDescriptorDependencies(&newPublication)
	if oldRow.phase != rv64TableDescriptorReleased || newRow.phase != rv64TableDescriptorPending || table.retained[producer] != newState || len(table.owner.descriptorDependencies.rows) != 1 || dependencyTestRefs(producer) != 1 || graph.live != 1 {
		t.Fatal("delayed table publication revived or consumed a replacement")
	}
	retirement := rv64TableDescriptorTestRetire(table, producer, newState)
	railshotFlushTableDescriptorDependencies(&retirement)
}

func TestRV64TableDescriptorDeniedClaimRepairsExactReceipt(t *testing.T) {
	producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
	graph, _ := newRV64FunctionDependencyGraph(1, 1)
	table := &Table{owner: &tableOwner{elementType: ValFuncRef}}
	state, row, publication := rv64TableDescriptorTestRecord(t, table, producer, graph)
	railshotFlushTableDescriptorDependencies(&publication)
	receipt := row.receipt
	graph.mu.Lock()
	record, _ := graph.recordLocked(receipt.identity)
	record.state = dependencyReserved
	graph.mu.Unlock()
	retirement := rv64TableDescriptorTestRetire(table, producer, state)
	_, panicked := cleanupTestPanic(func() { railshotFlushTableDescriptorDependencies(&retirement) })
	if !panicked || row.phase != rv64TableDescriptorPending || dependencyTestRefs(producer) != 1 {
		t.Fatal("denied table claim erased its exact obligation")
	}
	graph.mu.Lock()
	record, _ = graph.recordLocked(receipt.identity)
	record.state = dependencyPending
	graph.mu.Unlock()
	var retry tableDescriptorDependencyBatch
	table.mu.Lock()
	railshotRetryTableDescriptorDependenciesLocked(table, &retry)
	table.mu.Unlock()
	railshotFlushTableDescriptorDependencies(&retry)
	if dependencyTestRefs(producer) != 0 || graph.live != 0 || row.phase != rv64TableDescriptorReleased {
		t.Fatal("repaired table obligation did not drain")
	}
}

func TestRV64TableDescriptorCapacityKeepsAnonymousUnit(t *testing.T) {
	for _, capacity := range []string{"node", "receipt"} {
		t.Run(capacity, func(t *testing.T) {
			owners := rv64FunctionDependencyUnitOwners(t, 2)
			blocker, producer := owners[0], owners[1]
			nodes := 1
			if capacity == "receipt" {
				nodes = 2
			}
			graph, _ := newRV64FunctionDependencyGraph(nodes, 1)
			blockerMember := &rv64FunctionDependencyOwnershipFor(blocker).member
			if err := graph.register(blockerMember); err != nil {
				t.Fatal(err)
			}
			var blockerReceipt dependencyReceipt
			if capacity == "receipt" {
				var err error
				blockerReceipt, err = graph.acquirePending(blockerMember, false)
				if err != nil {
					t.Fatal(err)
				}
			}
			table := &Table{owner: &tableOwner{elementType: ValFuncRef}}
			state, row, publication := rv64TableDescriptorTestRecord(t, table, producer, graph)
			railshotFlushTableDescriptorDependencies(&publication)
			if row.phase != rv64TableDescriptorAnonymous || row.receipt.graph != nil || dependencyTestRefs(producer) != 1 {
				t.Fatal(capacity, "capacity did not preserve one anonymous table unit")
			}
			retirement := rv64TableDescriptorTestRetire(table, producer, state)
			railshotFlushTableDescriptorDependencies(&retirement)
			if dependencyTestRefs(producer) != 0 || row.phase != rv64TableDescriptorReleased {
				t.Fatal("anonymous table unit did not drain")
			}
			blockerReceipt.release()
			blocker.lifeMu.Lock()
			blocker.resourcesClosed = true
			blocker.lifeMu.Unlock()
			railshotFunctionDependencyCleanupCompleted(blocker)
		})
	}
}

func TestRV64NumericTableDescriptorRemainsLegacy(t *testing.T) {
	producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
	if !producer.retainResourceRoot() {
		t.Fatal("numeric table bootstrap")
	}
	table := &Table{owner: &tableOwner{elementType: ValI32}, retained: make(map[*Instance]*retainedInstanceRoot)}
	state := &retainedInstanceRoot{precise: true}
	table.retained[producer] = state
	var batch tableDescriptorDependencyBatch
	table.mu.Lock()
	railshotRecordTableDescriptorDependencyLocked(table, producer, state, &batch)
	delete(table.retained, producer)
	railshotRetireTableDescriptorDependencyLocked(table, producer, state, &batch)
	table.mu.Unlock()
	railshotFlushTableDescriptorDependencies(&batch)
	if table.owner.descriptorDependencies.rows != nil || dependencyTestRefs(producer) != 0 {
		t.Fatal("numeric Table allocated an RV64 descriptor receipt")
	}
}

func TestRV64TableDescriptorActiveWriterPromiseStaysBootstrap(t *testing.T) {
	producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
	if !producer.retainResourceRoot() {
		t.Fatal("writer bootstrap")
	}
	table := &Table{desc: make([]byte, 8), owner: &tableOwner{elementType: ValFuncRef}, retained: make(map[*Instance]*retainedInstanceRoot)}
	state := &retainedInstanceRoot{activeWriters: 1}
	table.retained[producer] = state
	table.mu.Lock()
	railshotRecordTableDescriptorDependencyLocked(table, producer, state, nil)
	row := table.owner.descriptorDependencies.rows[state]
	table.mu.Unlock()
	table.pruneRetainedInstances()
	if row == nil || row.phase != rv64TableDescriptorBootstrap || row.receipt.graph != nil || dependencyTestRefs(producer) != 1 {
		t.Fatal("active-writer-only promise was published as a persistent receipt")
	}
	table.mu.Lock()
	state.activeWriters = 0
	table.mu.Unlock()
	table.pruneRetainedInstances()
	if row.phase != rv64TableDescriptorReleased || len(table.owner.descriptorDependencies.rows) != 0 || dependencyTestRefs(producer) != 0 {
		t.Fatal("expired active-writer promise did not release its exact bootstrap")
	}
}

func TestRV64TableDescriptorPublishingCloseOverlap(t *testing.T) {
	producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
	graph, _ := newRV64FunctionDependencyGraph(1, 1)
	table, err := NewTable(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = table.Close() }) })
	_, row, publication := rv64TableDescriptorTestRecord(t, table, producer, graph)
	graph.mu.Lock()
	type result struct {
		value    any
		panicked bool
	}
	published := make(chan result, 1)
	go func() {
		value, panicked := cleanupTestPanic(func() { railshotFlushTableDescriptorDependencies(&publication) })
		published <- result{value, panicked}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		table.mu.Lock()
		phase := row.phase
		table.mu.Unlock()
		if phase == rv64TableDescriptorPublishing {
			break
		}
		if time.Now().After(deadline) {
			graph.mu.Unlock()
			select {
			case <-published:
			case <-time.After(5 * time.Second):
			}
			t.Fatal("table publisher did not reach graph-blocked phase")
		}
	}
	closed := make(chan result, 1)
	go func() {
		value, panicked := cleanupTestPanic(func() {
			if err := table.Close(); err != nil {
				panic(err)
			}
		})
		closed <- result{value, panicked}
	}()
	var closeResult result
	select {
	case closeResult = <-closed:
	case <-time.After(5 * time.Second):
		graph.mu.Unlock()
		select {
		case <-published:
		case <-time.After(5 * time.Second):
			t.Fatal("table publisher did not join after graph release")
		}
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Fatal("table terminal worker did not join after graph release")
		}
		t.Fatal("table terminal claim blocked on publication")
	}
	graph.mu.Unlock()
	var publishResult result
	select {
	case publishResult = <-published:
	case <-time.After(5 * time.Second):
		t.Fatal("table publisher did not finish after graph release")
	}
	if closeResult.panicked || publishResult.panicked {
		t.Fatal("overlapping table publication/close panicked", closeResult.value, publishResult.value)
	}
	if row.phase != rv64TableDescriptorReleased || len(table.owner.descriptorDependencies.rows) != 0 || dependencyTestRefs(producer) != 0 || graph.live != 0 || !table.closed || table.desc != nil || table.owner.arena != nil {
		t.Fatal("overlapping table publisher resurrected a terminal row")
	}
}
