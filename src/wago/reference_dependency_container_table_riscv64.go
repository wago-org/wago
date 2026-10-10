//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
)

// Host funcref-table descriptors are physical acquisitions distinct from their
// counted table-storage ownership. Instance-owned tables stay on the legacy
// path until their durable terminal holder is installed independently.
type tableDescriptorDependencies struct {
	// Protected by the containing Table.mu. The map-state pointer distinguishes
	// an old acquisition from a later replacement for the same producer.
	rows map[*retainedInstanceRoot]*rv64TableDescriptorDependency
}

type rv64TableDescriptorDependencyPhase uint8

const (
	rv64TableDescriptorBootstrap rv64TableDescriptorDependencyPhase = iota
	rv64TableDescriptorPublishing
	rv64TableDescriptorPending
	rv64TableDescriptorAnonymous
	rv64TableDescriptorFailed
	rv64TableDescriptorReleasing
	rv64TableDescriptorReleased
)

type rv64TableDescriptorDependency struct {
	table            *Table
	producer         *Instance
	mapState         *retainedInstanceRoot
	graph            *dependencyGraph // nil selects the process graph; tests inject bounded graphs
	receipt          dependencyReceipt
	receiptIdentity  uint64
	phase            rv64TableDescriptorDependencyPhase
	retireRequested  bool
	releaseCommitted bool
}

type tableDescriptorDependencyBatch struct {
	publications importDedup[*rv64TableDescriptorDependency]
	retirements  importDedup[*rv64TableDescriptorDependency]
	legacy       importDedup[*Instance]
}

// Caller owns the concrete bootstrap and Table.mu. Recording performs no graph
// or lifetime operation; publication runs only after every outer native lock.
func railshotRecordTableDescriptorDependencyLocked(table *Table, producer *Instance, mapState *retainedInstanceRoot, batch *tableDescriptorDependencyBatch) {
	if table.owner == nil || table.owner.instance != nil || table.owner.elementType != ValFuncRef {
		return
	}
	if table.owner.descriptorDependencies.rows == nil {
		table.owner.descriptorDependencies.rows = make(map[*retainedInstanceRoot]*rv64TableDescriptorDependency)
	}
	if table.owner.descriptorDependencies.rows[mapState] != nil {
		panic("duplicate RV64 table descriptor acquisition identity")
	}
	row := &rv64TableDescriptorDependency{table: table, producer: producer, mapState: mapState}
	table.owner.descriptorDependencies.rows[mapState] = row
	if batch != nil {
		batch.publications.add(row)
	}
}

func railshotPublishTableDescriptorDependencyLocked(table *Table, mapState *retainedInstanceRoot, batch *tableDescriptorDependencyBatch) {
	if batch == nil || table.owner == nil {
		return
	}
	if row := table.owner.descriptorDependencies.rows[mapState]; row != nil && (row.phase == rv64TableDescriptorBootstrap || row.phase == rv64TableDescriptorFailed) {
		batch.publications.add(row)
	}
}

func railshotRetireTableDescriptorDependencyLocked(table *Table, producer *Instance, mapState *retainedInstanceRoot, batch *tableDescriptorDependencyBatch) {
	if table.owner == nil {
		batch.legacy.add(producer)
		return
	}
	row := table.owner.descriptorDependencies.rows[mapState]
	if row == nil {
		batch.legacy.add(producer)
		return
	}
	if row.producer != producer || row.mapState != mapState || row.table != table {
		panic("RV64 table descriptor acquisition identity mismatch")
	}
	row.retireRequested = true
	batch.retirements.add(row)
}

func railshotRetryTableDescriptorDependenciesLocked(table *Table, batch *tableDescriptorDependencyBatch) {
	if table.owner == nil {
		return
	}
	for _, row := range table.owner.descriptorDependencies.rows {
		row.retireRequested = true
		batch.retirements.add(row)
	}
}

func railshotFlushTableDescriptorDependencies(batch *tableDescriptorDependencyBatch) {
	var cleanup resourceCleanup
	cleanup.run(func() { batch.publications.drain((*rv64TableDescriptorDependency).publish) })
	cleanup.run(func() { batch.retirements.drain((*rv64TableDescriptorDependency).release) })
	cleanup.run(func() { batch.legacy.drain((*Instance).releaseResourceRoot) })
	cleanup.finish()
}

func (row *rv64TableDescriptorDependency) publish() {
	table := row.table
	table.mu.Lock()
	if row.phase != rv64TableDescriptorBootstrap && row.phase != rv64TableDescriptorFailed {
		table.mu.Unlock()
		return
	}
	row.phase = rv64TableDescriptorPublishing
	table.mu.Unlock()

	receipt, exchange, err := rv64AcquireContainerDescriptorDependency(row.producer, row.graph)
	table.mu.Lock()
	if row.phase != rv64TableDescriptorPublishing || table.owner.descriptorDependencies.rows[row.mapState] != row {
		table.mu.Unlock()
		if exchange {
			receipt.release()
		}
		panic("RV64 table descriptor publication lost its stable row")
	}
	if exchange {
		row.receipt = receipt
		row.receiptIdentity = receipt.identity
		row.phase = rv64TableDescriptorPending
	} else if errors.Is(err, errDependencyCapacity) {
		row.phase = rv64TableDescriptorAnonymous
	} else {
		row.phase = rv64TableDescriptorFailed
	}
	retire := row.retireRequested
	table.mu.Unlock()

	var cleanup resourceCleanup
	if exchange {
		cleanup.run(row.producer.releaseResourceRoot)
	}
	if retire {
		cleanup.run(row.release)
	}
	if err != nil && !errors.Is(err, errDependencyCapacity) {
		cleanup.run(func() { panic(fmt.Sprintf("RV64 table descriptor dependency: %v", err)) })
	}
	cleanup.finish()
}

func (row *rv64TableDescriptorDependency) release() {
	table := row.table
	table.mu.Lock()
	row.retireRequested = true
	previous := row.phase
	switch previous {
	case rv64TableDescriptorPublishing:
		table.mu.Unlock()
		return
	case rv64TableDescriptorReleasing, rv64TableDescriptorReleased:
		table.mu.Unlock()
		return
	case rv64TableDescriptorBootstrap, rv64TableDescriptorPending, rv64TableDescriptorAnonymous, rv64TableDescriptorFailed:
		row.phase = rv64TableDescriptorReleasing
	default:
		table.mu.Unlock()
		panic("RV64 table descriptor dependency has an invalid phase")
	}
	receipt := row.receipt
	table.mu.Unlock()

	claimed := false
	var cleanup resourceCleanup
	if receipt.graph == nil {
		table.mu.Lock()
		if table.owner.descriptorDependencies.rows[row.mapState] == row && row.phase == rv64TableDescriptorReleasing && row.retireRequested && table.retained[row.producer] != row.mapState {
			row.releaseCommitted = true
			claimed = true
		}
		table.mu.Unlock()
		if claimed {
			cleanup.run(row.producer.releaseResourceRoot)
		}
	} else {
		cleanup.run(func() {
			claimed = receipt.releaseWithCounter(func() bool {
				nativeExecutionMu.Lock()
				defer nativeExecutionMu.Unlock()
				table.mu.Lock()
				defer table.mu.Unlock()
				if table.owner.descriptorDependencies.rows[row.mapState] != row || row.phase != rv64TableDescriptorReleasing || !row.retireRequested || table.retained[row.producer] == row.mapState || row.receiptIdentity != receipt.identity {
					return false
				}
				row.releaseCommitted = true
				return true
			})
		})
	}
	cleanup.run(func() {
		table.mu.Lock()
		committed := row.releaseCommitted
		if committed {
			row.phase = rv64TableDescriptorReleased
			delete(table.owner.descriptorDependencies.rows, row.mapState)
			row.receipt = dependencyReceipt{}
		} else {
			row.phase = previous
		}
		table.mu.Unlock()
		if !committed {
			panic("RV64 table descriptor dependency release did not claim its exact row")
		}
	})
	cleanup.finish()
}
