//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
)

// Mutable funcref-container descriptors are physical acquisitions distinct
// from their counted global-cell storage. Rows stay Pending/external in this
// checkpoint: a writer's invocation does not authorize adoption onto an owner.
type globalDescriptorDependencies struct {
	// Protected by the containing globalOwner.mu. The map-state pointer, rather
	// than the producer alone, distinguishes an old acquisition from a later
	// replacement for the same producer.
	rows map[*retainedInstanceRoot]*rv64GlobalDescriptorDependency
	// Instance-owned holders are linked under Instance.lifeMu before g.owner is
	// published. The chain and exact authority survive globalCells teardown.
	nextOwned *globalOwner
	holder    *rv64FunctionDependencyOwnership
}

type rv64GlobalDescriptorDependencyPhase uint8

const (
	rv64GlobalDescriptorBootstrap rv64GlobalDescriptorDependencyPhase = iota
	rv64GlobalDescriptorPublishing
	rv64GlobalDescriptorPending
	rv64GlobalDescriptorAnonymous
	rv64GlobalDescriptorFailed
	rv64GlobalDescriptorReleasing
	rv64GlobalDescriptorReleased
)

type rv64GlobalDescriptorDependency struct {
	owner            *globalOwner
	producer         *Instance
	mapState         *retainedInstanceRoot
	graph            *dependencyGraph // nil selects the process graph; tests inject bounded graphs
	receipt          dependencyReceipt
	receiptIdentity  uint64
	phase            rv64GlobalDescriptorDependencyPhase
	retireRequested  bool
	releaseCommitted bool
}

type globalDescriptorDependencyBatch struct {
	publications importDedup[*rv64GlobalDescriptorDependency]
	retirements  importDedup[*rv64GlobalDescriptorDependency]
	legacy       importDedup[*Instance]
}

// The caller already owns the row's concrete bootstrap. Record its immutable
// identity under the container lock, but perform no graph/lifetime operation.
func railshotRecordGlobalDescriptorDependencyLocked(owner *globalOwner, producer *Instance, mapState *retainedInstanceRoot, batch *globalDescriptorDependencyBatch) {
	// Instance-owned cells are torn down with their instance and need an
	// authoritative instance-side holder before receipts can safely survive a
	// denied terminal claim. Unbound synthetic owners remain on the legacy path.
	if owner.instance != nil && (owner.descriptorDependencies.holder == nil || owner.descriptorDependencies.holder.member.owner != owner.instance) {
		return
	}
	if owner.descriptorDependencies.rows == nil {
		owner.descriptorDependencies.rows = make(map[*retainedInstanceRoot]*rv64GlobalDescriptorDependency)
	}
	if owner.descriptorDependencies.rows[mapState] != nil {
		panic("duplicate RV64 global descriptor acquisition identity")
	}
	row := &rv64GlobalDescriptorDependency{owner: owner, producer: producer, mapState: mapState}
	owner.descriptorDependencies.rows[mapState] = row
	batch.publications.add(row)
}

func railshotPublishGlobalDescriptorDependencyLocked(owner *globalOwner, mapState *retainedInstanceRoot, batch *globalDescriptorDependencyBatch) {
	if row := owner.descriptorDependencies.rows[mapState]; row != nil && (row.phase == rv64GlobalDescriptorBootstrap || row.phase == rv64GlobalDescriptorFailed) {
		batch.publications.add(row)
	}
}

// Retirement only marks the exact map acquisition. Publication, graph claims,
// and physical release are completed after native/container locks are dropped.
func railshotRetireGlobalDescriptorDependencyLocked(owner *globalOwner, producer *Instance, mapState *retainedInstanceRoot, batch *globalDescriptorDependencyBatch) {
	row := owner.descriptorDependencies.rows[mapState]
	if row == nil {
		// Synthetic/legacy rows have no receipt identity and retain the historical
		// exact physical-root drain outside the container lock.
		batch.legacy.add(producer)
		return
	}
	if row.producer != producer || row.mapState != mapState || row.owner != owner {
		panic("RV64 global descriptor acquisition identity mismatch")
	}
	row.retireRequested = true
	batch.retirements.add(row)
}

func railshotRetryGlobalDescriptorDependenciesLocked(owner *globalOwner, batch *globalDescriptorDependencyBatch) {
	for _, row := range owner.descriptorDependencies.rows {
		row.retireRequested = true
		batch.retirements.add(row)
	}
}

func railshotFlushGlobalDescriptorDependencies(batch *globalDescriptorDependencyBatch) {
	var cleanup resourceCleanup
	cleanup.run(func() { batch.publications.drain((*rv64GlobalDescriptorDependency).publish) })
	cleanup.run(func() { batch.retirements.drain((*rv64GlobalDescriptorDependency).release) })
	cleanup.run(func() { batch.legacy.drain((*Instance).releaseResourceRoot) })
	cleanup.finish()
}

// Caller holds in.lifeMu and has not published owner through g.owner yet.
func railshotLinkOwnedGlobalDescriptorHolderLocked(in *Instance, owner *globalOwner) {
	if in == nil || owner == nil || owner.instance != in || owner.typ != ValFuncRef || owner.descriptorDependencies.holder != nil {
		return
	}
	ownership := rv64FunctionDependencyOwnershipForLocked(in)
	owner.descriptorDependencies.holder = ownership
	owner.descriptorDependencies.nextOwned = ownership.ownedGlobalDescriptors.Load()
	ownership.ownedGlobalDescriptors.Store(owner)
}

func railshotOwnedGlobalDescriptorDependenciesComplete(ownership *rv64FunctionDependencyOwnership) bool {
	if ownership == nil {
		return true
	}
	for owner := ownership.ownedGlobalDescriptors.Load(); owner != nil; owner = owner.descriptorDependencies.nextOwned {
		owner.mu.Lock()
		complete := owner.descriptorDependencies.holder == ownership && len(owner.descriptorDependencies.rows) == 0
		owner.mu.Unlock()
		if !complete {
			return false
		}
	}
	return true
}

// Traverse the immutable owner chain one node at a time. Never retain an owner
// or traversal lock across graph work, physical release, or callbacks.
func railshotReleaseOwnedGlobalDescriptorDependencies(in *Instance) {
	ownership := rv64ExistingFunctionDependencyOwnership(in)
	if ownership == nil {
		return
	}
	var cleanup resourceCleanup
	for owner := ownership.ownedGlobalDescriptors.Load(); owner != nil; {
		next := owner.descriptorDependencies.nextOwned
		var batch globalDescriptorDependencyBatch
		owner.mu.Lock()
		if owner.descriptorDependencies.holder == ownership {
			railshotRetryGlobalDescriptorDependenciesLocked(owner, &batch)
		}
		owner.mu.Unlock()
		cleanup.run(func() { railshotFlushGlobalDescriptorDependencies(&batch) })
		owner = next
	}
	cleanup.run(func() { rv64RetireFunctionDependencyMember(&ownership.member) })
	cleanup.finish()
}

// Repeated Close is the repair route after physical teardown has cleared the
// instance's native directories and globalCells. Reentrant Close during the
// physical callback journal must remain prompt and cannot drain this metadata.
func railshotRetryClosedGlobalDescriptorRetirement(in *Instance) error {
	if in == nil || in.invocationState.Load() != instanceInvocationClosed {
		return nil
	}
	ownership := rv64ExistingFunctionDependencyOwnership(in)
	if ownership == nil || !ownership.physicalCleanupFinished.Load() {
		return nil
	}
	var cleanup resourceCleanup
	cleanup.run(func() { railshotReleaseOwnedGlobalDescriptorDependencies(in) })
	if cleanup.panicked {
		return fmt.Errorf("wago: RV64 closed global descriptor retirement: %w", ErrCallbackPanic)
	}
	return nil
}

// Acquire a second, graph-identified physical unit while the caller's exact
// bootstrap remains live. Every error leaves that bootstrap untouched.
func rv64AcquireGlobalDescriptorDependency(producer *Instance, graph *dependencyGraph) (dependencyReceipt, bool, error) {
	if graph == nil {
		graph = rv64FunctionDependencyGraph()
	}
	member := &rv64FunctionDependencyOwnershipFor(producer).member
	if err := graph.registerRetained(member); err != nil {
		return dependencyReceipt{}, false, err
	}
	receipt, err := graph.acquirePending(member, true)
	if err != nil {
		return dependencyReceipt{}, false, err
	}
	return receipt, true, nil
}

func (row *rv64GlobalDescriptorDependency) publish() {
	owner := row.owner
	owner.mu.Lock()
	if row.phase != rv64GlobalDescriptorBootstrap && row.phase != rv64GlobalDescriptorFailed {
		owner.mu.Unlock()
		return
	}
	row.phase = rv64GlobalDescriptorPublishing
	owner.mu.Unlock()

	receipt, exchange, err := rv64AcquireGlobalDescriptorDependency(row.producer, row.graph)
	owner.mu.Lock()
	if row.phase != rv64GlobalDescriptorPublishing || owner.descriptorDependencies.rows[row.mapState] != row {
		owner.mu.Unlock()
		if exchange {
			receipt.release()
		}
		panic("RV64 global descriptor publication lost its stable row")
	}
	if exchange {
		row.receipt = receipt
		row.receiptIdentity = receipt.identity
		row.phase = rv64GlobalDescriptorPending
	} else if errors.Is(err, errDependencyCapacity) {
		// Capacity is the one permitted anonymous fallback. Other failures keep a
		// distinct retryable phase over the same exact bootstrap.
		row.phase = rv64GlobalDescriptorAnonymous
	} else {
		row.phase = rv64GlobalDescriptorFailed
	}
	retire := row.retireRequested
	owner.mu.Unlock()

	var cleanup resourceCleanup
	if exchange {
		// The receipt is authoritative before the original bootstrap is exchanged.
		cleanup.run(row.producer.releaseResourceRoot)
	}
	if retire {
		cleanup.run(row.release)
	}
	if err != nil && !errors.Is(err, errDependencyCapacity) {
		cleanup.run(func() { panic(fmt.Sprintf("RV64 global descriptor dependency: %v", err)) })
	}
	cleanup.finish()
}

func (row *rv64GlobalDescriptorDependency) release() {
	owner := row.owner
	owner.mu.Lock()
	row.retireRequested = true
	previous := row.phase
	switch previous {
	case rv64GlobalDescriptorPublishing:
		owner.mu.Unlock()
		return // the publisher observes retireRequested after installing its result
	case rv64GlobalDescriptorReleasing, rv64GlobalDescriptorReleased:
		owner.mu.Unlock()
		return
	case rv64GlobalDescriptorBootstrap, rv64GlobalDescriptorPending, rv64GlobalDescriptorAnonymous, rv64GlobalDescriptorFailed:
		row.phase = rv64GlobalDescriptorReleasing
	default:
		owner.mu.Unlock()
		panic("RV64 global descriptor dependency has an invalid phase")
	}
	receipt := row.receipt
	owner.mu.Unlock()

	claimed := false
	var cleanup resourceCleanup
	if receipt.graph == nil {
		// Claim the anonymous/bootstrap obligation before physical callbacks.
		owner.mu.Lock()
		if owner.descriptorDependencies.rows[row.mapState] == row && row.phase == rv64GlobalDescriptorReleasing && row.retireRequested && owner.retained[row.producer] != row.mapState {
			row.releaseCommitted = true
			claimed = true
		}
		owner.mu.Unlock()
		if claimed {
			cleanup.run(row.producer.releaseResourceRoot)
		}
	} else {
		cleanup.run(func() {
			claimed = receipt.releaseWithCounter(func() bool {
				nativeExecutionMu.Lock()
				defer nativeExecutionMu.Unlock()
				owner.mu.Lock()
				defer owner.mu.Unlock()
				if owner.descriptorDependencies.rows[row.mapState] != row || row.phase != rv64GlobalDescriptorReleasing || !row.retireRequested || owner.retained[row.producer] == row.mapState || row.receiptIdentity != receipt.identity {
					return false
				}
				row.releaseCommitted = true
				return true
			})
		})
	}

	// Physical release can run arbitrary finalizers and panic after consumption.
	// The exact commit bit therefore decides whether this stable row is retired.
	cleanup.run(func() {
		owner.mu.Lock()
		committed := row.releaseCommitted
		if committed {
			row.phase = rv64GlobalDescriptorReleased
			delete(owner.descriptorDependencies.rows, row.mapState)
			row.receipt = dependencyReceipt{}
		} else {
			row.phase = previous
		}
		owner.mu.Unlock()
		if !committed {
			panic("RV64 global descriptor dependency release did not claim its exact row")
		}
	})
	if holder := owner.descriptorDependencies.holder; holder != nil {
		cleanup.run(func() { rv64RetireFunctionDependencyMember(&holder.member) })
	}
	cleanup.finish()
}
