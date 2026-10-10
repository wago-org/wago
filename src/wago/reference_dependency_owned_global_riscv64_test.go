//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// This fixture uses the existing private post-instantiation export seam. It
// exercises genuine mapped storage and lazy owner binding without widening
// mutable reference-global admission.
func rv64OwnedGlobalDescriptorFixture(t *testing.T, artifact bool) (owner, producer *Instance, global *Global, row *rv64GlobalDescriptorDependency) {
	t.Helper()
	compiled := globalValueAccessCompile(t, false, artifact)
	var err error
	owner, err = Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = owner.Close() }) })
	producer, err = Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = producer.Close() }) })
	global = globalValueAccessOwnedCell(t, owner)
	if again := globalValueAccessOwnedCell(t, owner); again != global {
		t.Fatal("lazy export alias replaced the owned Global")
	}
	descriptor, ok := producer.localFuncrefDescriptor(0)
	if !ok {
		t.Fatal("mapped producer descriptor")
	}
	global.owner.mu.Lock()
	writeGlobalObject(global, ValFuncRef, descriptor)
	global.owner.mu.Unlock()
	if !global.retainProducerInstance(producer) {
		t.Fatal("owned Global did not retain mapped producer")
	}
	global.owner.mu.Lock()
	state := global.owner.retained[producer]
	row = global.owner.descriptorDependencies.rows[state]
	holder := global.owner.descriptorDependencies.holder
	global.owner.mu.Unlock()
	ownership := rv64ExistingFunctionDependencyOwnership(owner)
	if row == nil || row.phase != rv64GlobalDescriptorPending || holder == nil || holder != ownership || ownership.ownedGlobalDescriptors.Load() != global.owner || global.owner.descriptorDependencies.nextOwned != nil {
		t.Fatal("owned Global lacks one authoritative holder/Pending row")
	}
	return owner, producer, global, row
}

func TestRV64OwnedGlobalDescriptorTerminalCallbackCompletes(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			owner, producer, global, row := rv64OwnedGlobalDescriptorFixture(t, artifact)
			ownership := rv64ExistingFunctionDependencyOwnership(owner)
			type panicMarker struct{}
			marker := &panicMarker{}
			callbacks := 0
			reentrantPrompt := false
			producer.referenceLifetime().afterPhysicalRelease(func() {
				callbacks++
				reentrantPrompt = owner.Close() == nil && !ownership.physicalCleanupFinished.Load() && !ownership.cleanupCompleted.Load()
				panic(marker)
			})
			if err := producer.Close(); err != nil || producer.resourcesClosed || dependencyTestRefs(producer) != 1 {
				t.Fatal("descriptor did not retain logically closed producer", err, producer.resourcesClosed, dependencyTestRefs(producer))
			}
			err := owner.Close()
			if !errors.Is(err, ErrCallbackPanic) || callbacks != 1 || !reentrantPrompt {
				t.Fatal("terminal callback panic/reentrant Close contract", err, callbacks, reentrantPrompt)
			}
			if waited := owner.WaitClosed(context.Background()); !errors.Is(waited, ErrCallbackPanic) {
				t.Fatal("WaitClosed lost synchronous adoption cleanup failure", waited)
			}
			global.owner.mu.Lock()
			rows := len(global.owner.descriptorDependencies.rows)
			global.owner.mu.Unlock()
			if row.phase != rv64GlobalDescriptorReleased || rows != 0 || !producer.resourcesClosed || producer.hasPhysicalResources() || !owner.resourcesClosed || owner.globalCells != nil || !ownership.physicalCleanupFinished.Load() || !ownership.cleanupCompleted.Load() {
				t.Fatal("owned descriptor terminal cleanup incomplete", row.phase, rows, producer.resourcesClosed, owner.resourcesClosed, ownership.physicalCleanupFinished.Load(), ownership.cleanupCompleted.Load())
			}
		})
	}
}

func TestRV64OwnedGlobalDescriptorDeniedTerminalRetry(t *testing.T) {
	owner, producer, global, row := rv64OwnedGlobalDescriptorFixture(t, false)
	ownership := rv64ExistingFunctionDependencyOwnership(owner)
	receipt := row.receipt
	if err := receipt.graph.register(&ownership.member); err != nil {
		t.Fatal("register exact container owner", err)
	}
	receipt.graph.mu.Lock()
	record, _ := receipt.graph.recordLocked(receipt.identity)
	record.state = dependencyReserved
	receipt.graph.mu.Unlock()
	faultOwned := true
	defer func() {
		if !faultOwned {
			return
		}
		receipt.graph.mu.Lock()
		if current, _ := receipt.graph.recordLocked(receipt.identity); current != nil && current.state == dependencyReserved {
			current.state = dependencyPending
		}
		receipt.graph.mu.Unlock()
	}()
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	first := owner.Close()
	global.owner.mu.Lock()
	rows := len(global.owner.descriptorDependencies.rows)
	global.owner.mu.Unlock()
	if !errors.Is(first, ErrCallbackPanic) || first != owner.closeState.Load().result || owner.WaitClosed(nil) != first || !owner.resourcesClosed || owner.globalCells != nil || !ownership.physicalCleanupFinished.Load() || ownership.cleanupCompleted.Load() || !ownership.member.active.Load() || rows != 1 || row.phase != rv64GlobalDescriptorPending || dependencyTestRefs(producer) != 1 || producer.resourcesClosed {
		t.Fatal("denied terminal claim lost durable holder/root", first, owner.resourcesClosed, ownership.physicalCleanupFinished.Load(), ownership.cleanupCompleted.Load(), rows, row.phase, dependencyTestRefs(producer), producer.resourcesClosed)
	}
	receipt.graph.mu.Lock()
	record, _ = receipt.graph.recordLocked(receipt.identity)
	record.state = dependencyPending
	receipt.graph.mu.Unlock()
	faultOwned = false
	second := owner.Close()
	global.owner.mu.Lock()
	rows = len(global.owner.descriptorDependencies.rows)
	global.owner.mu.Unlock()
	memberGraph := ownership.member.graph.Load()
	memberGraph.mu.Lock()
	retired := memberGraph.owners[owner] == nil && ownership.member.receipts == 0
	memberGraph.mu.Unlock()
	if second != first || owner.closeState.Load().result != first || owner.WaitClosed(nil) != first || rows != 0 || row.phase != rv64GlobalDescriptorReleased || dependencyTestRefs(producer) != 0 || !producer.resourcesClosed || producer.hasPhysicalResources() || !ownership.cleanupCompleted.Load() || ownership.member.active.Load() || !retired {
		t.Fatal("repaired repeated Close did not preserve result and complete retirement", second, rows, row.phase, dependencyTestRefs(producer), producer.resourcesClosed, ownership.cleanupCompleted.Load())
	}
}

func TestRV64OwnedGlobalDescriptorPublishingCompletesAfterPhysicalTeardown(t *testing.T) {
	compiled := globalValueAccessCompile(t, false, false)
	owner, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = owner.Close() }) })
	producer, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = producer.Close() }) })
	global := globalValueAccessOwnedCell(t, owner)
	ownership := rv64ExistingFunctionDependencyOwnership(owner)
	graph, err := newRV64FunctionDependencyGraph(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.register(&ownership.member); err != nil {
		t.Fatal("register exact container owner", err)
	}
	_, row, publication := rv64GlobalDescriptorTestRecord(t, global.owner, producer, graph)

	graph.mu.Lock()
	type publishResult struct {
		value    any
		panicked bool
	}
	published := make(chan publishResult, 1)
	go func() {
		value, panicked := cleanupTestPanic(func() { railshotFlushGlobalDescriptorDependencies(&publication) })
		published <- publishResult{value: value, panicked: panicked}
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
			t.Fatal("owned publisher did not reach the graph-blocked phase")
		}
	}
	closed := make(chan error, 1)
	go func() { closed <- owner.Close() }()
	var closeErr error
	closeTimedOut := false
	select {
	case closeErr = <-closed:
	case <-time.After(5 * time.Second):
		closeTimedOut = true
	}
	if closeTimedOut {
		graph.mu.Unlock()
		select {
		case <-published:
		case <-time.After(5 * time.Second):
			t.Fatal("publisher did not finish after releasing a blocked Close")
		}
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Fatal("container Close did not finish after graph release")
		}
		t.Fatal("container Close blocked on owned descriptor publication")
	}
	if closeErr != nil {
		graph.mu.Unlock()
		select {
		case <-published:
		case <-time.After(5 * time.Second):
		}
		t.Fatal("container Close while publication blocked", closeErr)
	}
	if !owner.resourcesClosed || !ownership.physicalCleanupFinished.Load() || ownership.cleanupCompleted.Load() || !ownership.member.active.Load() || row.phase != rv64GlobalDescriptorPublishing || dependencyTestRefs(producer) != 1 {
		graph.mu.Unlock()
		select {
		case <-published:
		case <-time.After(5 * time.Second):
		}
		t.Fatal("physical teardown did not leave the exact publishing row authoritative")
	}
	graph.mu.Unlock()
	select {
	case result := <-published:
		if result.panicked {
			t.Fatal("publisher panicked after container physical teardown", result.value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("publisher did not finish after graph release")
	}
	global.owner.mu.Lock()
	rows := len(global.owner.descriptorDependencies.rows)
	global.owner.mu.Unlock()
	graph.mu.Lock()
	retired := graph.owners[owner] == nil && ownership.member.receipts == 0
	graph.mu.Unlock()
	if rows != 0 || row.phase != rv64GlobalDescriptorReleased || dependencyTestRefs(producer) != 0 || !ownership.cleanupCompleted.Load() || ownership.member.active.Load() || !retired {
		t.Fatal("late publisher did not complete and retire the exact container member", rows, row.phase, dependencyTestRefs(producer), ownership.cleanupCompleted.Load(), ownership.member.active.Load(), retired)
	}
}

func TestRV64OwnedGlobalDescriptorClosedAdoptionRetirementOverlap(t *testing.T) {
	owner, producer, global, row := rv64OwnedGlobalDescriptorFixture(t, false)
	ownership := rv64ExistingFunctionDependencyOwnership(owner)
	if !owner.retainResourceRootForFinalization() {
		t.Fatal("failed to retain consumer through closed adoption")
	}
	rootHeld := true
	if err := owner.beginInstanceInvocation(); err != nil {
		owner.releaseResourceRoot()
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		owner.endInvocation()
		owner.releaseResourceRoot()
		t.Fatal(err)
	}
	graph := row.receipt.graph
	graph.mu.Lock()
	graphHeld := true
	type terminalResult struct {
		value    any
		panicked bool
	}
	terminal := make(chan terminalResult, 1)
	go func() {
		value, panicked := cleanupTestPanic(owner.endInvocation)
		terminal <- terminalResult{value: value, panicked: panicked}
	}()
	joined := false
	t.Cleanup(func() {
		if graphHeld {
			graph.mu.Unlock()
			graphHeld = false
		}
		if !joined {
			select {
			case <-terminal:
			case <-time.After(5 * time.Second):
				t.Error("timed out joining terminal scheduler")
			}
		}
		if rootHeld {
			owner.releaseResourceRoot()
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		global.owner.mu.Lock()
		phase := row.phase
		global.owner.mu.Unlock()
		if phase == rv64GlobalDescriptorAdopting {
			break
		}
		if time.Now().After(deadline) {
			graph.mu.Unlock()
			graphHeld = false
			t.Fatal("closed adopter did not reach graph-blocked phase")
		}
	}
	var retire globalDescriptorDependencyBatch
	global.owner.mu.Lock()
	exact := global.owner.retained[producer] == row.mapState
	if exact {
		delete(global.owner.retained, producer)
		railshotRetireGlobalDescriptorDependencyLocked(global.owner, producer, row.mapState, &retire)
	}
	global.owner.mu.Unlock()
	if !exact {
		graph.mu.Unlock()
		graphHeld = false
		t.Fatal("fixture did not retain the exact descriptor acquisition")
	}
	railshotFlushGlobalDescriptorDependencies(&retire)
	global.owner.mu.Lock()
	retireRequested := row.retireRequested && row.phase == rv64GlobalDescriptorAdopting
	global.owner.mu.Unlock()
	graph.mu.Unlock()
	graphHeld = false
	select {
	case result := <-terminal:
		joined = true
		if result.panicked {
			t.Fatal("terminal scheduler panicked after retirement overlap", result.value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("terminal scheduler did not finish after graph release")
	}
	global.owner.mu.Lock()
	rows := len(global.owner.descriptorDependencies.rows)
	global.owner.mu.Unlock()
	if !retireRequested || row.phase != rv64GlobalDescriptorReleased || rows != 0 || dependencyTestRefs(producer) != 0 || graph.live != 0 || ownership.member.outgoing.Load() != 0 {
		t.Fatal("retirement overlap did not consume the adopted row", retireRequested, row.phase, rows, dependencyTestRefs(producer), graph.live, ownership.member.outgoing.Load())
	}
	owner.releaseResourceRoot()
	rootHeld = false
}

func TestRV64ClosedAdoptionDrainsPublicationAfterCallbackPanic(t *testing.T) {
	owner, producer, global, retiring := rv64OwnedGlobalDescriptorFixture(t, false)
	ownership := rv64ExistingFunctionDependencyOwnership(owner)
	queuedProducer, err := Instantiate(owner.c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = queuedProducer.Close() }) })
	// This private Go-backed holder uses the real descriptor bootstrap and graph
	// publication path; it is not an admitted mapped Table export.
	table := &Table{owner: &tableOwner{instance: owner, elementType: ValFuncRef}}
	owner.lifeMu.Lock()
	railshotLinkOwnedTableDescriptorHolderLocked(owner, table)
	owner.lifeMu.Unlock()
	state, queued, _ := rv64TableDescriptorTestRecord(t, table, queuedProducer, retiring.receipt.graph)
	t.Cleanup(func() {
		var batch tableDescriptorDependencyBatch
		table.mu.Lock()
		if table.retained[queuedProducer] == state {
			delete(table.retained, queuedProducer)
			railshotRetireTableDescriptorDependencyLocked(table, queuedProducer, state, &batch)
		}
		table.mu.Unlock()
		_, _ = cleanupTestPanic(func() { railshotFlushTableDescriptorDependencies(&batch) })
	})

	global.owner.mu.Lock()
	if global.owner.retained[producer] != retiring.mapState {
		global.owner.mu.Unlock()
		t.Fatal("retiring row lost exact map state")
	}
	delete(global.owner.retained, producer)
	var retirement globalDescriptorDependencyBatch
	railshotRetireGlobalDescriptorDependencyLocked(global.owner, producer, retiring.mapState, &retirement)
	global.owner.mu.Unlock()

	acquired := make(chan struct{})
	publishProceed := make(chan struct{})
	published := make(chan struct {
		value    any
		panicked bool
	}, 1)
	go func() {
		value, panicked := cleanupTestPanic(func() {
			queued.publishWithAcquireHook(func() {
				close(acquired)
				<-publishProceed
			})
		})
		published <- struct {
			value    any
			panicked bool
		}{value, panicked}
	}()
	var publishOnce sync.Once
	publishJoined := false
	callbackProceed := make(chan struct{})
	var callbackOnce sync.Once
	t.Cleanup(func() {
		publishOnce.Do(func() { close(publishProceed) })
		callbackOnce.Do(func() { close(callbackProceed) })
		if !publishJoined {
			select {
			case <-published:
			case <-time.After(5 * time.Second):
				t.Error("timed out joining delayed publisher")
			}
		}
	})
	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("delayed publisher did not acquire its receipt")
	}
	callbackStarted := make(chan struct{})
	marker := new(int)
	producer.referenceLifetime().afterPhysicalRelease(func() {
		close(callbackStarted)
		<-callbackProceed
		panic(marker)
	})
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	if !owner.retainResourceRootForFinalization() {
		t.Fatal("failed to retain scheduler owner")
	}
	rootHeld := true
	t.Cleanup(func() {
		if rootHeld {
			owner.releaseResourceRoot()
		}
	})
	type closeResult struct {
		err      error
		value    any
		panicked bool
	}
	closed := make(chan closeResult, 1)
	go func() {
		var err error
		value, panicked := cleanupTestPanic(func() { err = owner.Close() })
		closed <- closeResult{err: err, value: value, panicked: panicked}
	}()
	closeJoined := false
	t.Cleanup(func() {
		publishOnce.Do(func() { close(publishProceed) })
		callbackOnce.Do(func() { close(callbackProceed) })
		if !closeJoined {
			select {
			case <-closed:
			case <-time.After(5 * time.Second):
				t.Error("timed out joining terminal Close worker")
			}
		}
	})
	select {
	case <-callbackStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("initial adoption sweep did not reach physical callback")
	}
	publishOnce.Do(func() { close(publishProceed) })
	select {
	case result := <-published:
		publishJoined = true
		if result.panicked {
			t.Fatal("delayed publisher panicked", result.value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("delayed publisher did not notify the running scheduler")
	}
	callbackOnce.Do(func() { close(callbackProceed) })
	var closeOutcome closeResult
	select {
	case closeOutcome = <-closed:
		closeJoined = true
	case <-time.After(5 * time.Second):
		t.Fatal("terminal scheduler did not drain requested publication")
	}
	waited := owner.WaitClosed(context.Background())
	if closeOutcome.panicked || !errors.Is(closeOutcome.err, ErrCallbackPanic) || waited != closeOutcome.err {
		t.Fatal("synchronous Close result lost or duplicated initial sweep panic", closeOutcome.err, closeOutcome.value, closeOutcome.panicked, waited)
	}
	table.mu.Lock()
	typed := queued.phase == rv64TableDescriptorTyped
	table.mu.Unlock()
	ownership.closedAdoptionMu.Lock()
	idle := !ownership.closedAdoptionRunning && !ownership.closedAdoptionRequested && ownership.closedAdoptionResult != nil
	ownership.closedAdoptionMu.Unlock()
	if !typed || !idle || retiring.phase != rv64GlobalDescriptorReleased {
		t.Fatal("queued publication was stranded after first sweep panic", typed, idle, retiring.phase)
	}
	var queuedRetirement tableDescriptorDependencyBatch
	table.mu.Lock()
	if table.retained[queuedProducer] != state {
		table.mu.Unlock()
		t.Fatal("queued row lost its exact retained state")
	}
	delete(table.retained, queuedProducer)
	railshotRetireTableDescriptorDependencyLocked(table, queuedProducer, state, &queuedRetirement)
	table.mu.Unlock()
	railshotFlushTableDescriptorDependencies(&queuedRetirement)
	owner.releaseResourceRoot()
	rootHeld = false
}

func TestRV64OwnedNumericGlobalDoesNotLinkDescriptorHolder(t *testing.T) {
	in := &Instance{}
	owner := &globalOwner{instance: in, typ: ValI32}
	in.lifeMu.Lock()
	railshotLinkOwnedGlobalDescriptorHolderLocked(in, owner)
	in.lifeMu.Unlock()
	if owner.descriptorDependencies.holder != nil || owner.descriptorDependencies.nextOwned != nil || in.importState.Load() != nil {
		t.Fatal("numeric Global allocated an RV64 descriptor holder")
	}
}

func TestRV64OwnedGlobalDescriptorsContinueAfterFirstCallbackPanic(t *testing.T) {
	compiled := globalValueAccessCompile(t, false, false)
	owner, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = owner.Close() }) })
	first, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = first.Close() }) })
	second, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = second.Close() }) })
	immutable, err := owner.ExportedGlobalObject("reference")
	if err != nil {
		t.Fatal(err)
	}
	mutable := globalValueAccessOwnedCell(t, owner)
	for _, fixture := range []struct {
		global   *Global
		producer *Instance
	}{{immutable, first}, {mutable, second}} {
		descriptor, ok := fixture.producer.localFuncrefDescriptor(0)
		if !ok {
			t.Fatal("mapped producer descriptor")
		}
		fixture.global.owner.mu.Lock()
		writeGlobalObject(fixture.global, ValFuncRef, descriptor)
		fixture.global.owner.mu.Unlock()
		if !fixture.global.retainProducerInstance(fixture.producer) {
			t.Fatal("owned Global retain")
		}
	}
	ownership := rv64ExistingFunctionDependencyOwnership(owner)
	count := 0
	for current := ownership.ownedGlobalDescriptors.Load(); current != nil; current = current.descriptorDependencies.nextOwned {
		count++
	}
	if count != 2 {
		t.Fatal("owned Global holder count", count)
	}
	marker := new(int)
	firstCallbacks, secondCallbacks := 0, 0
	first.referenceLifetime().afterPhysicalRelease(func() { firstCallbacks++; panic(marker) })
	second.referenceLifetime().afterPhysicalRelease(func() { secondCallbacks++ })
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); !errors.Is(err, ErrCallbackPanic) {
		t.Fatal("first callback panic was not reported", err)
	}
	if firstCallbacks != 1 || secondCallbacks != 1 || !first.resourcesClosed || !second.resourcesClosed || first.hasPhysicalResources() || second.hasPhysicalResources() || !ownership.physicalCleanupFinished.Load() || !ownership.cleanupCompleted.Load() {
		t.Fatal("callback panic skipped a later owned Global", firstCallbacks, secondCallbacks, first.resourcesClosed, second.resourcesClosed, ownership.physicalCleanupFinished.Load(), ownership.cleanupCompleted.Load())
	}
	for _, global := range []*Global{immutable, mutable} {
		global.owner.mu.Lock()
		rows := len(global.owner.descriptorDependencies.rows)
		global.owner.mu.Unlock()
		if rows != 0 {
			t.Fatal("owned Global retained a completed row", rows)
		}
	}
}

func TestRV64ClosedComponentCompletesAfterTypedAdoptingHandoff(t *testing.T) {
	compiled := globalValueAccessCompile(t, false, false)
	a, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Instantiate(compiled)
	if err != nil {
		a.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = a.Close() }) })
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = b.Close() }) })
	g, err := newRV64FunctionDependencyGraph(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	aOwnership := rv64FunctionDependencyOwnershipFor(a)
	bOwnership := rv64FunctionDependencyOwnershipFor(b)
	if err := g.register(&aOwnership.member); err != nil {
		t.Fatal(err)
	}
	if err := g.register(&bOwnership.member); err != nil {
		t.Fatal(err)
	}
	if err := b.beginInstanceInvocation(); err != nil {
		t.Fatal(err)
	}
	bLease := true
	t.Cleanup(func() {
		if bLease {
			b.endInvocation()
		}
	})
	bToA := dependencyTestAcquire(t, g, &aOwnership.member)
	if typed, err := bToA.adopt(&bOwnership.member, dependencyFunction); !typed || err != nil {
		t.Fatal(typed, err)
	}
	bOwnership.functions.push(functionDependencyRoot{producer: a, receipt: bToA})
	bOwnership.managed = true
	b.endInvocation()
	bLease = false

	global := globalValueAccessOwnedCell(t, a)
	aMapState, aRow, publication := rv64GlobalDescriptorTestRecord(t, global.owner, b, g)
	t.Cleanup(func() {
		var retire globalDescriptorDependencyBatch
		global.owner.mu.Lock()
		if global.owner.descriptorDependencies.rows[aMapState] == aRow {
			if global.owner.retained[b] == aMapState {
				delete(global.owner.retained, b)
			}
			railshotRetireGlobalDescriptorDependencyLocked(global.owner, b, aMapState, &retire)
		}
		global.owner.mu.Unlock()
		var cleanup resourceCleanup
		cleanup.run(func() { railshotFlushGlobalDescriptorDependencies(&retire) })
		cleanup.run(func() { bOwnership.functions.drain(bOwnership) })
		cleanup.finish()
	})
	if !a.retainResourceRootForFinalization() {
		t.Fatal("failed to retain closed owner until the late row is published")
	}
	aRoot := true
	t.Cleanup(func() {
		if aRoot {
			a.releaseResourceRoot()
		}
	})
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a.releaseResourceRoot()
	aRoot = false
	if a.resourcesClosed || dependencyTestRefs(a) != 1 || !a.closeState.Load().terminalComplete.Load() {
		t.Fatal("closed cycle member did not remain terminal and retained", a.resourcesClosed, dependencyTestRefs(a))
	}

	typed := make(chan struct{})
	resume := make(chan struct{})
	var resumeOnce sync.Once
	resumeAdopter := func() { resumeOnce.Do(func() { close(resume) }) }
	aRow.afterTyped = func() {
		close(typed)
		<-resume
	}
	type publishResult struct {
		value    any
		panicked bool
	}
	published := make(chan publishResult, 1)
	go func() {
		value, panicked := cleanupTestPanic(func() { railshotFlushGlobalDescriptorDependencies(&publication) })
		published <- publishResult{value: value, panicked: panicked}
	}()
	joined := false
	t.Cleanup(func() {
		resumeAdopter()
		if !joined {
			select {
			case <-published:
			case <-time.After(5 * time.Second):
				t.Error("timed out cleaning up graph-Typed adoption handoff")
			}
		}
	})
	select {
	case <-typed:
	case <-time.After(5 * time.Second):
		t.Fatal("late closed adoption did not reach graph-Typed handoff")
	}
	aCallbacks, bCallbacks := 0, 0
	callbackEntered := make(chan struct{})
	resumeCallback := make(chan struct{})
	var callbackOnce sync.Once
	resumePhysical := func() { callbackOnce.Do(func() { close(resumeCallback) }) }
	a.referenceLifetime().afterPhysicalRelease(func() {
		aCallbacks++
		close(callbackEntered)
		<-resumeCallback
	})
	b.referenceLifetime().afterPhysicalRelease(func() { bCallbacks++ })
	closed := make(chan error, 1)
	go func() { closed <- b.Close() }()
	closeJoined := false
	t.Cleanup(func() {
		resumePhysical()
		if !closeJoined {
			select {
			case <-closed:
			case <-time.After(5 * time.Second):
				t.Error("timed out cleaning up component Close worker")
			}
		}
	})
	select {
	case <-callbackEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("component teardown did not reach the blocked physical callback")
	}
	coordinator := aOwnership.componentRelease.Load()
	if coordinator == nil || coordinator != bOwnership.componentRelease.Load() || !coordinator.published.Load() {
		t.Fatal("typed/adopting overlap lost its published coordinator")
	}
	completionFailed := make(chan struct{})
	resumeCompletion := make(chan struct{})
	var completionOnce sync.Once
	resumeCheck := func() { completionOnce.Do(func() { close(resumeCompletion) }) }
	coordinator.release.afterFailedCompletion = func() {
		close(completionFailed)
		<-resumeCompletion
	}
	t.Cleanup(resumeCheck)
	resumePhysical()
	select {
	case <-completionFailed:
	case <-time.After(5 * time.Second):
		t.Fatal("component release did not reach its final failed completion check")
	}
	if !coordinator.physicalJournalsComplete() || aCallbacks != 1 || bCallbacks != 1 || !aRow.retireRequested || aRow.phase != rv64GlobalDescriptorAdopting {
		t.Fatal("typed/adopting overlap lost callback or persistent coordinator", coordinator, aCallbacks, bCallbacks, aRow.retireRequested, aRow.phase)
	}
	resumeAdopter()
	select {
	case result := <-published:
		joined = true
		if result.panicked {
			t.Fatal("late closed adopter panicked after component claim", result.value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("late closed adopter did not hand completion to the component coordinator")
	}
	resumeCheck()
	select {
	case err := <-closed:
		closeJoined = true
		if err != nil {
			t.Fatal("component Close failed after completion handoff", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("component Close did not observe the completion handoff")
	}
	if aOwnership.componentRelease.Load() != nil || bOwnership.componentRelease.Load() != nil || !aOwnership.cleanupCompleted.Load() || !bOwnership.cleanupCompleted.Load() || aOwnership.member.active.Load() || bOwnership.member.active.Load() || g.live != 0 || dependencyTestRefs(a) != 0 || dependencyTestRefs(b) != 0 {
		t.Fatal("typed/adopting handoff stranded component guards or roots")
	}
}

func TestRV64OwnedGlobalDescriptorClosedAdoptionFormsCollectibleCycle(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			compiled := globalValueAccessCompile(t, false, artifact)
			a, err := Instantiate(compiled)
			if err != nil {
				t.Fatal(err)
			}
			b, err := Instantiate(compiled)
			if err != nil {
				a.Close()
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = a.Close() }) })
			t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = b.Close() }) })
			aGlobal := globalValueAccessOwnedCell(t, a)
			bGlobal := globalValueAccessOwnedCell(t, b)
			install := func(global *Global, producer *Instance) *rv64GlobalDescriptorDependency {
				descriptor, ok := producer.localFuncrefDescriptor(0)
				if !ok {
					t.Fatal("mapped cycle descriptor")
				}
				global.owner.mu.Lock()
				writeGlobalObject(global, ValFuncRef, descriptor)
				global.owner.mu.Unlock()
				if !global.retainProducerInstance(producer) {
					t.Fatal("owned Global cycle retain")
				}
				global.owner.mu.Lock()
				state := global.owner.retained[producer]
				row := global.owner.descriptorDependencies.rows[state]
				global.owner.mu.Unlock()
				if row == nil || row.phase != rv64GlobalDescriptorPending {
					t.Fatal("owned Global cycle lacks Pending row")
				}
				return row
			}
			aRow := install(aGlobal, b)
			bRow := install(bGlobal, a)
			t.Cleanup(func() {
				_, _ = cleanupTestPanic(func() {
					var aBatch, bBatch globalDescriptorDependencyBatch
					aGlobal.owner.mu.Lock()
					if aGlobal.owner.retained[b] == aRow.mapState {
						delete(aGlobal.owner.retained, b)
						railshotRetireGlobalDescriptorDependencyLocked(aGlobal.owner, b, aRow.mapState, &aBatch)
					}
					aGlobal.owner.mu.Unlock()
					bGlobal.owner.mu.Lock()
					if bGlobal.owner.retained[a] == bRow.mapState {
						delete(bGlobal.owner.retained, a)
						railshotRetireGlobalDescriptorDependencyLocked(bGlobal.owner, a, bRow.mapState, &bBatch)
					}
					bGlobal.owner.mu.Unlock()
					var cleanup resourceCleanup
					cleanup.run(func() { railshotFlushGlobalDescriptorDependencies(&aBatch) })
					cleanup.run(func() { railshotFlushGlobalDescriptorDependencies(&bBatch) })
					cleanup.finish()
				})
			})
			invocations := false
			delayedCallbacks := 0
			if artifact {
				if err := a.beginInstanceInvocation(); err != nil {
					t.Fatal(err)
				}
				if err := b.beginInstanceInvocation(); err != nil {
					a.endInvocation()
					t.Fatal(err)
				}
				invocations = true
				t.Cleanup(func() {
					if invocations {
						a.endInvocation()
						b.endInvocation()
					}
				})
				a.referenceLifetime().afterPhysicalRelease(func() {
					delayedCallbacks++
					panic("delayed component callback")
				})
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			if err := b.Close(); err != nil {
				t.Fatal(err)
			}
			if artifact {
				if aRow.phase != rv64GlobalDescriptorPending || bRow.phase != rv64GlobalDescriptorPending {
					t.Fatal("active invocation allowed premature closed adoption", aRow.phase, bRow.phase)
				}
				a.endInvocation()
				if a.resourcesClosed || b.resourcesClosed {
					t.Fatal("first delayed terminal owner collected before its peer was eligible")
				}
				b.endInvocation()
				invocations = false
				if delayedCallbacks != 1 || a.WaitClosed(nil) != nil || b.WaitClosed(nil) != nil || !errors.Is(b.waitTerminalClose(), ErrCallbackPanic) {
					t.Fatal("delayed terminal collector lost callback ownership", delayedCallbacks, a.WaitClosed(nil), b.WaitClosed(nil), b.waitTerminalClose())
				}
			}
			aOwnership := rv64ExistingFunctionDependencyOwnership(a)
			bOwnership := rv64ExistingFunctionDependencyOwnership(b)
			if !a.resourcesClosed || !b.resourcesClosed || aRow.phase != rv64GlobalDescriptorReleased || bRow.phase != rv64GlobalDescriptorReleased || dependencyTestRefs(a) != 0 || dependencyTestRefs(b) != 0 || !aOwnership.cleanupCompleted.Load() || !bOwnership.cleanupCompleted.Load() || aOwnership.member.active.Load() || bOwnership.member.active.Load() {
				t.Fatal("genuine owned Global cycle did not automatically converge")
			}
		})
	}
}

func TestRV64DelayedTerminalCollectionBlocksWaiterAndOwnsCallbackError(t *testing.T) {
	rt := globalValueAccessRuntime(t)
	compiled := globalValueAccessCompile(t, false, false)
	a := globalValueAccessInstantiate(t, rt, compiled, nil)
	b := globalValueAccessInstantiate(t, rt, compiled, nil)
	aGlobal := globalValueAccessOwnedCell(t, a)
	bGlobal := globalValueAccessOwnedCell(t, b)
	install := func(global *Global, producer *Instance) *rv64GlobalDescriptorDependency {
		descriptor, ok := producer.localFuncrefDescriptor(0)
		if !ok {
			t.Fatal("mapped cycle descriptor")
		}
		global.owner.mu.Lock()
		writeGlobalObject(global, ValFuncRef, descriptor)
		global.owner.mu.Unlock()
		if !global.retainProducerInstance(producer) {
			t.Fatal("owned Global cycle retain")
		}
		global.owner.mu.Lock()
		state := global.owner.retained[producer]
		row := global.owner.descriptorDependencies.rows[state]
		global.owner.mu.Unlock()
		if row == nil || row.phase != rv64GlobalDescriptorPending {
			t.Fatal("owned Global cycle lacks Pending row")
		}
		return row
	}
	aRow := install(aGlobal, b)
	bRow := install(bGlobal, a)
	t.Cleanup(func() {
		_, _ = cleanupTestPanic(func() {
			var aBatch, bBatch globalDescriptorDependencyBatch
			aGlobal.owner.mu.Lock()
			if aGlobal.owner.retained[b] == aRow.mapState {
				delete(aGlobal.owner.retained, b)
				railshotRetireGlobalDescriptorDependencyLocked(aGlobal.owner, b, aRow.mapState, &aBatch)
			}
			aGlobal.owner.mu.Unlock()
			bGlobal.owner.mu.Lock()
			if bGlobal.owner.retained[a] == bRow.mapState {
				delete(bGlobal.owner.retained, a)
				railshotRetireGlobalDescriptorDependencyLocked(bGlobal.owner, a, bRow.mapState, &bBatch)
			}
			bGlobal.owner.mu.Unlock()
			var cleanup resourceCleanup
			cleanup.run(func() { railshotFlushGlobalDescriptorDependencies(&aBatch) })
			cleanup.run(func() { railshotFlushGlobalDescriptorDependencies(&bBatch) })
			cleanup.finish()
		})
	})

	if err := a.beginInvocation(); err != nil {
		t.Fatal(err)
	}
	aLease := true
	if err := b.beginInvocation(); err != nil {
		a.endInvocation()
		t.Fatal(err)
	}
	bLease := true
	t.Cleanup(func() {
		if aLease {
			a.endInvocation()
		}
		if bLease {
			b.endInvocation()
		}
	})

	callbackEntered := make(chan struct{})
	releaseCallback := make(chan struct{})
	var releaseOnce sync.Once
	releasePhysical := func() { releaseOnce.Do(func() { close(releaseCallback) }) }
	type invocationOutcome struct {
		value    any
		panicked bool
	}
	type terminalWaiter struct {
		name            string
		result          chan error
		started, joined bool
		err             error
	}
	type invocationWorker struct {
		name            string
		result          chan invocationOutcome
		started, joined bool
		outcome         invocationOutcome
	}
	aTerminal := &terminalWaiter{name: "a", result: make(chan error, 1)}
	bTerminal := &terminalWaiter{name: "b", result: make(chan error, 1)}
	aEnded := &invocationWorker{name: "a", result: make(chan invocationOutcome, 1)}
	bEnded := &invocationWorker{name: "b", result: make(chan invocationOutcome, 1)}
	t.Cleanup(func() {
		releasePhysical()
		for _, worker := range []*invocationWorker{aEnded, bEnded} {
			if worker.started && !worker.joined {
				select {
				case worker.outcome = <-worker.result:
					worker.joined = true
				case <-time.After(5 * time.Second):
					t.Errorf("timed out joining final invocation worker %s", worker.name)
				}
			}
		}
		for _, waiter := range []*terminalWaiter{aTerminal, bTerminal} {
			if waiter.started && !waiter.joined {
				select {
				case waiter.err = <-waiter.result:
					waiter.joined = true
				case <-time.After(5 * time.Second):
					t.Errorf("timed out joining terminal waiter %s", waiter.name)
				}
			}
		}
	})
	a.referenceLifetime().afterPhysicalRelease(func() {
		close(callbackEntered)
		<-releaseCallback
		panic("delayed terminal callback")
	})
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if a.closeState.Load().terminalDone == nil || b.closeState.Load().terminalDone == nil {
		t.Fatal("runtime-backed close lacks real terminal wait channels")
	}
	aTerminal.started = true
	go func() { aTerminal.result <- a.waitTerminalClose() }()
	bTerminal.started = true
	go func() { bTerminal.result <- b.waitTerminalClose() }()
	awaitTerminalWait(t, aTerminal.result)
	awaitTerminalWait(t, bTerminal.result)

	// Hold the genuine Runtime store notification after finalizing is claimed.
	// B's last invocation then publishes terminal readiness while A remains in
	// that transient state, forcing the durable finalizer-exit retry path.
	rt.refStore.mu.Lock()
	var storeOnce sync.Once
	releaseStore := func() { storeOnce.Do(rt.refStore.mu.Unlock) }
	t.Cleanup(releaseStore)
	aLease = false
	aEnded.started = true
	go func() {
		value, panicked := cleanupTestPanic(a.endInvocation)
		aEnded.result <- invocationOutcome{value: value, panicked: panicked}
	}()
	waitFinalizing := func(in *Instance) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			in.lifeMu.Lock()
			finalizing := in.finalizing
			in.lifeMu.Unlock()
			if finalizing {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("terminal owner did not enter its finalization scan")
			}
			runtime.Gosched()
		}
	}
	waitFinalizing(a)
	if a.resourcesClosed || b.resourcesClosed {
		t.Fatal("first terminal owner collected before its peer became ready")
	}
	bLease = false
	bEnded.started = true
	go func() {
		value, panicked := cleanupTestPanic(b.endInvocation)
		bEnded.result <- invocationOutcome{value: value, panicked: panicked}
	}()
	waitFinalizing(b)
	releaseStore()
	select {
	case <-callbackEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("delayed component callback did not start")
	}
	earlyTerminal := 0
	for _, waiter := range []*terminalWaiter{aTerminal, bTerminal} {
		select {
		case waiter.err = <-waiter.result:
			waiter.joined = true
			if waiter.err != nil {
				t.Fatalf("terminal waiter %s returned an early error: %v", waiter.name, waiter.err)
			}
			earlyTerminal++
		default:
		}
	}
	if earlyTerminal == 2 {
		t.Fatal("both terminal waiters returned before the owned callback completed")
	}
	if a.WaitClosed(nil) != nil || b.WaitClosed(nil) != nil {
		t.Fatal("delayed callback mutated the published logical Close result", a.WaitClosed(nil), b.WaitClosed(nil))
	}
	releasePhysical()
	invocationPanics := make(map[string]any)
	for _, worker := range []*invocationWorker{aEnded, bEnded} {
		if !worker.joined {
			select {
			case worker.outcome = <-worker.result:
				worker.joined = true
			case <-time.After(5 * time.Second):
				t.Fatalf("final invocation %s did not finish after callback release", worker.name)
			}
		}
		if worker.outcome.panicked {
			invocationPanics[worker.name] = worker.outcome.value
		}
	}
	if len(invocationPanics) != 0 {
		t.Fatal("final invocation propagated a captured callback panic", invocationPanics)
	}
	terminalErrors := 0
	terminalUnexpected := make(map[string]error)
	for _, waiter := range []*terminalWaiter{aTerminal, bTerminal} {
		if !waiter.joined {
			select {
			case waiter.err = <-waiter.result:
				waiter.joined = true
			case <-time.After(5 * time.Second):
				t.Fatalf("terminal waiter %s did not finish after callback release", waiter.name)
			}
		}
		if errors.Is(waiter.err, ErrCallbackPanic) {
			terminalErrors++
		} else if waiter.err != nil {
			terminalUnexpected[waiter.name] = waiter.err
		}
	}
	if len(terminalUnexpected) != 0 {
		t.Fatal("terminal waiters returned unexpected errors", terminalUnexpected)
	}
	if terminalErrors != 1 {
		t.Fatal("delayed callback error did not belong to exactly one terminal owner", terminalErrors)
	}
	if a.WaitClosed(nil) != nil || b.WaitClosed(nil) != nil || !a.resourcesClosed || !b.resourcesClosed || aRow.phase != rv64GlobalDescriptorReleased || bRow.phase != rv64GlobalDescriptorReleased {
		t.Fatal("delayed terminal collection did not converge with immutable logical results")
	}
}
