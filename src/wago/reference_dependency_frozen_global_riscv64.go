//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"sync"
)

type rv64FrozenGlobalDependencyState uint8

const (
	rv64FrozenGlobalDependencyAttached rv64FrozenGlobalDependencyState = iota
	rv64FrozenGlobalDependencyReleasing
	rv64FrozenGlobalDependencyReleased
)

// A counted immutable cell can belong to A while its descriptor belongs to B.
// One exact acquisition per B owns all its frozen aliases until physical
// teardown. These rows have no importer-counter or early-transfer obligation.
type rv64FrozenGlobalDependencyRow struct {
	holder *rv64FrozenGlobalDependencyRows
	root   functionDependencyRoot
	state  rv64FrozenGlobalDependencyState
}

type rv64FrozenGlobalDependencyRows struct {
	mu      sync.Mutex
	rows    importDedup[*rv64FrozenGlobalDependencyRow]
	entries []importGlobalReference
}

// The store-authorized retainer has already acquired a concrete bootstrap root
// on B. Never skip retained registration's physical-guard check on active reuse.
// Graph work and bootstrap release happen after all container/store locks end.
func rv64AcquireFrozenGlobalDependency(producer *Instance, graph *dependencyGraph, cleanup *resourceCleanup) (functionDependencyRoot, bool, error) {
	root := functionDependencyRoot{producer: producer}
	if graph == nil {
		graph = rv64FunctionDependencyGraph()
	}
	member := &rv64FunctionDependencyOwnershipFor(producer).member
	err := graph.registerRetained(member)
	if err == nil {
		root.receipt, err = graph.acquirePending(member, true)
	}
	if errors.Is(err, errDependencyCapacity) {
		return root, false, nil // Keep this exact bootstrap, identified as anonymous.
	}
	if err != nil {
		cleanup.run(producer.releaseResourceRoot)
		return functionDependencyRoot{}, false, fmt.Errorf("frozen global dependency: %w", err)
	}
	// The caller records this Pending unit before exchanging the bootstrap.
	// Logical-close pre-finalization can invoke other producers' callbacks even
	// while this new root still pins B; a panic must abort, never lose the row.
	return root, true, nil
}

func railshotHasFrozenGlobalDependencies(entries []importGlobalReference) bool {
	for _, entry := range entries {
		if entry.dependency != nil {
			return true
		}
	}
	return false
}

func railshotInstallFrozenGlobalDependencies(in *Instance, entries []importGlobalReference) {
	for _, entry := range entries {
		if entry.dependency != nil {
			rv64FunctionDependencyOwnershipFor(in).frozenGlobals.Store(entry.dependency.holder)
			return
		}
	}
}

func rv64ExistingFrozenGlobalDependencyRows(in *Instance) *rv64FrozenGlobalDependencyRows {
	if ownership := rv64ExistingFunctionDependencyOwnership(in); ownership != nil {
		return ownership.frozenGlobals.Load()
	}
	return nil
}

func railshotReleaseInstanceGlobalReferences(in *Instance) {
	if rows := rv64ExistingFrozenGlobalDependencyRows(in); rows != nil {
		// The stable holder remains authoritative if terminal teardown has
		// cleared the native directory after a denied receipt claim.
		var cleanup resourceCleanup
		rows.rows.each(func(row *rv64FrozenGlobalDependencyRow) {
			cleanup.run(func() { rows.release(row) })
		})
		for i := range rows.entries {
			if rows.entries[i].dependency == nil {
				rows.entries[i] = importGlobalReference{}
			}
		}
		cleanup.finish()
		return
	}
	railshotReleaseGlobalReferences(in.importGlobalReferences())
}

func railshotAdoptFrozenGlobalDependencies(in *Instance, nativeLeaseReleased bool) error {
	rows := rv64ExistingFrozenGlobalDependencyRows(in)
	if rows == nil {
		return nil
	}
	if !nativeLeaseReleased {
		return errors.New("frozen global dependency adoption still owns construction native lease")
	}
	var result error
	rows.rows.each(func(row *rv64FrozenGlobalDependencyRow) {
		if result != nil {
			return
		}
		rows.mu.Lock()
		receipt, attached := row.root.receipt, row.state == rv64FrozenGlobalDependencyAttached
		rows.mu.Unlock()
		if !attached || receipt.graph == nil {
			return
		}
		member, err := rv64RegisteredFunctionDependencyMember(receipt.graph, in)
		if errors.Is(err, errDependencyCapacity) {
			return // This terminal holder still owns the exact Pending unit.
		}
		if err != nil {
			result = err
			return
		}
		_, result = receipt.adopt(member, dependencyFrozenDescriptor)
	})
	return result
}

func (rows *rv64FrozenGlobalDependencyRows) complete() bool {
	rows.mu.Lock()
	defer rows.mu.Unlock()
	complete := true
	rows.rows.each(func(row *rv64FrozenGlobalDependencyRow) {
		complete = complete && row.state == rv64FrozenGlobalDependencyReleased
	})
	return complete
}

// Only builder rollback or terminal physical teardown drains frozen rows.
// Consumer operation roots do not authorize pruning descriptors from a directory
// an invocation can still read. Claim first, clear aliases, then release B.
func (rows *rv64FrozenGlobalDependencyRows) release(row *rv64FrozenGlobalDependencyRow) {
	rows.mu.Lock()
	if row.state != rv64FrozenGlobalDependencyAttached {
		rows.mu.Unlock()
		return
	}
	row.state = rv64FrozenGlobalDependencyReleasing
	root := row.root
	rows.mu.Unlock()
	claimed := false
	clearAliases := func() {
		claimed = true
		for i := range rows.entries {
			if rows.entries[i].dependency == row {
				rows.entries[i] = importGlobalReference{}
			}
		}
	}
	var cleanup resourceCleanup
	cleanup.run(func() {
		if root.receipt.graph != nil {
			claimed = root.receipt.releaseWithCleanup(clearAliases)
		} else {
			clearAliases()
			root.producer.releaseResourceRoot()
		}
	})
	rows.mu.Lock()
	if claimed {
		row.state, row.root = rv64FrozenGlobalDependencyReleased, functionDependencyRoot{}
	} else {
		row.state = rv64FrozenGlobalDependencyAttached
	}
	rows.mu.Unlock()
	if !claimed {
		cleanup.run(func() { panic("frozen global dependency release did not claim its exact receipt") })
	}
	cleanup.finish()
}
