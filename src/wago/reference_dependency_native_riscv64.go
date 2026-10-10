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

type rv64FunctionDependencyOwnership struct {
	member                  dependencyMember
	functions               importDedup[functionDependencyRoot]
	memories                atomic.Pointer[rv64MemoryDependencyRows]
	globals                 atomic.Pointer[rv64GlobalDependencyRows]
	tables                  atomic.Pointer[rv64TableDependencyRows]
	frozenGlobals           atomic.Pointer[rv64FrozenGlobalDependencyRows]
	ownedGlobalDescriptors  atomic.Pointer[globalOwner]
	managed                 bool        // remains true after drain; a reentrant call never rescans imports
	physicalCleanupFinished atomic.Bool // set only after the complete physical callback journal
	cleanupCompleted        atomic.Bool // physical journal plus every authoritative receipt holder
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
	ownership.functions, ownership.managed = a.dependencies.roots, true
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
	var result error
	ownership.functions.each(func(root functionDependencyRoot) {
		if result != nil || root.receipt.graph == nil {
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

func railshotDrainInstanceFunctionDependencies(in *Instance) bool {
	ownership := rv64ExistingFunctionDependencyOwnership(in)
	if ownership == nil || !ownership.managed {
		return false
	}
	ownership.functions.drain(functionDependencyRoot.release)
	return true
}

func railshotFunctionDependencyCleanupCompleted(in *Instance) {
	if ownership := rv64ExistingFunctionDependencyOwnership(in); ownership != nil {
		ownership.physicalCleanupFinished.Store(true)
		rv64RetireFunctionDependencyMember(&ownership.member)
	}
}

func rv64MaybeCompleteFunctionDependencies(ownership *rv64FunctionDependencyOwnership) {
	if ownership == nil || ownership.cleanupCompleted.Load() || !ownership.physicalCleanupFinished.Load() {
		return
	}
	if rows := ownership.memories.Load(); rows != nil && !rows.complete() {
		return
	}
	if rows := ownership.globals.Load(); rows != nil && !rows.complete() {
		return
	}
	if rows := ownership.tables.Load(); rows != nil && !rows.complete() {
		return
	}
	if rows := ownership.frozenGlobals.Load(); rows != nil && !rows.complete() {
		return
	}
	if !railshotOwnedGlobalDescriptorDependenciesComplete(ownership) {
		return
	}
	ownership.cleanupCompleted.Store(true)
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
