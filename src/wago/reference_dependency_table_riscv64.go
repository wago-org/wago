//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

type tableDependencyAttachments struct{ rows *rv64TableDependencyRows }
type rv64TableDependencyState uint8

const (
	rv64TableDependencyAttached rv64TableDependencyState = iota
	rv64TableDependencyReleasing
	rv64TableDependencyReleased
)

// Counted table storage is distinct from the owners of descriptors in its rows.
// Stable records keep the actual acquired store even after later token egress.
type rv64TableDependencyRow struct {
	table            *Table
	store            *referenceStore
	snapshot         rv64TableDependencySnapshot
	root             functionDependencyRoot
	state            rv64TableDependencyState
	transferred      bool
	counterCommitted bool
	receiptIdentity  uint64
}
type rv64TableDependencyRows struct {
	mu       sync.Mutex
	consumer *Instance
	rows     importDedup[*rv64TableDependencyRow]
}
type rv64TableDependencySnapshot struct {
	owner          *tableOwner
	producer       *Instance
	descriptor     uintptr
	extent         int
	capacity       uint32
	elementType    ValType
	exact          ValueTypeDescriptor
	types          []DefinedTypeDescriptor
	hasExact       bool
	declaredHasMax bool
	addr64         bool
	store          *referenceStore
}

// The caller holds owner.mu; current length and entry contents remain mutable.
// Capacity and backing metadata describe the fixed storage reservation.
func rv64SnapshotTableDependency(table *Table, owner *tableOwner, acquiring bool) (rv64TableDependencySnapshot, error) {
	table.mu.Lock()
	defer table.mu.Unlock()
	if table.owner != owner || owner.closed || table.closed || len(table.desc) < 8 {
		return rv64TableDependencySnapshot{}, errors.New("table attachment storage identity is invalid")
	}
	if acquiring {
		if err := owner.validateImporterCountLocked(); err != nil {
			return rv64TableDependencySnapshot{}, err
		}
	}
	capacity := binary.LittleEndian.Uint32(table.desc[4:])
	entryBytes := uint64(8)
	if owner.elementType == ValFuncRef {
		entryBytes = runtime.TableEntryBytes
	}
	if 8+uint64(capacity)*entryBytes > uint64(len(table.desc)) {
		return rv64TableDependencySnapshot{}, errors.New("table attachment capacity exceeds descriptor storage")
	}
	return rv64TableDependencySnapshot{owner: owner, producer: owner.instance, descriptor: offHeapSlicePtr(table.desc), extent: len(table.desc), capacity: capacity, elementType: owner.elementType, exact: owner.valueType, types: owner.types, hasExact: owner.hasValueType, declaredHasMax: owner.declaredHasMax, addr64: owner.addr64, store: owner.store}, nil
}
func (snapshot rv64TableDependencySnapshot) matches(current rv64TableDependencySnapshot) bool {
	sameTypes := len(snapshot.types) == len(current.types)
	if sameTypes && len(snapshot.types) != 0 {
		sameTypes = &snapshot.types[0] == &current.types[0]
	}
	return snapshot.owner == current.owner && snapshot.producer == current.producer && snapshot.descriptor == current.descriptor && snapshot.extent == current.extent && snapshot.capacity == current.capacity && snapshot.elementType == current.elementType && snapshot.exact == current.exact && sameTypes && snapshot.hasExact == current.hasExact && snapshot.declaredHasMax == current.declaredHasMax && snapshot.addr64 == current.addr64 && snapshot.store == current.store
}

func railshotPrepareTableDependencies(b *instanceBuilder, cleanup *resourceCleanup) error {
	for index := 0; index < b.c.tableImportCount(); index++ {
		def, _ := b.c.tableImportAt(index)
		table, ok := b.imports.table(b.c.tableImportBindingKey(index))
		if !ok || table == nil {
			return fmt.Errorf("missing imported table %q", def.Key)
		}
		exact, err := b.c.tableExactType(index)
		if err != nil {
			return fmt.Errorf("imported table %q exact type: %w", def.Key, err)
		}
		if err = rv64AcquireTableDependency(&b.tableAttachments, table, b.c.tableElementType(index), exact, b.c.Types, b.opts.store, b.collector, def.Addr64, nil, cleanup); err != nil {
			return fmt.Errorf("imported table %q: %w", def.Key, err)
		}
	}
	return nil
}

// Binding performs no graph/root acquisition or release. The constructor owns
// its native lease here; every successful row was acquired before that lease.
func railshotBindTableImport(a *tableImportAttachments, table *Table, elementType ValType, exact ValueTypeDescriptor, types []DefinedTypeDescriptor, store *referenceStore, collector *gc.Collector, addr64 bool) error {
	rows := a.dependencies.rows
	if rows == nil {
		return errors.New("table binding has no managed acquisition")
	}
	rows.mu.Lock()
	var row *rv64TableDependencyRow
	rows.rows.each(func(candidate *rv64TableDependencyRow) {
		if candidate.table == table && candidate.state == rv64TableDependencyAttached {
			row = candidate
		}
	})
	var acquiredStore *referenceStore
	var snapshot rv64TableDependencySnapshot
	var producer *Instance
	if row != nil {
		acquiredStore, snapshot, producer = row.store, row.snapshot, row.root.producer
	}
	rows.mu.Unlock()
	if row == nil || acquiredStore != store {
		return errors.New("table binding has no exact store acquisition")
	}
	if err := table.validateImportWithRetainedOwner(elementType, exact, types, store, collector, addr64, producer); err != nil {
		return err
	}
	owner := snapshot.owner
	owner.mu.Lock()
	current, err := rv64SnapshotTableDependency(table, owner, false)
	if err == nil && !snapshot.matches(current) {
		err = errors.New("table attachment identity or type changed before binding")
	}
	owner.mu.Unlock()
	return err
}

func rv64AcquireTableDependency(a *tableImportAttachments, table *Table, elementType ValType, exact ValueTypeDescriptor, types []DefinedTypeDescriptor, store *referenceStore, collector *gc.Collector, addr64 bool, graph *dependencyGraph, cleanup *resourceCleanup) error {
	if a.set.n != 0 && a.store != store {
		return errors.New("table import attachments use inconsistent reference stores")
	}
	if a.set.contains(table) {
		return railshotBindTableImport(a, table, elementType, exact, types, store, collector, addr64)
	}
	if err := table.validateImportWithCollector(elementType, exact, types, store, collector, addr64); err != nil {
		return err
	}
	owner := table.owner
	owner.mu.Lock()
	snapshot, err := rv64SnapshotTableDependency(table, owner, true)
	owner.mu.Unlock()
	if err != nil {
		return err
	}
	if snapshot.producer == nil {
		if err := table.attachImporterWithExpectedHost(elementType, exact, types, store, collector, addr64, owner); err != nil {
			return err
		}
		// Publish the exact successful claim before any fallible revalidation.
		// Constructor rollback drains this row conditionally; a corrupted owner
		// or topology cannot erase the only record of an unconsumed obligation.
		a.recordTableDependency(table, store, snapshot, functionDependencyRoot{})
		// The acquired host count prevents Close. Capture its fixed identity after
		// the actual topology claim; reaching maxInt is valid after acquisition.
		owner.mu.Lock()
		current, err := rv64SnapshotTableDependency(table, owner, false)
		owner.mu.Unlock()
		if err != nil || !snapshot.matches(current) {
			if err != nil {
				return err
			}
			return errors.New("host table attachment identity changed during acquisition")
		}
		return nil
	}
	if graph == nil {
		graph = rv64FunctionDependencyGraph()
	}
	member, err := rv64RegisteredFunctionDependencyMember(graph, snapshot.producer)
	var receipt dependencyReceipt
	if err == nil {
		receipt, err = graph.acquirePending(member, false)
	}
	if errors.Is(err, errDependencyCapacity) {
		if !snapshot.producer.retainResourceRoot() {
			return errors.New("table owner instance is closed")
		}
	} else if err != nil {
		return fmt.Errorf("table dependency: %w", err)
	}
	root := functionDependencyRoot{producer: snapshot.producer, receipt: receipt}
	// Successful real retention permits this exact producer's later logical
	// Close, while reference/storage validation stays outside the graph lock.
	err = table.validateImportWithRetainedOwner(elementType, exact, types, store, collector, addr64, root.producer)
	owner.mu.Lock()
	if err == nil {
		var current rv64TableDependencySnapshot
		current, err = rv64SnapshotTableDependency(table, owner, true)
		if err == nil && !snapshot.matches(current) {
			err = errors.New("table attachment identity or type changed during acquisition")
		}
	}
	if err == nil {
		owner.importers++
	}
	owner.mu.Unlock()
	if err != nil {
		cleanup.run(root.release)
		return err
	}
	a.recordTableDependency(table, store, snapshot, root)
	return nil
}
func (a *tableImportAttachments) recordTableDependency(table *Table, store *referenceStore, snapshot rv64TableDependencySnapshot, root functionDependencyRoot) {
	if a.dependencies.rows == nil {
		a.dependencies.rows = &rv64TableDependencyRows{}
	}
	a.dependencies.rows.rows.push(&rv64TableDependencyRow{table: table, store: store, snapshot: snapshot, root: root, receiptIdentity: root.receipt.identity})
	if a.set.n == 0 {
		a.store = store
	}
	a.set.push(table)
}

func railshotHasBuilderTableDependencies(a *tableImportAttachments) bool {
	return a.dependencies.rows != nil
}

func railshotDrainBuilderTableDependencies(a *tableImportAttachments) bool {
	rows := a.dependencies.rows
	if rows == nil {
		return false
	}
	var cleanup resourceCleanup
	cleanup.run(rows.drain)
	if rows.complete() {
		a.dependencies.rows = nil
		a.set.reset()
		a.store = nil
	}
	cleanup.finish()
	return true
}

func railshotInstallTableDependencies(in *Instance, a *tableImportAttachments) {
	rows := a.dependencies.rows
	if rows == nil {
		return
	}
	ownership := rv64FunctionDependencyOwnershipFor(in)
	rows.consumer = in // builder owns exclusion until this exact pointer moves
	ownership.tables.Store(rows)
	a.dependencies.rows = nil
	a.set.reset()
	a.store = nil
}

func rv64ExistingTableDependencyRows(in *Instance) *rv64TableDependencyRows {
	if ownership := rv64ExistingFunctionDependencyOwnership(in); ownership != nil {
		return ownership.tables.Load()
	}
	return nil
}

func railshotAdoptTableDependencies(in *Instance, nativeLeaseReleased bool) error {
	rows := rv64ExistingTableDependencyRows(in)
	if rows == nil {
		return nil
	}
	if !nativeLeaseReleased {
		return errors.New("table dependency adoption still owns construction native lease")
	}
	var result error
	rows.rows.each(func(row *rv64TableDependencyRow) {
		if result != nil {
			return
		}
		rows.mu.Lock()
		receipt, attached := row.root.receipt, row.state == rv64TableDependencyAttached
		rows.mu.Unlock()
		if !attached || receipt.graph == nil {
			return
		}
		member, err := rv64RegisteredFunctionDependencyMember(receipt.graph, in)
		if errors.Is(err, errDependencyCapacity) {
			return // terminal holder keeps this exact pending/external acquisition
		}
		if err != nil {
			result = err
			return
		}
		_, err = receipt.adopt(member, dependencyTableStorage)
		if rows.adoptionReleaseWon(row, receipt, err) {
			return // graph claim precedes counter consumption; reuse cannot resurrect a row
		}
		result = err
	})
	return result
}

func (rows *rv64TableDependencyRows) adoptionReleaseWon(row *rv64TableDependencyRow, receipt dependencyReceipt, err error) bool {
	if !errors.Is(err, errDependencyState) && !errors.Is(err, errDependencyIdentity) {
		return false
	}
	rows.mu.Lock()
	defer rows.mu.Unlock()
	return (row.state == rv64TableDependencyReleased || row.counterCommitted) && row.receiptIdentity == receipt.identity
}

// The terminal owner already holds the resourcesClosed claim; builder cleanup
// has no published consumer. Every other release first acquires a physical root
// on the consumer, covering typed, anonymous and host-only rows alike. Thus a
// terminal claim cannot overtake a counter decrement or a producer callback.
func (rows *rv64TableDependencyRows) release(row *rv64TableDependencyRow, terminal, transferred bool) {
	consumer := rows.consumer
	if consumer != nil && !terminal && !consumer.retainResourceRootForFinalization() {
		return // terminal owner drains; never fall back to ordinary detach
	}
	var cleanup resourceCleanup
	rows.mu.Lock()
	// An adopted graph edge remains the closed callee's dependency while
	// an open downstream importer retains it. Only graph collection or the
	// terminal physical drain can retire that edge safely.
	keepClosedEdge := transferred && !terminal && consumer != nil && row.root.receipt.graph != nil && consumer.invocationState.Load()&instanceInvocationClosed != 0 && !rv64StorageFreeCallable(consumer.c)
	if row.state != rv64TableDependencyAttached || keepClosedEdge {
		rows.mu.Unlock()
		if consumer != nil && !terminal {
			cleanup.run(consumer.releaseResourceRoot)
		}
		cleanup.finish()
		return
	}
	row.state, row.transferred = rv64TableDependencyReleasing, transferred
	root := row.root
	rows.mu.Unlock()
	counterConsumed := false
	consumeCounter := func() bool {
		counterConsumed = row.table.consumeImporterCounter(row.store, row.snapshot.owner, root.producer, row.snapshot.elementType)
		if counterConsumed {
			rows.mu.Lock()
			row.counterCommitted = true
			rows.mu.Unlock()
		}
		return counterConsumed
	}
	if root.receipt.graph != nil {
		cleanup.run(func() {
			if !root.receipt.releaseWithCounter(consumeCounter) {
				panic("table dependency lost its exact producer receipt")
			}
		})
	} else {
		cleanup.run(func() {
			if !consumeCounter() {
				panic("table dependency lost its exact importer-counter obligation")
			}
		})
		if counterConsumed && root.producer != nil {
			cleanup.run(root.release)
		}
	}
	rows.mu.Lock()
	if counterConsumed {
		row.root = functionDependencyRoot{}
		row.state = rv64TableDependencyReleased
	} else {
		// A failed internal claim did not consume this counter. Preserve the
		// concrete obligation and prevent successful registry retirement.
		row.state, row.transferred = rv64TableDependencyAttached, false
	}
	rows.mu.Unlock()
	if counterConsumed {
		cleanup.run(func() { railshotFinishCompactExternHostTableDetach(row.table) })
	}
	if consumer != nil && !terminal {
		cleanup.run(consumer.releaseResourceRoot)
	}
	cleanup.finish()
}

func (rows *rv64TableDependencyRows) drain() {
	var cleanup resourceCleanup
	rows.rows.each(func(row *rv64TableDependencyRow) {
		cleanup.run(func() { rows.release(row, true, false) })
	})
	cleanup.finish()
}

func (rows *rv64TableDependencyRows) complete() bool {
	rows.mu.Lock()
	defer rows.mu.Unlock()
	complete := true
	rows.rows.each(func(row *rv64TableDependencyRow) { complete = complete && row.state == rv64TableDependencyReleased })
	return complete
}

func railshotDrainInstanceTableDependencies(in *Instance) bool {
	rows := rv64ExistingTableDependencyRows(in)
	if rows == nil {
		return false
	}
	rows.drain()
	return true
}

func railshotTransferTableDependency(in *Instance, table *Table) bool {
	rows := rv64ExistingTableDependencyRows(in)
	if rows == nil {
		return false
	}
	rows.rows.each(func(row *rv64TableDependencyRow) {
		if row.table == table {
			rows.release(row, false, true)
		}
	})
	return true // managed holder is authoritative, including released/missing rows
}

func railshotTransferredTableDependency(in *Instance, table *Table) (bool, bool) {
	rows := rv64ExistingTableDependencyRows(in)
	if rows == nil {
		return false, false
	}
	rows.mu.Lock()
	defer rows.mu.Unlock()
	transferred := false
	rows.rows.each(func(row *rv64TableDependencyRow) {
		if row.table == table {
			transferred = row.transferred
		}
	})
	return transferred, true
}
