//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"sync"
)

type memoryDependencyAttachments struct {
	rows *rv64MemoryDependencyRows
}

type rv64MemoryDependencyState uint8

const (
	rv64MemoryDependencyAttached rv64MemoryDependencyState = iota
	rv64MemoryDependencyReleasing
	rv64MemoryDependencyReleased
)

// Rows have stable identities across the builder handoff. The holder mutex
// protects counter obligations separately from the receipt's own release claim.
// Released rows retain only the handle/transfer marker; reentry never rescans
// imports or recreates an entry in transferredImportAttachments.
type rv64MemoryDependencyRow struct {
	memory           *Memory
	root             functionDependencyRoot
	state            rv64MemoryDependencyState
	transferred      bool
	counterCommitted bool   // Releasing is reversible until the exact counter commits
	receiptIdentity  uint64 // immutable acquisition generation, retained after drain
}

type rv64MemoryDependencyRows struct {
	mu       sync.Mutex
	consumer *Instance
	rows     importDedup[*rv64MemoryDependencyRow]
}

func memoryDependencyShape(s *memoryState) uint64 {
	mask := memory32ImporterMask
	if s.has(memoryStateAddr64) {
		mask = memoryStateImporterMask << memoryStateImporterShift
	}
	return s.meta &^ mask
}

func railshotAcquireMemoryDependency(a *memoryImportAttachments, memory *Memory, cleanup *resourceCleanup) error {
	return rv64AcquireMemoryDependency(a, memory, nil, cleanup)
}

func rv64AcquireMemoryDependency(a *memoryImportAttachments, memory *Memory, g *dependencyGraph, cleanup *resourceCleanup) error {
	if memory == nil {
		return errors.New("memory is nil")
	}
	s := memory.state.Load()
	if s == nil {
		return errors.New("memory has not been exported for import")
	}
	s.mu.Lock()
	count, err := memory.validateImporterLocked(s)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	producer, backing, shape := s.owner, memory.jm, memoryDependencyShape(s)
	if producer == nil {
		// No graph edge exists for a host memory. Its exact counter still belongs
		// to the row journal and needs consumer lifetime exclusion during release.
		s.setImporterCount(count + 1)
		s.mu.Unlock()
		a.recordMemoryDependency(memory, functionDependencyRoot{})
		return nil
	}
	s.mu.Unlock()

	if g == nil {
		g = rv64FunctionDependencyGraph()
	}
	member, err := rv64RegisteredFunctionDependencyMember(g, producer)
	var receipt dependencyReceipt
	if err == nil {
		receipt, err = g.acquirePending(member, false)
	}
	if errors.Is(err, errDependencyCapacity) {
		if !producer.retainResourceRoot() {
			return errors.New("memory owner instance is closed")
		}
	} else if err != nil {
		return fmt.Errorf("memory dependency: %w", err)
	}
	root := functionDependencyRoot{producer: producer, receipt: receipt}
	s.mu.Lock()
	count, err = memory.validateImporterLocked(s)
	if err == nil && (memory.state.Load() != s || s.owner != producer || memory.jm != backing || memoryDependencyShape(s) != shape) {
		err = errors.New("memory attachment identity or type changed during acquisition")
	}
	if err == nil {
		// Successful real retention linearized producer Close. Do not veto a
		// later logical Close here or perform another producer retain.
		s.setImporterCount(count + 1)
	}
	s.mu.Unlock()
	if err != nil {
		cleanup.run(root.release)
		return err
	}
	a.recordMemoryDependency(memory, root)
	return nil
}

func (a *memoryImportAttachments) recordMemoryDependency(memory *Memory, root functionDependencyRoot) {
	if a.dependencies.rows == nil {
		a.dependencies.rows = &rv64MemoryDependencyRows{}
	}
	a.dependencies.rows.rows.push(&rv64MemoryDependencyRow{memory: memory, root: root, receiptIdentity: root.receipt.identity})
}

func railshotHasBuilderMemoryDependencies(a *memoryImportAttachments) bool {
	return a.dependencies.rows != nil
}

func railshotDrainBuilderMemoryDependencies(a *memoryImportAttachments) bool {
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

func railshotInstallMemoryDependencies(in *Instance, a *memoryImportAttachments) {
	rows := a.dependencies.rows
	if rows == nil {
		return
	}
	ownership := rv64FunctionDependencyOwnershipFor(in)
	rows.consumer = in // builder owns exclusion until this exact pointer moves
	ownership.memories.Store(rows)
	a.dependencies.rows = nil
	a.set.reset()
}

func rv64ExistingMemoryDependencyRows(in *Instance) *rv64MemoryDependencyRows {
	if ownership := rv64ExistingFunctionDependencyOwnership(in); ownership != nil {
		return ownership.memories.Load()
	}
	return nil
}

func railshotAdoptMemoryDependencies(in *Instance, nativeLeaseReleased bool) error {
	rows := rv64ExistingMemoryDependencyRows(in)
	if rows == nil {
		return nil
	}
	if !nativeLeaseReleased {
		return errors.New("memory dependency adoption still owns construction native lease")
	}
	var result error
	rows.rows.each(func(row *rv64MemoryDependencyRow) {
		if result != nil {
			return
		}
		rows.mu.Lock()
		receipt, attached := row.root.receipt, row.state == rv64MemoryDependencyAttached
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
		_, err = receipt.adopt(member, dependencyMemory)
		if rows.adoptionReleaseWon(row, receipt, err) {
			return // graph claim precedes counter consumption; reuse cannot resurrect a row
		}
		result = err
	})
	return result
}

func (rows *rv64MemoryDependencyRows) adoptionReleaseWon(row *rv64MemoryDependencyRow, receipt dependencyReceipt, err error) bool {
	if !errors.Is(err, errDependencyState) && !errors.Is(err, errDependencyIdentity) {
		return false
	}
	rows.mu.Lock()
	defer rows.mu.Unlock()
	return (row.state == rv64MemoryDependencyReleased || row.counterCommitted) && row.receiptIdentity == receipt.identity
}

// The terminal owner already holds the resourcesClosed claim; builder cleanup
// has no published consumer. Every other release first acquires a physical root
// on the consumer, covering typed, anonymous and host-only rows alike. Thus a
// terminal claim cannot overtake a counter decrement or a producer callback.
func (rows *rv64MemoryDependencyRows) release(row *rv64MemoryDependencyRow, terminal, transferred bool) {
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
	if row.state != rv64MemoryDependencyAttached || keepClosedEdge {
		rows.mu.Unlock()
		if consumer != nil && !terminal {
			cleanup.run(consumer.releaseResourceRoot)
		}
		cleanup.finish()
		return
	}
	row.state, row.transferred = rv64MemoryDependencyReleasing, transferred
	root := row.root
	rows.mu.Unlock()
	counterConsumed := false
	consumeCounter := func() bool {
		counterConsumed = row.memory.consumeImporterCounter(root.producer)
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
				panic("memory dependency lost its exact producer receipt")
			}
		})
	} else {
		cleanup.run(func() {
			if !consumeCounter() {
				panic("memory dependency lost its exact importer-counter obligation")
			}
		})
		if counterConsumed && root.producer != nil {
			cleanup.run(root.release)
		}
	}
	rows.mu.Lock()
	if counterConsumed {
		row.root = functionDependencyRoot{}
		row.state = rv64MemoryDependencyReleased
	} else {
		// A failed internal claim did not consume this counter. Preserve the
		// concrete obligation and prevent successful registry retirement.
		row.state, row.transferred = rv64MemoryDependencyAttached, false
	}
	rows.mu.Unlock()
	if consumer != nil && !terminal {
		cleanup.run(consumer.releaseResourceRoot)
	}
	cleanup.finish()
}

func (rows *rv64MemoryDependencyRows) drain() {
	var cleanup resourceCleanup
	rows.rows.each(func(row *rv64MemoryDependencyRow) {
		cleanup.run(func() { rows.release(row, true, false) })
	})
	cleanup.finish()
}

func (rows *rv64MemoryDependencyRows) complete() bool {
	rows.mu.Lock()
	defer rows.mu.Unlock()
	complete := true
	rows.rows.each(func(row *rv64MemoryDependencyRow) { complete = complete && row.state == rv64MemoryDependencyReleased })
	return complete
}

func railshotHasInstanceMemoryDependencies(in *Instance) bool {
	return rv64ExistingMemoryDependencyRows(in) != nil
}

func railshotDrainInstanceMemoryDependencies(in *Instance) bool {
	rows := rv64ExistingMemoryDependencyRows(in)
	if rows == nil {
		return false
	}
	rows.drain()
	return true
}

func railshotTransferMemoryDependency(in *Instance, memory *Memory) bool {
	rows := rv64ExistingMemoryDependencyRows(in)
	if rows == nil {
		return false
	}
	rows.rows.each(func(row *rv64MemoryDependencyRow) {
		if row.memory == memory {
			rows.release(row, false, true)
		}
	})
	return true // managed holder is authoritative, including released/missing rows
}

func railshotTransferredMemoryDependency(in *Instance, memory *Memory) (bool, bool) {
	rows := rv64ExistingMemoryDependencyRows(in)
	if rows == nil {
		return false, false
	}
	rows.mu.Lock()
	defer rows.mu.Unlock()
	transferred := false
	rows.rows.each(func(row *rv64MemoryDependencyRow) {
		if row.memory == memory {
			transferred = row.transferred
		}
	})
	return transferred, true
}
