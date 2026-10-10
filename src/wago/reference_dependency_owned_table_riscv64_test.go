//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func rv64OwnedTableDescriptorInstall(t *testing.T, table *Table, producer *Instance, local int) {
	t.Helper()
	descriptor, ok := producer.localFuncrefDescriptor(local)
	if !ok {
		t.Fatal("mapped producer descriptor")
	}
	offset := int(descriptor - uint64(offHeapSlicePtr(producer.funcRefDescs)))
	nativeExecutionMu.Lock()
	table.mu.Lock()
	copy(table.desc[8:8+coreruntime.TableEntryBytes], producer.funcRefDescs[offset:offset+coreruntime.TableEntryBytes])
	table.mu.Unlock()
	nativeExecutionMu.Unlock()
	if !table.retainProducerInstance(producer) {
		t.Fatal("owned Table did not retain mapped producer")
	}
}

// The existing owned-table bridge fixture installs one authenticated mapped
// producer descriptor under the real native/Table locks. This isolates the
// durable instance-side holder without adding a producer-to-owner import edge.
func rv64OwnedTableDescriptorFixture(t *testing.T, artifact bool) (owner, producer *Instance, table *Table, row *rv64TableDescriptorDependency) {
	t.Helper()
	code := rv64TableStageCompile(t, "owned-table-descriptor-holder", rv64ForeignTableCallModule([]wasm.ValType{wasm.I32}, 7, false, false), artifact)
	var err error
	owner, err = Instantiate(code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = owner.Close() }) })
	producer, err = Instantiate(code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = producer.Close() }) })
	table, err = owner.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	if again, err := owner.ExportedTable("table"); err != nil || again != table {
		t.Fatal("lazy export alias replaced the owned Table", err)
	}
	rv64InstallForeignTableTargetAt(t, owner, producer, 1)
	table.mu.Lock()
	state := table.retained[producer]
	row = table.owner.descriptorDependencies.rows[state]
	holder := table.owner.descriptorDependencies.holder
	table.mu.Unlock()
	ownership := rv64ExistingFunctionDependencyOwnership(owner)
	if row == nil || row.phase != rv64TableDescriptorPending || holder == nil || holder != ownership || ownership.ownedTableDescriptors.Load() != table || table.owner.descriptorDependencies.nextOwned != nil {
		t.Fatal("owned Table lacks one authoritative holder/Pending row")
	}
	return owner, producer, table, row
}

func TestRV64OwnedTableDescriptorTerminalCallbackCompletes(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			owner, producer, table, row := rv64OwnedTableDescriptorFixture(t, artifact)
			ownership := rv64ExistingFunctionDependencyOwnership(owner)
			marker := new(int)
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
			table.mu.Lock()
			rows := len(table.owner.descriptorDependencies.rows)
			table.mu.Unlock()
			if row.phase != rv64TableDescriptorReleased || rows != 0 || !producer.resourcesClosed || producer.hasPhysicalResources() || !owner.resourcesClosed || !ownership.physicalCleanupFinished.Load() || !ownership.cleanupCompleted.Load() {
				t.Fatal("owned Table descriptor terminal cleanup incomplete", row.phase, rows, producer.resourcesClosed, owner.resourcesClosed, ownership.physicalCleanupFinished.Load(), ownership.cleanupCompleted.Load())
			}
		})
	}
}

func TestRV64OwnedTableDescriptorDeniedTerminalRetry(t *testing.T) {
	owner, producer, table, row := rv64OwnedTableDescriptorFixture(t, false)
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
	table.mu.Lock()
	rows := len(table.owner.descriptorDependencies.rows)
	table.mu.Unlock()
	if !errors.Is(first, ErrCallbackPanic) || first != owner.closeState.Load().result || owner.WaitClosed(nil) != first || !owner.resourcesClosed || !ownership.physicalCleanupFinished.Load() || ownership.cleanupCompleted.Load() || !ownership.member.active.Load() || rows != 1 || row.phase != rv64TableDescriptorPending || dependencyTestRefs(producer) != 1 || producer.resourcesClosed {
		t.Fatal("denied terminal claim lost durable Table holder/root", first, owner.resourcesClosed, ownership.physicalCleanupFinished.Load(), ownership.cleanupCompleted.Load(), rows, row.phase, dependencyTestRefs(producer), producer.resourcesClosed)
	}
	receipt.graph.mu.Lock()
	record, _ = receipt.graph.recordLocked(receipt.identity)
	record.state = dependencyPending
	receipt.graph.mu.Unlock()
	faultOwned = false
	second := owner.Close()
	table.mu.Lock()
	rows = len(table.owner.descriptorDependencies.rows)
	table.mu.Unlock()
	memberGraph := ownership.member.graph.Load()
	memberGraph.mu.Lock()
	retired := memberGraph.owners[owner] == nil && ownership.member.receipts == 0
	memberGraph.mu.Unlock()
	if second != first || owner.closeState.Load().result != first || owner.WaitClosed(nil) != first || rows != 0 || row.phase != rv64TableDescriptorReleased || dependencyTestRefs(producer) != 0 || !producer.resourcesClosed || producer.hasPhysicalResources() || !ownership.cleanupCompleted.Load() || ownership.member.active.Load() || !retired {
		t.Fatal("repaired repeated Close did not preserve result and complete Table retirement", second, rows, row.phase, dependencyTestRefs(producer), producer.resourcesClosed, ownership.cleanupCompleted.Load())
	}
}

func TestRV64OwnedTableDescriptorPublishingCompletesAfterPhysicalTeardown(t *testing.T) {
	code := rv64TableStageCompile(t, "owned-table-descriptor-publishing", rv64ForeignTableCallModule([]wasm.ValType{wasm.I32}, 7, false, false), false)
	owner, err := Instantiate(code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = owner.Close() }) })
	producer, err := Instantiate(code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = producer.Close() }) })
	table, err := owner.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	ownership := rv64ExistingFunctionDependencyOwnership(owner)
	graph, err := newRV64FunctionDependencyGraph(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.register(&ownership.member); err != nil {
		t.Fatal("register exact container owner", err)
	}
	_, row, publication := rv64TableDescriptorTestRecord(t, table, producer, graph)

	graph.mu.Lock()
	type publishResult struct {
		value    any
		panicked bool
	}
	published := make(chan publishResult, 1)
	go func() {
		value, panicked := cleanupTestPanic(func() { railshotFlushTableDescriptorDependencies(&publication) })
		published <- publishResult{value: value, panicked: panicked}
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
			t.Fatal("owned Table publisher did not reach the graph-blocked phase")
		}
	}
	closed := make(chan error, 1)
	go func() { closed <- owner.Close() }()
	var closeErr error
	select {
	case closeErr = <-closed:
	case <-time.After(5 * time.Second):
		graph.mu.Unlock()
		select {
		case <-published:
		case <-time.After(5 * time.Second):
		}
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
		}
		t.Fatal("container Close blocked on owned Table descriptor publication")
	}
	if closeErr != nil {
		graph.mu.Unlock()
		select {
		case <-published:
		case <-time.After(5 * time.Second):
		}
		t.Fatal("container Close while publication blocked", closeErr)
	}
	if !owner.resourcesClosed || !ownership.physicalCleanupFinished.Load() || ownership.cleanupCompleted.Load() || !ownership.member.active.Load() || row.phase != rv64TableDescriptorPublishing || dependencyTestRefs(producer) != 1 {
		graph.mu.Unlock()
		select {
		case <-published:
		case <-time.After(5 * time.Second):
		}
		t.Fatal("physical teardown did not leave the exact publishing Table row authoritative")
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
	table.mu.Lock()
	rows := len(table.owner.descriptorDependencies.rows)
	table.mu.Unlock()
	graph.mu.Lock()
	retired := graph.owners[owner] == nil && ownership.member.receipts == 0
	graph.mu.Unlock()
	if rows != 0 || row.phase != rv64TableDescriptorReleased || dependencyTestRefs(producer) != 0 || !ownership.cleanupCompleted.Load() || ownership.member.active.Load() || !retired {
		t.Fatal("late publisher did not complete and retire the exact Table member", rows, row.phase, dependencyTestRefs(producer), ownership.cleanupCompleted.Load(), ownership.member.active.Load(), retired)
	}
}

func TestRV64OwnedNumericTableDoesNotLinkDescriptorHolder(t *testing.T) {
	in := &Instance{}
	table := &Table{owner: &tableOwner{instance: in, elementType: ValI32}}
	in.lifeMu.Lock()
	railshotLinkOwnedTableDescriptorHolderLocked(in, table)
	in.lifeMu.Unlock()
	if table.owner.descriptorDependencies.holder != nil || table.owner.descriptorDependencies.nextOwned != nil || in.importState.Load() != nil {
		t.Fatal("numeric Table allocated an RV64 descriptor holder")
	}
}

func TestRV64OwnedTableDescriptorsContinueAfterFirstCallbackPanic(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			code := rv64TableStageCompile(t, "owned-table-descriptor-two-holders", rv64ForeignTableCallModule([]wasm.ValType{wasm.I32}, 7, false, false), artifact)
			owner, err := Instantiate(code)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = owner.Close() }) })
			firstProducer, err := Instantiate(code)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = firstProducer.Close() }) })
			secondProducer, err := Instantiate(code)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = secondProducer.Close() }) })
			first, err := owner.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			// Current RV64 admission intentionally rejects multiple local tables.
			// Add a private second holder to the genuine mapped owner's immutable
			// chain so callback continuation is tested without widening admission.
			second := &Table{desc: make([]byte, 8+coreruntime.TableEntryBytes), owner: &tableOwner{instance: owner, elementType: ValFuncRef}, next: owner.table}
			binary.LittleEndian.PutUint32(second.desc, 1)
			binary.LittleEndian.PutUint32(second.desc[4:], 1)
			owner.lifeMu.Lock()
			railshotLinkOwnedTableDescriptorHolderLocked(owner, second)
			owner.table = second
			owner.lifeMu.Unlock()
			rv64OwnedTableDescriptorInstall(t, first, firstProducer, 0)
			rv64OwnedTableDescriptorInstall(t, second, secondProducer, 1)
			ownership := rv64ExistingFunctionDependencyOwnership(owner)
			count := 0
			for table := ownership.ownedTableDescriptors.Load(); table != nil; table = table.owner.descriptorDependencies.nextOwned {
				count++
			}
			if count != 2 {
				t.Fatal("owned Table holder count", count)
			}
			marker := new(int)
			firstCallbacks, secondCallbacks := 0, 0
			// Export order makes second the first terminal chain node. Its panic
			// must not prevent the later first-table receipt from draining.
			secondProducer.referenceLifetime().afterPhysicalRelease(func() { secondCallbacks++; panic(marker) })
			firstProducer.referenceLifetime().afterPhysicalRelease(func() { firstCallbacks++ })
			if err := firstProducer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := secondProducer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := owner.Close(); !errors.Is(err, ErrCallbackPanic) {
				t.Fatal("first Table callback panic was not reported", err)
			}
			if firstCallbacks != 1 || secondCallbacks != 1 || !firstProducer.resourcesClosed || !secondProducer.resourcesClosed || firstProducer.hasPhysicalResources() || secondProducer.hasPhysicalResources() || !ownership.physicalCleanupFinished.Load() || !ownership.cleanupCompleted.Load() {
				t.Fatal("callback panic skipped a later owned Table", firstCallbacks, secondCallbacks, firstProducer.resourcesClosed, secondProducer.resourcesClosed, ownership.physicalCleanupFinished.Load(), ownership.cleanupCompleted.Load())
			}
			for _, table := range []*Table{first, second} {
				table.mu.Lock()
				rows := len(table.owner.descriptorDependencies.rows)
				table.mu.Unlock()
				if rows != 0 {
					t.Fatal("owned Table retained a completed row", rows)
				}
			}
		})
	}
}

func TestRV64OwnedTableDescriptorClosedAdoptionFormsCollectibleCycle(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			code := rv64TableStageCompile(t, "owned-table-descriptor-cycle", rv64ForeignTableCallModule([]wasm.ValType{wasm.I32}, 7, false, false), artifact)
			a, err := Instantiate(code)
			if err != nil {
				t.Fatal(err)
			}
			b, err := Instantiate(code)
			if err != nil {
				a.Close()
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = a.Close() }) })
			t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = b.Close() }) })
			aTable, err := a.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			bTable, err := b.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			rv64InstallForeignTableTargetAt(t, a, b, 1)
			rv64InstallForeignTableTargetAt(t, b, a, 1)
			row := func(table *Table, producer *Instance) *rv64TableDescriptorDependency {
				table.mu.Lock()
				state := table.retained[producer]
				result := table.owner.descriptorDependencies.rows[state]
				table.mu.Unlock()
				if result == nil || result.phase != rv64TableDescriptorPending {
					t.Fatal("owned Table cycle lacks Pending row")
				}
				return result
			}
			aRow := row(aTable, b)
			bRow := row(bTable, a)
			t.Cleanup(func() {
				_, _ = cleanupTestPanic(func() {
					var aBatch, bBatch tableDescriptorDependencyBatch
					aTable.mu.Lock()
					if aTable.retained[b] == aRow.mapState {
						delete(aTable.retained, b)
						railshotRetireTableDescriptorDependencyLocked(aTable, b, aRow.mapState, &aBatch)
					}
					aTable.mu.Unlock()
					bTable.mu.Lock()
					if bTable.retained[a] == bRow.mapState {
						delete(bTable.retained, a)
						railshotRetireTableDescriptorDependencyLocked(bTable, a, bRow.mapState, &bBatch)
					}
					bTable.mu.Unlock()
					var cleanup resourceCleanup
					cleanup.run(func() { railshotFlushTableDescriptorDependencies(&aBatch) })
					cleanup.run(func() { railshotFlushTableDescriptorDependencies(&bBatch) })
					cleanup.finish()
				})
			})
			invocations := false
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
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			if err := b.Close(); err != nil {
				t.Fatal(err)
			}
			if artifact {
				if aRow.phase != rv64TableDescriptorPending || bRow.phase != rv64TableDescriptorPending {
					t.Fatal("active invocation allowed premature closed adoption", aRow.phase, bRow.phase)
				}
				a.endInvocation()
				if a.resourcesClosed || b.resourcesClosed {
					t.Fatal("first delayed terminal owner collected before its peer was eligible")
				}
				b.endInvocation()
				invocations = false
			}
			aOwnership := rv64ExistingFunctionDependencyOwnership(a)
			bOwnership := rv64ExistingFunctionDependencyOwnership(b)
			if !a.resourcesClosed || !b.resourcesClosed || aRow.phase != rv64TableDescriptorReleased || bRow.phase != rv64TableDescriptorReleased || dependencyTestRefs(a) != 0 || dependencyTestRefs(b) != 0 || !aOwnership.cleanupCompleted.Load() || !bOwnership.cleanupCompleted.Load() || aOwnership.member.active.Load() || bOwnership.member.active.Load() {
				t.Fatal("genuine owned Table cycle did not automatically converge")
			}
		})
	}
}
