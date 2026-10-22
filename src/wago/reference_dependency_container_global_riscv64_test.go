//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"testing"
	"time"
)

func rv64GlobalDescriptorTestRecord(t *testing.T, owner *globalOwner, producer *Instance, graph *dependencyGraph) (*retainedInstanceRoot, *rv64GlobalDescriptorDependency, globalDescriptorDependencyBatch) {
	t.Helper()
	if !producer.retainResourceRoot() {
		t.Fatal("descriptor bootstrap")
	}
	state := &retainedInstanceRoot{precise: true}
	var batch globalDescriptorDependencyBatch
	owner.mu.Lock()
	if owner.retained == nil {
		owner.retained = make(map[*Instance]*retainedInstanceRoot)
	}
	owner.retained[producer] = state
	railshotRecordGlobalDescriptorDependencyLocked(owner, producer, state, &batch)
	row := owner.descriptorDependencies.rows[state]
	row.graph = graph
	owner.mu.Unlock()
	return state, row, batch
}

func rv64GlobalDescriptorTestRetire(owner *globalOwner, producer *Instance, state *retainedInstanceRoot) globalDescriptorDependencyBatch {
	var batch globalDescriptorDependencyBatch
	owner.mu.Lock()
	if owner.retained[producer] == state {
		delete(owner.retained, producer)
	}
	railshotRetireGlobalDescriptorDependencyLocked(owner, producer, state, &batch)
	owner.mu.Unlock()
	return batch
}

func TestRV64GlobalDescriptorPendingLifecycle(t *testing.T) {
	producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
	graph, err := newRV64FunctionDependencyGraph(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	owner := &globalOwner{}
	state, row, publication := rv64GlobalDescriptorTestRecord(t, owner, producer, graph)
	railshotFlushGlobalDescriptorDependencies(&publication)
	if dependencyTestRefs(producer) != 1 || row.phase != rv64GlobalDescriptorPending || row.receipt.graph != graph || row.receiptIdentity == 0 {
		t.Fatal("descriptor publication did not exchange one bootstrap for one Pending receipt")
	}
	graph.mu.Lock()
	record, _ := graph.recordLocked(row.receiptIdentity)
	pending := record != nil && record.state == dependencyPending && record.target.owner == producer && record.consumer == nil && record.kind == 0
	graph.mu.Unlock()
	if !pending {
		t.Fatal("mutable global descriptor receipt was not Pending/external")
	}

	retirement := rv64GlobalDescriptorTestRetire(owner, producer, state)
	railshotFlushGlobalDescriptorDependencies(&retirement)
	owner.mu.Lock()
	remaining := len(owner.descriptorDependencies.rows)
	owner.mu.Unlock()
	if dependencyTestRefs(producer) != 0 || remaining != 0 || graph.live != 0 || row.phase != rv64GlobalDescriptorReleased {
		t.Fatal("exact Pending descriptor row did not drain")
	}
}

func TestRV64GlobalDescriptorDelayedPublicationCannotReviveReplacement(t *testing.T) {
	producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
	graph, _ := newRV64FunctionDependencyGraph(1, 2)
	owner := &globalOwner{}
	oldState, oldRow, oldPublication := rv64GlobalDescriptorTestRecord(t, owner, producer, graph)
	oldRetirement := rv64GlobalDescriptorTestRetire(owner, producer, oldState)
	newState, newRow, newPublication := rv64GlobalDescriptorTestRecord(t, owner, producer, graph)

	// A closer/overwriter may retire the bootstrap before its delayed publisher.
	railshotFlushGlobalDescriptorDependencies(&oldRetirement)
	railshotFlushGlobalDescriptorDependencies(&oldPublication)
	railshotFlushGlobalDescriptorDependencies(&newPublication)
	owner.mu.Lock()
	current := owner.retained[producer]
	rows := len(owner.descriptorDependencies.rows)
	owner.mu.Unlock()
	if oldRow.phase != rv64GlobalDescriptorReleased || newRow.phase != rv64GlobalDescriptorPending || current != newState || rows != 1 || dependencyTestRefs(producer) != 1 || graph.live != 1 {
		t.Fatal("delayed publication revived or consumed a replacement row")
	}
	retirement := rv64GlobalDescriptorTestRetire(owner, producer, newState)
	railshotFlushGlobalDescriptorDependencies(&retirement)
}

func TestRV64GlobalDescriptorDeniedClaimRepairsExactReceipt(t *testing.T) {
	producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
	graph, _ := newRV64FunctionDependencyGraph(1, 1)
	owner := &globalOwner{}
	state, row, publication := rv64GlobalDescriptorTestRecord(t, owner, producer, graph)
	railshotFlushGlobalDescriptorDependencies(&publication)
	receipt := row.receipt
	graph.mu.Lock()
	record, _ := graph.recordLocked(receipt.identity)
	record.state = dependencyReserved
	graph.mu.Unlock()
	retirement := rv64GlobalDescriptorTestRetire(owner, producer, state)
	_, panicked := cleanupTestPanic(func() { railshotFlushGlobalDescriptorDependencies(&retirement) })
	if !panicked || row.phase != rv64GlobalDescriptorPending || dependencyTestRefs(producer) != 1 {
		t.Fatal("denied claim erased or consumed the exact descriptor obligation")
	}
	graph.mu.Lock()
	record, _ = graph.recordLocked(receipt.identity)
	record.state = dependencyPending
	graph.mu.Unlock()
	var retry globalDescriptorDependencyBatch
	owner.mu.Lock()
	railshotRetryGlobalDescriptorDependenciesLocked(owner, &retry)
	owner.mu.Unlock()
	railshotFlushGlobalDescriptorDependencies(&retry)
	if dependencyTestRefs(producer) != 0 || graph.live != 0 || row.phase != rv64GlobalDescriptorReleased {
		t.Fatal("repaired exact descriptor obligation did not drain")
	}
}

func TestRV64GlobalDescriptorCapacityKeepsAnonymousUnit(t *testing.T) {
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
			owner := &globalOwner{}
			state, row, publication := rv64GlobalDescriptorTestRecord(t, owner, producer, graph)
			railshotFlushGlobalDescriptorDependencies(&publication)
			wantLive := 0
			if capacity == "receipt" {
				wantLive = 1
			}
			if row.phase != rv64GlobalDescriptorAnonymous || row.receipt.graph != nil || dependencyTestRefs(producer) != 1 || graph.live != wantLive {
				t.Fatal(capacity, "capacity did not preserve one anonymous physical unit")
			}
			retirement := rv64GlobalDescriptorTestRetire(owner, producer, state)
			railshotFlushGlobalDescriptorDependencies(&retirement)
			if dependencyTestRefs(producer) != 0 || row.phase != rv64GlobalDescriptorReleased {
				t.Fatal("capacity fallback unit did not drain")
			}
			blockerReceipt.release()
			blocker.lifeMu.Lock()
			blocker.resourcesClosed = true
			blocker.lifeMu.Unlock()
			railshotFunctionDependencyCleanupCompleted(blocker)
		})
	}
}

func TestRV64GlobalDescriptorAuthorityFailureRetriesSameBootstrap(t *testing.T) {
	producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
	authoritative, _ := newRV64FunctionDependencyGraph(1, 1)
	foreign, _ := newRV64FunctionDependencyGraph(1, 1)
	member := &rv64FunctionDependencyOwnershipFor(producer).member
	if err := authoritative.register(member); err != nil {
		t.Fatal(err)
	}
	owner := &globalOwner{}
	state, row, publication := rv64GlobalDescriptorTestRecord(t, owner, producer, foreign)
	_, panicked := cleanupTestPanic(func() { railshotFlushGlobalDescriptorDependencies(&publication) })
	if !panicked || row.phase != rv64GlobalDescriptorFailed || dependencyTestRefs(producer) != 1 || row.receipt.graph != nil {
		t.Fatal("foreign authority failure consumed or anonymized the bootstrap")
	}
	var retry globalDescriptorDependencyBatch
	owner.mu.Lock()
	row.graph = authoritative
	railshotPublishGlobalDescriptorDependencyLocked(owner, state, &retry)
	owner.mu.Unlock()
	railshotFlushGlobalDescriptorDependencies(&retry)
	if row.phase != rv64GlobalDescriptorPending || row.receipt.graph != authoritative || dependencyTestRefs(producer) != 1 || authoritative.live != 1 {
		t.Fatal("same failed bootstrap did not publish on retry")
	}
	retirement := rv64GlobalDescriptorTestRetire(owner, producer, state)
	railshotFlushGlobalDescriptorDependencies(&retirement)
}

func TestRV64GlobalDescriptorPublishingCloseOverlap(t *testing.T) {
	producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
	graph, _ := newRV64FunctionDependencyGraph(1, 1)
	global := newGlobal(ValFuncRef, 0, V128{}, true)
	t.Cleanup(func() {
		_, _ = cleanupTestPanic(func() { _ = global.Close() })
	})
	state, row, publication := rv64GlobalDescriptorTestRecord(t, global.owner, producer, graph)
	_ = state

	graph.mu.Lock()
	type panicResult struct {
		value    any
		panicked bool
	}
	published := make(chan panicResult, 1)
	go func() {
		value, panicked := cleanupTestPanic(func() { railshotFlushGlobalDescriptorDependencies(&publication) })
		published <- panicResult{value: value, panicked: panicked}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		global.owner.mu.Lock()
		phase := row.phase
		global.owner.mu.Unlock()
		if phase == rv64GlobalDescriptorPublishing {
			break
		}
		if time.Now().After(deadline) {
			graph.mu.Unlock()
			select {
			case <-published:
			case <-time.After(5 * time.Second):
			}
			t.Fatal("publisher did not reach graph-blocked phase")
		}
	}
	type closeResult struct {
		err      error
		value    any
		panicked bool
	}
	closed := make(chan closeResult, 1)
	go func() {
		var err error
		value, panicked := cleanupTestPanic(func() { err = global.Close() })
		closed <- closeResult{err: err, value: value, panicked: panicked}
	}()
	var closeOutcome closeResult
	closeTimedOut := false
	select {
	case closeOutcome = <-closed:
	case <-time.After(5 * time.Second):
		closeTimedOut = true
	}
	graph.mu.Unlock()
	var result panicResult
	select {
	case result = <-published:
	case <-time.After(5 * time.Second):
		t.Fatal("publisher did not join after graph release")
	}
	if closeTimedOut {
		select {
		case closeOutcome = <-closed:
		case <-time.After(5 * time.Second):
			t.Fatal("Close worker did not join after graph release")
		}
		t.Fatal("Close waited on graph publication while owning terminal state")
	}
	if closeOutcome.panicked || closeOutcome.err != nil {
		t.Fatal("overlapping Close failed", closeOutcome.err, closeOutcome.value)
	}
	if result.panicked {
		t.Fatal("publisher panicked after overlapping Close", result.value)
	}
	global.owner.mu.Lock()
	rows := len(global.owner.descriptorDependencies.rows)
	global.owner.mu.Unlock()
	if row.phase != rv64GlobalDescriptorReleased || rows != 0 || dependencyTestRefs(producer) != 0 || graph.live != 0 {
		t.Fatal("overlapping publisher resurrected a terminal Global row")
	}
}

func TestRV64GlobalDescriptorMappedReleasePanicCompletes(t *testing.T) {
	compiled := rv64TableStageCompile(t, "global-descriptor-release-panic", rv64ReferenceGlobalExportModule(t, false, false, false, 1), false)
	producer, err := Instantiate(compiled)
	compiled.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = cleanupTestPanic(func() { _ = producer.Close() })
	})
	graph, _ := newRV64FunctionDependencyGraph(1, 1)
	owner := &globalOwner{}
	state, row, publication := rv64GlobalDescriptorTestRecord(t, owner, producer, graph)
	t.Cleanup(func() {
		var retirement globalDescriptorDependencyBatch
		owner.mu.Lock()
		current := owner.retained[producer]
		if current != nil {
			delete(owner.retained, producer)
			railshotRetireGlobalDescriptorDependencyLocked(owner, producer, current, &retirement)
		}
		railshotRetryGlobalDescriptorDependenciesLocked(owner, &retirement)
		owner.mu.Unlock()
		_, _ = cleanupTestPanic(func() { railshotFlushGlobalDescriptorDependencies(&retirement) })
	})
	railshotFlushGlobalDescriptorDependencies(&publication)
	type panicMarker struct{}
	marker := &panicMarker{}
	callbacks := 0
	lockAvailable := false
	producer.referenceLifetime().afterPhysicalRelease(func() {
		callbacks++
		lockAvailable = nativeExecutionMu.TryLock()
		if lockAvailable {
			nativeExecutionMu.Unlock()
		}
		panic(marker)
	})
	if err := producer.Close(); err != nil || producer.resourcesClosed || dependencyTestRefs(producer) != 1 {
		t.Fatal("descriptor root did not hold logically closed mapped producer", err, producer.resourcesClosed, dependencyTestRefs(producer))
	}
	retirement := rv64GlobalDescriptorTestRetire(owner, producer, state)
	value, panicked := cleanupTestPanic(func() { railshotFlushGlobalDescriptorDependencies(&retirement) })
	owner.mu.Lock()
	rows := len(owner.descriptorDependencies.rows)
	owner.mu.Unlock()
	if !panicked || value != marker || callbacks != 1 || !lockAvailable || row.phase != rv64GlobalDescriptorReleased || rows != 0 || graph.live != 0 || !producer.resourcesClosed || producer.hasPhysicalResources() {
		t.Fatal("mapped release callback did not preserve panic and complete receipt/native cleanup", panicked, value, callbacks, lockAvailable, row.phase, rows, graph.live, producer.resourcesClosed)
	}
}
