//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// The one process graph is a bounded, noncollecting metadata prerequisite.
// Exhaustion preserves identified anonymous roots. Full collection requires
// complete policy-bounded growable registration; this cache cap is not A4.
var rv64FunctionDependencyRegistry struct {
	once  sync.Once
	graph *dependencyGraph
}

func newRV64FunctionDependencyGraph(nodes, receipts int) (*dependencyGraph, error) {
	g, err := newDependencyGraph(nodes, receipts)
	if err == nil {
		g.afterReceiptRelease = func(target, consumer *dependencyMember) {
			rv64RetireFunctionDependencyMember(target)
			if consumer != target {
				rv64RetireFunctionDependencyMember(consumer)
			}
		}
	}
	return g, err
}

func rv64FunctionDependencyGraph() *dependencyGraph {
	rv64FunctionDependencyRegistry.once.Do(func() {
		g, err := newRV64FunctionDependencyGraph(maxDependencyPrimitiveCapacity, maxDependencyPrimitiveCapacity)
		if err != nil {
			panic(err)
		}
		rv64FunctionDependencyRegistry.graph = g
	})
	return rv64FunctionDependencyRegistry.graph
}

type functionDependencyRoot struct {
	producer *Instance
	receipt  dependencyReceipt // zero means this exact acquisition stayed anonymous
}

func (r functionDependencyRoot) release() {
	if r.receipt.graph != nil {
		r.receipt.release()
	} else {
		r.producer.releaseResourceRoot()
	}
}

type functionDependencyAttachments struct {
	roots importDedup[functionDependencyRoot]
}

type rv64FunctionDependencyState uint8

const (
	rv64FunctionDependencyAttached rv64FunctionDependencyState = iota
	rv64FunctionDependencyReleasing
	rv64FunctionDependencyReleased
)

// Installed function roots use stable rows so a denied exact receipt release
// remains owned and retryable. Builder rollback retains its private drain batch:
// those receipts are unpublished and each release claims before callbacks.
type rv64FunctionDependencyRow struct {
	root  functionDependencyRoot
	state rv64FunctionDependencyState
}

type rv64FunctionDependencyRows struct {
	mu     sync.Mutex
	inline [4]rv64FunctionDependencyRow
	n      int
	extra  []rv64FunctionDependencyRow
}

func (rows *rv64FunctionDependencyRows) push(root functionDependencyRoot) {
	rows.mu.Lock()
	defer rows.mu.Unlock()
	inline := min(rows.n, len(rows.inline))
	for i := 0; i < inline; i++ {
		if rows.inline[i].state == rv64FunctionDependencyReleased {
			rows.inline[i] = rv64FunctionDependencyRow{root: root}
			return
		}
	}
	for i := range rows.extra {
		if rows.extra[i].state == rv64FunctionDependencyReleased {
			rows.extra[i] = rv64FunctionDependencyRow{root: root}
			return
		}
	}
	if rows.n < len(rows.inline) {
		rows.inline[rows.n].root = root
	} else {
		rows.extra = append(rows.extra, rv64FunctionDependencyRow{root: root})
	}
	rows.n++
}

func (rows *rv64FunctionDependencyRows) each(fn func(*rv64FunctionDependencyRow)) {
	inline := min(rows.n, len(rows.inline))
	for i := 0; i < inline; i++ {
		fn(&rows.inline[i])
	}
	for i := range rows.extra {
		fn(&rows.extra[i])
	}
}

type rv64FunctionDependencyOwnership struct {
	member                  dependencyMember
	functions               rv64FunctionDependencyRows
	memories                atomic.Pointer[rv64MemoryDependencyRows]
	globals                 atomic.Pointer[rv64GlobalDependencyRows]
	tables                  atomic.Pointer[rv64TableDependencyRows]
	frozenGlobals           atomic.Pointer[rv64FrozenGlobalDependencyRows]
	ownedGlobalDescriptors  atomic.Pointer[globalOwner]
	ownedTableDescriptors   atomic.Pointer[Table]
	managed                 bool // remains true after drain; a reentrant call never rescans imports
	componentRelease        atomic.Pointer[rv64DependencyComponentCoordinator]
	physicalCleanupFinished atomic.Bool // set only after the complete physical callback journal
	cleanupCompleted        atomic.Bool // physical journal plus every authoritative receipt holder
	closedAdoptionMu        sync.Mutex
	closedAdoptionState     *instanceCloseState
	closedAdoptionReady     bool
	closedAdoptionRunning   bool
	closedAdoptionRequested bool
	closedAdoptionResult    error
}

type rv64DependencyComponentCoordinator struct {
	release      *dependencyComponentRelease
	published    atomic.Bool
	teardownDone []atomic.Bool
}

func rv64FunctionDependencyOwnershipFor(in *Instance) *rv64FunctionDependencyOwnership {
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	return rv64FunctionDependencyOwnershipForLocked(in)
}

func rv64FunctionDependencyOwnershipForLocked(in *Instance) *rv64FunctionDependencyOwnership {
	accounting := &in.ensureImportState().rootAccounting
	if accounting.dependencies == nil {
		accounting.dependencies = &rv64FunctionDependencyOwnership{}
		accounting.dependencies.member.owner = in
	}
	return accounting.dependencies
}

func rv64RegisteredFunctionDependencyMember(g *dependencyGraph, in *Instance) (*dependencyMember, error) {
	ownership := rv64FunctionDependencyOwnershipFor(in)
	m := &ownership.member
	if m.graph.Load() == g && m.active.Load() {
		return m, nil
	}
	err := g.register(m)
	// Another caller may have published this one authoritative member while we
	// waited for the graph. Never accept another graph or a retired identity.
	if err == nil || errors.Is(err, errDependencyMember) && m.graph.Load() == g && m.active.Load() {
		return m, nil
	}
	return nil, err
}

func railshotAcquireFunctionDependency(a *functionImportAttachments, producer *Instance) error {
	return rv64AcquireFunctionDependency(a, producer, rv64FunctionDependencyGraph())
}

// Explicit graph argument lets isolated capacity/race tests use the real
// acquisition path without resetting a live process registry or replacing retain.
func rv64AcquireFunctionDependency(a *functionImportAttachments, producer *Instance, g *dependencyGraph) error {
	m, err := rv64RegisteredFunctionDependencyMember(g, producer)
	var receipt dependencyReceipt
	if err == nil {
		receipt, err = g.acquirePending(m, false)
	}
	if errors.Is(err, errDependencyCapacity) {
		if !producer.retainResourceRoot() {
			return fmt.Errorf("producer instance is closed")
		}
	} else if err != nil {
		return fmt.Errorf("native function dependency: %w", err)
	}
	a.dependencies.roots.push(functionDependencyRoot{producer: producer, receipt: receipt})
	return nil
}

func railshotDrainBuilderFunctionDependencies(a *functionImportAttachments) bool {
	if a.dependencies.roots.n == 0 {
		return false // legacy/synthetic sets have no receipt acquisition records
	}
	// Builder records are authoritative even when every acquisition is anonymous.
	a.set.reset()
	a.dependencies.roots.drain(functionDependencyRoot.release)
	return true
}

func railshotHasBuilderFunctionDependencies(a *functionImportAttachments) bool {
	return a.dependencies.roots.n != 0
}

func railshotInstallFunctionDependencies(in *Instance, a *functionImportAttachments) {
	if a.dependencies.roots.n == 0 {
		return
	}
	ownership := rv64FunctionDependencyOwnershipFor(in)
	a.dependencies.roots.each(func(root functionDependencyRoot) {
		ownership.functions.push(root)
	})
	ownership.managed = true
	a.dependencies.roots = importDedup[functionDependencyRoot]{}
	a.set.reset()
}

func rv64ExistingFunctionDependencyOwnership(in *Instance) *rv64FunctionDependencyOwnership {
	if in == nil {
		return nil
	}
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if state := in.importState.Load(); state != nil {
		return state.rootAccounting.dependencies
	}
	return nil
}

func railshotAdoptFunctionDependencies(in *Instance, nativeLeaseReleased bool) error {
	ownership := rv64ExistingFunctionDependencyOwnership(in)
	if ownership == nil || !ownership.managed {
		return nil
	}
	if !nativeLeaseReleased {
		return errors.New("native function dependency adoption still owns construction native lease")
	}
	rows := &ownership.functions
	var result error
	rows.each(func(row *rv64FunctionDependencyRow) {
		if result != nil {
			return
		}
		rows.mu.Lock()
		root, attached := row.root, row.state == rv64FunctionDependencyAttached
		rows.mu.Unlock()
		if !attached || root.receipt.graph == nil {
			return
		}
		g := root.receipt.graph
		consumer, err := rv64RegisteredFunctionDependencyMember(g, in)
		if errors.Is(err, errDependencyCapacity) {
			return
		} // exact pending/external receipt remains terminally owned
		if err != nil {
			result = err
			return
		}
		_, result = root.receipt.adopt(consumer, dependencyFunction)
	})
	return result
}

func (rows *rv64FunctionDependencyRows) release(ownership *rv64FunctionDependencyOwnership, row *rv64FunctionDependencyRow) {
	rows.mu.Lock()
	if row.state != rv64FunctionDependencyAttached {
		rows.mu.Unlock()
		return
	}
	row.state = rv64FunctionDependencyReleasing
	root := row.root
	rows.mu.Unlock()
	claimed := false
	var cleanup resourceCleanup
	if root.receipt.graph != nil {
		cleanup.run(func() {
			root.receipt.releaseWithCleanup(func() { claimed = true })
		})
	} else if root.producer != nil {
		cleanup.run(func() {
			claimed = true
			root.producer.releaseResourceRoot()
		})
	}
	rows.mu.Lock()
	if claimed {
		row.root = functionDependencyRoot{}
		row.state = rv64FunctionDependencyReleased
	} else {
		row.state = rv64FunctionDependencyAttached
	}
	rows.mu.Unlock()
	if !claimed {
		cleanup.run(func() { panic("function dependency release did not claim its exact receipt") })
	} else {
		cleanup.run(func() { rv64RetireFunctionDependencyMember(&ownership.member) })
	}
	cleanup.finish()
}

func (rows *rv64FunctionDependencyRows) drain(ownership *rv64FunctionDependencyOwnership) {
	var cleanup resourceCleanup
	rows.each(func(row *rv64FunctionDependencyRow) {
		cleanup.run(func() { rows.release(ownership, row) })
	})
	cleanup.finish()
}

func (rows *rv64FunctionDependencyRows) complete() bool {
	rows.mu.Lock()
	defer rows.mu.Unlock()
	complete := true
	rows.each(func(row *rv64FunctionDependencyRow) {
		complete = complete && row.state == rv64FunctionDependencyReleased
	})
	return complete
}

func railshotDrainInstanceFunctionDependencies(in *Instance) bool {
	ownership := rv64ExistingFunctionDependencyOwnership(in)
	if ownership == nil || !ownership.managed {
		return false
	}
	ownership.functions.drain(ownership)
	return true
}

func railshotFunctionDependencyCleanupCompleted(in *Instance) {
	if ownership := rv64ExistingFunctionDependencyOwnership(in); ownership != nil {
		ownership.physicalCleanupFinished.Store(true)
		rv64RetireFunctionDependencyMember(&ownership.member)
	}
}

func rv64FunctionDependencyHoldersComplete(ownership *rv64FunctionDependencyOwnership) bool {
	if ownership == nil || !ownership.functions.complete() {
		return false
	}
	if rows := ownership.memories.Load(); rows != nil && !rows.complete() {
		return false
	}
	if rows := ownership.globals.Load(); rows != nil && !rows.complete() {
		return false
	}
	if rows := ownership.tables.Load(); rows != nil && !rows.complete() {
		return false
	}
	if rows := ownership.frozenGlobals.Load(); rows != nil && !rows.complete() {
		return false
	}
	return railshotOwnedGlobalDescriptorDependenciesComplete(ownership) && railshotOwnedTableDescriptorDependenciesComplete(ownership)
}

func railshotAdoptClosedContainerDependencies(in *Instance) {
	var cleanup resourceCleanup
	cleanup.run(func() { railshotAdoptClosedTableDescriptorDependencies(in) })
	cleanup.run(func() { railshotAdoptClosedGlobalDescriptorDependencies(in) })
	cleanup.finish()
}

func railshotClosedContainerAdoptionReady(in *Instance) bool {
	if in == nil {
		return false
	}
	stateRoot := in.importState.Load()
	if stateRoot == nil || stateRoot.rootAccounting.dependencies == nil {
		return false
	}
	ownership := stateRoot.rootAccounting.dependencies
	state := in.closeState.Load()
	ownership.closedAdoptionMu.Lock()
	ready := ownership.closedAdoptionReady && ownership.closedAdoptionState == state
	ownership.closedAdoptionMu.Unlock()
	return ready
}

func railshotTerminalClosedContainerDependencyAdoption(in *Instance, state *instanceCloseState) {
	ownership := rv64ExistingFunctionDependencyOwnership(in)
	if ownership == nil || state == nil || in.closeState.Load() != state {
		return
	}
	ownership.closedAdoptionMu.Lock()
	if ownership.closedAdoptionState != nil && ownership.closedAdoptionState != state {
		ownership.closedAdoptionMu.Unlock()
		panic("RV64 closed container adoption changed close identity")
	}
	ownership.closedAdoptionState = state
	ownership.closedAdoptionReady = true
	if ownership.closedAdoptionRunning {
		ownership.closedAdoptionRequested = true
		ownership.closedAdoptionMu.Unlock()
		return
	}
	ownership.closedAdoptionRunning = true
	ownership.closedAdoptionRequested = false
	ownership.closedAdoptionMu.Unlock()
	rv64RunClosedContainerDependencyAdoption(in, ownership)
}

func railshotPublishClosedContainerAdoptionResult(in *Instance, state *instanceCloseState, err error) {
	ownership := rv64ExistingFunctionDependencyOwnership(in)
	if ownership == nil {
		return
	}
	ownership.closedAdoptionMu.Lock()
	if ownership.closedAdoptionState == state {
		ownership.closedAdoptionResult = err
	}
	ownership.closedAdoptionMu.Unlock()
}

func railshotClosedContainerAdoptionResult(in *Instance, state *instanceCloseState) error {
	ownership := rv64ExistingFunctionDependencyOwnership(in)
	if ownership == nil {
		return nil
	}
	ownership.closedAdoptionMu.Lock()
	defer ownership.closedAdoptionMu.Unlock()
	if ownership.closedAdoptionState == state {
		return ownership.closedAdoptionResult
	}
	return nil
}

func railshotRequestClosedContainerDependencyAdoption(in *Instance) {
	ownership := rv64ExistingFunctionDependencyOwnership(in)
	if ownership == nil || in == nil {
		return
	}
	state := in.closeState.Load()
	ownership.closedAdoptionMu.Lock()
	if !ownership.closedAdoptionReady || ownership.closedAdoptionState != state {
		ownership.closedAdoptionMu.Unlock()
		return
	}
	if ownership.closedAdoptionRunning {
		ownership.closedAdoptionRequested = true
		ownership.closedAdoptionMu.Unlock()
		return
	}
	ownership.closedAdoptionRunning = true
	ownership.closedAdoptionRequested = false
	ownership.closedAdoptionMu.Unlock()
	rv64RunClosedContainerDependencyAdoption(in, ownership)
}

func rv64RunClosedContainerDependencyAdoption(in *Instance, ownership *rv64FunctionDependencyOwnership) {
	var cleanup resourceCleanup
	for {
		cleanup.run(func() { railshotAdoptClosedContainerDependencies(in) })
		ownership.closedAdoptionMu.Lock()
		if ownership.closedAdoptionRequested {
			ownership.closedAdoptionRequested = false
			ownership.closedAdoptionMu.Unlock()
			continue
		}
		ownership.closedAdoptionRunning = false
		ownership.closedAdoptionMu.Unlock()
		break
	}
	cleanup.finish()
}

func rv64MaybeCompleteFunctionDependencies(ownership *rv64FunctionDependencyOwnership) {
	if ownership == nil || ownership.cleanupCompleted.Load() || !ownership.physicalCleanupFinished.Load() {
		return
	}
	if !rv64FunctionDependencyHoldersComplete(ownership) {
		return
	}
	ownership.cleanupCompleted.Store(true)
}

// rv64CollectClosedDependencyComponent is an internal driver only. Admission
// remains unchanged. It persists the irreversible release object on every
// member before the first callback and uses the existing physical teardown
// order rather than independently pruning individual holders.
func rv64CollectClosedDependencyComponent(seed *dependencyMember, nodeBudget, receiptBudget int) bool {
	return rv64CollectClosedDependencyComponentWithPublicationHook(seed, nodeBudget, receiptBudget, nil)
}

// The hook is a deterministic test seam between the first ownership pointer
// and the remaining pointers. The coordinator's published flag is the actual
// production gate: no worker may drive physical teardown until every member
// can find the same persistent retry owner.
func rv64CollectClosedDependencyComponentWithPublicationHook(seed *dependencyMember, nodeBudget, receiptBudget int, afterFirst func()) bool {
	if seed == nil || seed.owner == nil {
		return false
	}
	ownership := rv64ExistingFunctionDependencyOwnership(seed.owner)
	if ownership == nil || &ownership.member != seed {
		return false
	}
	if coordinator := ownership.componentRelease.Load(); coordinator != nil {
		if !coordinator.published.Load() {
			return false
		}
		return rv64DriveDependencyComponentRelease(coordinator)
	}
	g := seed.graph.Load()
	if g == nil {
		return false
	}
	candidate := g.discoverClosedComponent(seed, nodeBudget, receiptBudget)
	if len(candidate.members) == 0 {
		return false
	}
	// Resolve the complete RV ownership directory before the irreversible
	// resourcesClosed/finalizer-detachment claim. Once that claim succeeds,
	// publication below must have no fallible member lookup or conflict check.
	ownerships := make([]*rv64FunctionDependencyOwnership, len(candidate.members))
	for i, member := range candidate.members {
		if member == nil || member.owner == nil {
			return false
		}
		memberOwnership := rv64ExistingFunctionDependencyOwnership(member.owner)
		if memberOwnership == nil || &memberOwnership.member != member || memberOwnership.componentRelease.Load() != nil {
			return false
		}
		ownerships[i] = memberOwnership
	}
	release, ok := g.claimClosedComponentForPhysicalRelease(candidate)
	if !ok {
		return false
	}
	coordinator := &rv64DependencyComponentCoordinator{release: release, teardownDone: make([]atomic.Bool, len(release.claim.members))}
	for i, memberOwnership := range ownerships {
		memberOwnership.componentRelease.Store(coordinator)
		if i == 0 && afterFirst != nil {
			afterFirst()
		}
	}
	coordinator.published.Store(true)
	return rv64DriveDependencyComponentRelease(coordinator)
}

func rv64RetryDependencyComponentHolders(owner *Instance, cleanup *resourceCleanup) {
	cleanup.run(func() { railshotDrainInstanceFunctionDependencies(owner) })
	cleanup.run(func() { railshotDrainInstanceGlobalDependencies(owner) })
	cleanup.run(func() { railshotReleaseInstanceGlobalReferences(owner) })
	cleanup.run(func() { railshotDrainInstanceTableDependencies(owner) })
	cleanup.run(func() { railshotReleaseOwnedTableDescriptorDependencies(owner) })
	cleanup.run(func() { railshotReleaseOwnedGlobalDescriptorDependencies(owner) })
	cleanup.run(func() { railshotDrainInstanceMemoryDependencies(owner) })
}

func rv64DriveDependencyComponentRelease(coordinator *rv64DependencyComponentCoordinator) (completed bool) {
	if coordinator == nil || !coordinator.published.Load() || coordinator.release == nil || coordinator.release.claim == nil || len(coordinator.teardownDone) != len(coordinator.release.claim.members) {
		return false
	}
	release := coordinator.release
	var cleanup resourceCleanup
	for i, member := range release.claim.members {
		index, owner := i, member.owner
		cleanup.run(func() {
			release.runPhysicalJournal(index, func(memberCleanup *resourceCleanup) bool {
				if !coordinator.teardownDone[index].Load() {
					memberCleanup.run(func() { owner.referenceLifetime().notifyStore(owner.refStore, referenceLifetimeResourcesReleased) })
					owner.releaseResources(memberCleanup)
					coordinator.teardownDone[index].Store(true)
				} else {
					rv64RetryDependencyComponentHolders(owner, memberCleanup)
				}
				return rv64FunctionDependencyHoldersComplete(rv64ExistingFunctionDependencyOwnership(owner))
			})
		})
		if release.physicalJournals[index].Load() == 2 {
			cleanup.run(func() { railshotFunctionDependencyCleanupCompleted(owner) })
		}
	}
	cleanup.run(func() { completed = release.complete() })
	if completed {
		for _, member := range release.claim.members {
			if ownership := rv64ExistingFunctionDependencyOwnership(member.owner); ownership != nil {
				ownership.componentRelease.CompareAndSwap(coordinator, nil)
			}
		}
	}
	cleanup.finish()
	return completed
}

func rv64RetireFunctionDependencyMember(m *dependencyMember) {
	if m == nil || m.owner == nil {
		return
	}
	ownership := rv64ExistingFunctionDependencyOwnership(m.owner)
	if ownership == nil || &ownership.member != m {
		return
	}
	rv64MaybeCompleteFunctionDependencies(ownership)
	if !ownership.cleanupCompleted.Load() {
		return
	}
	if g := m.graph.Load(); g != nil {
		_ = g.unregister(m)
	}
}
