//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"sync"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type globalDependencyAttachments struct{ rows *rv64GlobalDependencyRows }

type rv64GlobalDependencyState uint8

const (
	rv64GlobalDependencyAttached rv64GlobalDependencyState = iota
	rv64GlobalDependencyReleasing
	rv64GlobalDependencyReleased
)

// Counted cell ownership is separate from frozen descriptor roots. Stable rows
// move from builder to terminal owner without changing acquisition identities.
// Released rows retain their handle and transfer marker to prevent legacy scans.
type rv64GlobalDependencyRow struct {
	global           *Global
	root             functionDependencyRoot
	state            rv64GlobalDependencyState
	transferred      bool
	counterCommitted bool // Releasing is reversible until the exact counter commits
	receiptIdentity  uint64
}

type rv64GlobalDependencyRows struct {
	mu       sync.Mutex
	consumer *Instance
	rows     importDedup[*rv64GlobalDependencyRow]
}

type rv64GlobalDependencySnapshot struct {
	owner    *globalOwner
	producer *Instance
	cell     uintptr
	size     int
	typ      ValType
	mutable  bool
	store    *referenceStore
	exact    ValueTypeDescriptor
	hasExact bool
	types    []DefinedTypeDescriptor
}

// The caller holds owner.mu. Validation and any producer/graph acquisition
// occur outside this lock; only exact cell ownership and count are claimed here.
func rv64SnapshotGlobalDependency(global *Global, owner *globalOwner) (rv64GlobalDependencySnapshot, error) {
	if global.owner != owner || owner.closed || !railshotCompactExternHostGlobalOpen(owner) || len(global.cell) < globalCellSize(owner.typ) || global.Type != owner.typ || global.Mutable != owner.mutable {
		return rv64GlobalDependencySnapshot{}, errors.New("global attachment identity or type is invalid")
	}
	if err := owner.validateImporterCountLocked(); err != nil {
		return rv64GlobalDependencySnapshot{}, err
	}
	return rv64GlobalDependencySnapshot{owner: owner, producer: owner.instance, cell: offHeapSlicePtr(global.cell), size: len(global.cell), typ: owner.typ, mutable: owner.mutable, store: owner.store, exact: owner.valueType, hasExact: owner.hasValueType, types: owner.types}, nil
}

func (snapshot rv64GlobalDependencySnapshot) matches(current rv64GlobalDependencySnapshot) bool {
	sameTypes := len(snapshot.types) == len(current.types)
	if sameTypes && len(snapshot.types) != 0 {
		sameTypes = &snapshot.types[0] == &current.types[0]
	}
	return snapshot.owner == current.owner && snapshot.producer == current.producer && snapshot.cell == current.cell && snapshot.size == current.size && snapshot.typ == current.typ && snapshot.mutable == current.mutable && snapshot.store == current.store && snapshot.exact == current.exact && snapshot.hasExact == current.hasExact && sameTypes
}

func railshotAcquireGlobalDependency(a *globalImportAttachments, global *Global, store *referenceStore, collector *gc.Collector, cleanup *resourceCleanup) error {
	return rv64AcquireGlobalDependency(a, global, store, collector, nil, cleanup)
}

func rv64AcquireGlobalDependency(a *globalImportAttachments, global *Global, store *referenceStore, collector *gc.Collector, graph *dependencyGraph, cleanup *resourceCleanup) error {
	if global == nil || global.owner == nil {
		return errors.New("global has no explicit owner")
	}
	validate := global.validateNumericImport
	if isReferenceValType(global.Type) {
		validate = func() error { return global.validateReferenceImportWithCollector(store, collector) }
	}
	if err := validate(); err != nil {
		return err
	}
	owner := global.owner
	owner.mu.Lock()
	snapshot, err := rv64SnapshotGlobalDependency(global, owner)
	if err != nil {
		owner.mu.Unlock()
		return err
	}
	producer := snapshot.producer
	if producer == nil {
		// Host globals own a count but no graph edge. Ownerless value descriptors
		// never enter this acquisition path; they materialize private cells.
		owner.importers++
		owner.mu.Unlock()
		a.recordGlobalDependency(global, functionDependencyRoot{})
		return nil
	}
	owner.mu.Unlock()
	if graph == nil {
		graph = rv64FunctionDependencyGraph()
	}
	member, err := rv64RegisteredFunctionDependencyMember(graph, producer)
	var receipt dependencyReceipt
	if err == nil {
		receipt, err = graph.acquirePending(member, false)
	}
	if errors.Is(err, errDependencyCapacity) {
		if !producer.retainResourceRoot() {
			return errors.New("global owner instance is closed")
		}
	} else if err != nil {
		return fmt.Errorf("global dependency: %w", err)
	}
	root := functionDependencyRoot{producer: producer, receipt: receipt}
	// Reference validation may inspect current contents and store/domain metadata.
	// It must not nest those locks with either the graph or the global counter.
	err = validate()
	owner.mu.Lock()
	if err == nil {
		var current rv64GlobalDependencySnapshot
		current, err = rv64SnapshotGlobalDependency(global, owner)
		if err == nil && !snapshot.matches(current) {
			err = errors.New("global attachment identity or type changed during acquisition")
		}
	}
	if err == nil {
		// Successful real retention linearizes logical Close. Increment once,
		// without a second retainer or a later logical-close veto.
		owner.importers++
	}
	owner.mu.Unlock()
	if err != nil {
		cleanup.run(root.release)
		return err
	}
	a.recordGlobalDependency(global, root)
	return nil
}

func (a *globalImportAttachments) recordGlobalDependency(global *Global, root functionDependencyRoot) {
	if a.dependencies.rows == nil {
		a.dependencies.rows = &rv64GlobalDependencyRows{}
	}
	a.dependencies.rows.rows.push(&rv64GlobalDependencyRow{global: global, root: root, receiptIdentity: root.receipt.identity})
}

func railshotHasBuilderGlobalDependencies(a *globalImportAttachments) bool {
	return a.dependencies.rows != nil
}

func railshotDrainBuilderGlobalDependencies(a *globalImportAttachments) bool {
	rows := a.dependencies.rows
	if rows == nil {
		return false
	}
	var cleanup resourceCleanup
	cleanup.run(rows.drain)
	if rows.complete() {
		a.dependencies.rows = nil
		a.set.reset()
	}
	cleanup.finish()
	return true
}

func railshotInstallGlobalDependencies(in *Instance, a *globalImportAttachments) {
	rows := a.dependencies.rows
	if rows == nil {
		return
	}
	ownership := rv64FunctionDependencyOwnershipFor(in)
	rows.consumer = in // builder owns exclusion until this exact pointer moves
	ownership.globals.Store(rows)
	a.dependencies.rows = nil
	a.set.reset()
}

func rv64ExistingGlobalDependencyRows(in *Instance) *rv64GlobalDependencyRows {
	if ownership := rv64ExistingFunctionDependencyOwnership(in); ownership != nil {
		return ownership.globals.Load()
	}
	return nil
}

func railshotAdoptGlobalDependencies(in *Instance, nativeLeaseReleased bool) error {
	rows := rv64ExistingGlobalDependencyRows(in)
	if rows == nil {
		return nil
	}
	if !nativeLeaseReleased {
		return errors.New("global dependency adoption still owns construction native lease")
	}
	var result error
	rows.rows.each(func(row *rv64GlobalDependencyRow) {
		if result != nil {
			return
		}
		rows.mu.Lock()
		receipt, attached := row.root.receipt, row.state == rv64GlobalDependencyAttached
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
		_, err = receipt.adopt(member, dependencyGlobalCell)
		if rows.adoptionReleaseWon(row, receipt, err) {
			return // graph claim precedes counter consumption; reuse cannot resurrect a row
		}
		result = err
	})
	return result
}

func (rows *rv64GlobalDependencyRows) adoptionReleaseWon(row *rv64GlobalDependencyRow, receipt dependencyReceipt, err error) bool {
	if !errors.Is(err, errDependencyState) && !errors.Is(err, errDependencyIdentity) {
		return false
	}
	rows.mu.Lock()
	defer rows.mu.Unlock()
	return (row.state == rv64GlobalDependencyReleased || row.counterCommitted) && row.receiptIdentity == receipt.identity
}

// The terminal owner already holds the resourcesClosed claim; builder cleanup
// has no published consumer. Every other release first acquires a physical root
// on the consumer, covering typed, anonymous and host-only rows alike. Thus a
// terminal claim cannot overtake a counter decrement or a producer callback.
func (rows *rv64GlobalDependencyRows) release(row *rv64GlobalDependencyRow, terminal, transferred bool) {
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
	if row.state != rv64GlobalDependencyAttached || keepClosedEdge {
		rows.mu.Unlock()
		if consumer != nil && !terminal {
			cleanup.run(consumer.releaseResourceRoot)
		}
		cleanup.finish()
		return
	}
	row.state, row.transferred = rv64GlobalDependencyReleasing, transferred
	root := row.root
	rows.mu.Unlock()
	counterConsumed := false
	consumeCounter := func() bool {
		counterConsumed = row.global.consumeImporterCounter(root.producer)
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
				panic("global dependency lost its exact producer receipt")
			}
		})
	} else {
		cleanup.run(func() {
			if !consumeCounter() {
				panic("global dependency lost its exact importer-counter obligation")
			}
		})
		if counterConsumed && root.producer != nil {
			cleanup.run(root.release)
		}
	}
	rows.mu.Lock()
	if counterConsumed {
		row.root = functionDependencyRoot{}
		row.state = rv64GlobalDependencyReleased
	} else {
		// A failed internal claim did not consume this counter. Preserve the
		// concrete obligation and prevent successful registry retirement.
		row.state, row.transferred = rv64GlobalDependencyAttached, false
	}
	rows.mu.Unlock()
	if consumer != nil && !terminal {
		cleanup.run(consumer.releaseResourceRoot)
	}
	cleanup.finish()
}

func (rows *rv64GlobalDependencyRows) drain() {
	var cleanup resourceCleanup
	rows.rows.each(func(row *rv64GlobalDependencyRow) {
		cleanup.run(func() { rows.release(row, true, false) })
	})
	cleanup.finish()
}

func (rows *rv64GlobalDependencyRows) complete() bool {
	rows.mu.Lock()
	defer rows.mu.Unlock()
	complete := true
	rows.rows.each(func(row *rv64GlobalDependencyRow) { complete = complete && row.state == rv64GlobalDependencyReleased })
	return complete
}

func railshotDrainInstanceGlobalDependencies(in *Instance) bool {
	rows := rv64ExistingGlobalDependencyRows(in)
	if rows == nil {
		return false
	}
	rows.drain()
	return true
}

func railshotTransferGlobalDependency(in *Instance, global *Global) bool {
	rows := rv64ExistingGlobalDependencyRows(in)
	if rows == nil {
		return false
	}
	rows.rows.each(func(row *rv64GlobalDependencyRow) {
		if row.global == global {
			rows.release(row, false, true)
		}
	})
	return true // managed holder is authoritative, including released/missing rows
}

func railshotTransferredGlobalDependency(in *Instance, global *Global) (bool, bool) {
	rows := rv64ExistingGlobalDependencyRows(in)
	if rows == nil {
		return false, false
	}
	rows.mu.Lock()
	defer rows.mu.Unlock()
	transferred := false
	rows.rows.each(func(row *rv64GlobalDependencyRow) {
		if row.global == global {
			transferred = row.transferred
		}
	})
	return transferred, true
}
