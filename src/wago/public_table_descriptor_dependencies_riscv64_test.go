//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func rv64PublicHostTablePendingDescriptor(t *testing.T, table *Table, producer *Instance) (*retainedInstanceRoot, *rv64TableDescriptorDependency) {
	t.Helper()
	table.mu.Lock()
	state := table.retained[producer]
	row := table.owner.descriptorDependencies.rows[state]
	table.mu.Unlock()
	if state == nil || row == nil || row.producer != producer || row.mapState != state || row.phase != rv64TableDescriptorPending || row.receipt.graph == nil || row.receipt.identity == 0 {
		t.Fatal("host table lacks one exact Pending descriptor receipt")
	}
	row.receipt.graph.mu.Lock()
	record, _ := row.receipt.graph.recordLocked(row.receipt.identity)
	pending := record != nil && record.state == dependencyPending && record.target != nil && record.target.owner == producer && record.consumer == nil && record.kind == 0
	row.receipt.graph.mu.Unlock()
	if !pending {
		t.Fatal("host table descriptor receipt is not Pending/external for the exact producer")
	}
	return state, row
}

func rv64PublicHostTableDescriptorFixture(t *testing.T) (*Table, *Instance, *rv64TableDescriptorDependency) {
	t.Helper()
	table, err := NewTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = table.Close() }) })
	code := tableCounterCompile(t, false, false)
	producer, err := Instantiate(code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = producer.Close() }) })
	descriptor, ok := producer.localFuncrefDescriptor(0)
	if !ok {
		t.Fatal("mapped producer descriptor")
	}
	offset := descriptor - uint64(offHeapSlicePtr(producer.funcRefDescs))
	nativeExecutionMu.Lock()
	copy(table.desc[8:8+coreruntime.TableEntryBytes], producer.funcRefDescs[int(offset):int(offset)+coreruntime.TableEntryBytes])
	binary.LittleEndian.PutUint32(table.desc, 1)
	nativeExecutionMu.Unlock()
	if !table.retainProducerInstance(producer) {
		t.Fatal("host table did not retain mapped producer")
	}
	_, row := rv64PublicHostTablePendingDescriptor(t, table, producer)
	return table, producer, row
}

// Active element initialization writes a real local descriptor while the
// constructor owns native serialization. The constructor records a stable
// Bootstrap and publishes it only after that serialization has ended.
func TestRV64PublicHostTableConstructorDescriptorReceipt(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			code := rv64TableStageCompile(t, "host-table-constructor-descriptor", rv64ImportedTableModule([]wasm.ValType{wasm.I32}, 7, false, true, true, 1), artifact)
			table, err := NewTable(1, 2)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = table.Close() }) })
			producer, err := Instantiate(code, NewImports().Table("env", "table", table))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = producer.Close() }) })
			_, row := rv64PublicHostTablePendingDescriptor(t, table, producer)
			if dependencyTestRefs(producer) != 1 {
				t.Fatal("constructor descriptor did not exchange its bootstrap for one receipt")
			}
			if err := producer.Close(); err != nil || producer.resourcesClosed || dependencyTestRefs(producer) != 1 {
				t.Fatal("constructor descriptor did not retain the logically closed producer", err)
			}
			if err := table.Close(); err != nil {
				t.Fatal(err)
			}
			if row.phase != rv64TableDescriptorReleased || dependencyTestRefs(producer) != 0 || !producer.resourcesClosed || producer.hasPhysicalResources() || table.desc != nil || table.owner.arena != nil {
				t.Fatal("constructor descriptor receipt did not complete host-table teardown")
			}
		})
	}
}

// A native table.set first creates an active-writer Bootstrap. Blocking the
// process graph catches its publication after the invocation has released the
// native lease; completion must leave one exact persistent Pending receipt.
func TestRV64PublicHostTableNativeWriterDescriptorReceipt(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			code := rv64TableStageCompile(t, "host-table-native-writer-descriptor", rv64ImportedTableModule([]wasm.ValType{wasm.I32}, 7, false, false, true, 1, true), artifact)
			table, err := NewTable(1, 2)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = table.Close() }) })
			producer, err := Instantiate(code, NewImports().Table("env", "table", table))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = cleanupTestPanic(func() { _ = producer.Close() }) })
			table.mu.Lock()
			initialRows := len(table.owner.descriptorDependencies.rows)
			table.mu.Unlock()
			if initialRows != 0 || dependencyTestRefs(producer) != 0 {
				t.Fatal("empty constructor retained a descriptor producer")
			}

			graph := rv64FunctionDependencyGraph()
			graph.mu.Lock()
			graphLocked := true
			type outcome struct {
				result []uint64
				err    error
			}
			done := make(chan outcome, 1)
			joined := false
			defer func() {
				if graphLocked {
					graph.mu.Unlock()
				}
				if !joined {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("native writer did not join")
					}
				}
			}()
			go func() {
				result, err := producer.Invoke("clear", 0)
				done <- outcome{result: result, err: err}
			}()

			var state *retainedInstanceRoot
			var row *rv64TableDescriptorDependency
			deadline := time.Now().Add(5 * time.Second)
			for {
				table.mu.Lock()
				state = table.retained[producer]
				row = table.owner.descriptorDependencies.rows[state]
				publishing := state != nil && row != nil && row.phase == rv64TableDescriptorPublishing && state.activeWriters == 0 && state.precise
				table.mu.Unlock()
				if publishing {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("native writer did not advance its Bootstrap to graph-blocked publication")
				}
			}
			if !nativeExecutionMu.TryLock() {
				t.Fatal("table descriptor publication retained the native execution lease")
			}
			nativeExecutionMu.Unlock()
			graph.mu.Unlock()
			graphLocked = false
			var call outcome
			select {
			case call = <-done:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("native writer did not finish after graph publication resumed")
			}
			if call.err != nil || len(call.result) != 1 || call.result[0] != 1 {
				t.Fatal("native table writer result", call.result, call.err)
			}
			publishedState, publishedRow := rv64PublicHostTablePendingDescriptor(t, table, producer)
			if publishedState != state || publishedRow != row || dependencyTestRefs(producer) != 1 {
				t.Fatal("native writer publication replaced its stable Bootstrap identity")
			}
			if err := producer.Close(); err != nil || producer.resourcesClosed {
				t.Fatal("native writer descriptor did not survive logical producer Close", err)
			}
			if err := table.Close(); err != nil {
				t.Fatal(err)
			}
			if row.phase != rv64TableDescriptorReleased || dependencyTestRefs(producer) != 0 || !producer.resourcesClosed || producer.hasPhysicalResources() {
				t.Fatal("native writer descriptor receipt did not complete host-table teardown")
			}
		})
	}
}

func TestRV64PublicHostTableDescriptorReceiptLifecycle(t *testing.T) {
	table, producer, row := rv64PublicHostTableDescriptorFixture(t)
	if err := producer.Close(); err != nil || producer.resourcesClosed || dependencyTestRefs(producer) != 1 {
		t.Fatal("table descriptor did not retain logically closed producer", err)
	}
	if err := table.Close(); err != nil {
		t.Fatal(err)
	}
	if row.phase != rv64TableDescriptorReleased || len(table.owner.descriptorDependencies.rows) != 0 || dependencyTestRefs(producer) != 0 || !producer.resourcesClosed || producer.hasPhysicalResources() || table.desc != nil || table.owner.arena != nil {
		t.Fatal("host table descriptor receipt did not complete physical teardown")
	}
}

func TestRV64PublicHostTableDescriptorDeniedTerminalRetry(t *testing.T) {
	table, producer, row := rv64PublicHostTableDescriptorFixture(t)
	receipt := row.receipt
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
	_, panicked := cleanupTestPanic(func() { _ = table.Close() })
	if !panicked || row.phase != rv64TableDescriptorPending || len(table.owner.descriptorDependencies.rows) != 1 || dependencyTestRefs(producer) != 1 || producer.resourcesClosed || table.desc != nil || table.owner.arena != nil {
		t.Fatal("denied terminal table claim lost its durable row/root")
	}
	receipt.graph.mu.Lock()
	record, _ = receipt.graph.recordLocked(receipt.identity)
	record.state = dependencyPending
	receipt.graph.mu.Unlock()
	faultOwned = false
	if err := table.Close(); err != nil {
		t.Fatal(err)
	}
	if row.phase != rv64TableDescriptorReleased || len(table.owner.descriptorDependencies.rows) != 0 || dependencyTestRefs(producer) != 0 || !producer.resourcesClosed || producer.hasPhysicalResources() {
		t.Fatal("repeated host Table.Close did not repair the exact receipt")
	}
}
