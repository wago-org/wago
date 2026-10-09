//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"
	"unsafe"
)

func frozenDependencyTestState(t *testing.T, row *rv64FrozenGlobalDependencyRow, state dependencyReceiptState, consumer *Instance) {
	t.Helper()
	r := row.root.receipt
	if r.graph == nil {
		t.Fatal("expected concrete frozen receipt")
	}
	r.graph.mu.Lock()
	defer r.graph.mu.Unlock()
	record, _ := r.graph.recordLocked(r.identity)
	if record == nil || record.state != state || record.target.owner != row.root.producer || consumer != nil && (record.consumer == nil || record.consumer.owner != consumer || record.kind != dependencyFrozenDescriptor) {
		t.Fatal("frozen receipt lost its exact classification/producer/consumer")
	}
}

func TestRV64FrozenGlobalDependenciesCapacityHandoff(t *testing.T) {
	for _, cap := range []string{"node", "receipt", "typed"} {
		t.Run(cap, func(t *testing.T) {
			owners := rv64FunctionDependencyUnitOwners(t, 3)
			a, b, consumer := owners[0], owners[1], owners[2]
			nodes := 3
			if cap == "node" {
				nodes = 1
			}
			if cap == "receipt" {
				nodes = 2
			}
			g, err := newRV64FunctionDependencyGraph(nodes, 1)
			if err != nil {
				t.Fatal(err)
			}
			rows := &rv64FrozenGlobalDependencyRows{}
			// These unmapped controls exercise actual bootstrap/graph operations;
			// public tests separately use the store-authorized mapped retainer.
			for _, owner := range []*Instance{a, b} {
				if !owner.retainResourceRoot() {
					t.Fatal("bootstrap")
				}
				var cleanup resourceCleanup
				root, exchange, err := rv64AcquireFrozenGlobalDependency(owner, g, &cleanup)
				if err != nil {
					t.Fatal(err)
				}
				row := &rv64FrozenGlobalDependencyRow{holder: rows, root: root}
				rows.rows.push(row)
				rows.entries = append(rows.entries, importGlobalReference{owner: owner, descriptor: 1, dependency: row})
				if exchange {
					owner.releaseResourceRoot()
				}
			}
			defer railshotReleaseGlobalReferences(rows.entries)
			if a.resourceRefs != 1 || b.resourceRefs != 1 || rows.rows.inline[0].root.receipt.graph != g || rows.rows.inline[1].root.receipt.graph != nil {
				t.Fatal("capacity did not preserve distinct Pending/anonymous units")
			}
			consumer.beginConstruction(nil)
			defer consumer.endConstruction()
			railshotInstallFrozenGlobalDependencies(consumer, rows.entries)
			if rv64ExistingFrozenGlobalDependencyRows(consumer) != rows || railshotAdoptFrozenGlobalDependencies(consumer, false) == nil {
				t.Fatal("handoff/native lease guard")
			}
			if err := railshotAdoptFrozenGlobalDependencies(consumer, true); err != nil {
				t.Fatal(err)
			}
			state := dependencyTyped
			if nodes < 3 {
				state = dependencyPending
			}
			frozenDependencyTestState(t, rows.rows.inline[0], state, map[bool]*Instance{true: consumer}[state == dependencyTyped])
			railshotReleaseInstanceGlobalReferences(consumer)
			railshotReleaseInstanceGlobalReferences(consumer)
			if !rows.complete() || a.resourceRefs != 0 || b.resourceRefs != 0 || g.live != 0 {
				t.Fatal("terminal exact drain")
			}
		})
	}
}

func TestRV64FrozenGlobalDependenciesDeniedClaimAndReuse(t *testing.T) {
	for _, typed := range []bool{false, true} {
		t.Run(fmt.Sprintf("typed%t", typed), func(t *testing.T) {
			owners := rv64FunctionDependencyUnitOwners(t, 2)
			producer, consumer := owners[0], owners[1]
			g, _ := newRV64FunctionDependencyGraph(2, 1)
			if !producer.retainResourceRoot() {
				t.Fatal("bootstrap")
			}
			var cleanup resourceCleanup
			root, exchange, err := rv64AcquireFrozenGlobalDependency(producer, g, &cleanup)
			if err != nil || !exchange {
				t.Fatal(err)
			}
			rows := &rv64FrozenGlobalDependencyRows{}
			row := &rv64FrozenGlobalDependencyRow{holder: rows, root: root}
			rows.rows.push(row)
			rows.entries = []importGlobalReference{{global: &Global{}, owner: producer, descriptor: 11, dependency: row}, {global: &Global{}, owner: producer, descriptor: 11, dependency: row}}
			defer railshotReleaseGlobalReferences(rows.entries)
			producer.releaseResourceRoot()
			consumer.beginConstruction(nil)
			defer consumer.endConstruction()
			railshotInstallFrozenGlobalDependencies(consumer, rows.entries)
			member, err := rv64RegisteredFunctionDependencyMember(g, consumer)
			if err != nil {
				t.Fatal(err)
			}
			if typed {
				if err := railshotAdoptFrozenGlobalDependencies(consumer, true); err != nil {
					t.Fatal(err)
				}
			}
			previous := dependencyPending
			if typed {
				previous = dependencyTyped
			}
			g.mu.Lock()
			record, _ := g.recordLocked(root.receipt.identity)
			record.state = dependencyReserved // explicit private denied-claim fault
			g.mu.Unlock()
			_, panicked := cleanupTestPanic(func() { rows.release(row) })
			g.mu.Lock()
			record, _ = g.recordLocked(root.receipt.identity)
			record.state = previous // repair the same actual acquisition
			g.mu.Unlock()
			if !panicked || row.state != rv64FrozenGlobalDependencyAttached || row.root != root || producer.resourceRefs != 1 || rows.complete() || rows.entries[0].dependency != row || rows.entries[1].dependency != row {
				t.Fatal("denied graph claim erased an alias/root/row")
			}
			// The stable holder remains authoritative after a directory is lost.
			consumer.ensureImportState().globalRefEntries = nil
			railshotReleaseInstanceGlobalReferences(consumer)
			if !rows.complete() || producer.resourceRefs != 0 || member.outgoing.Load() != 0 {
				t.Fatal("repaired holder did not drain")
			}
			if _, err := root.receipt.adopt(member, dependencyFrozenDescriptor); !errors.Is(err, errDependencyState) {
				t.Fatal("released Pending/Typed snapshot adopted", err)
			}
			next, err := g.acquirePending(&rv64ExistingFunctionDependencyOwnership(producer).member, false)
			if err != nil {
				t.Fatal(err)
			}
			defer next.release()
			if next.identity == root.receipt.identity {
				t.Fatal("receipt generation reused")
			}
			if _, err := root.receipt.adopt(member, dependencyFrozenDescriptor); !errors.Is(err, errDependencyIdentity) {
				t.Fatal("stale frozen identity revived", err)
			}
			railshotReleaseInstanceGlobalReferences(consumer)
			if producer.resourceRefs != 1 {
				t.Fatal("repeated holder release consumed new generation")
			}
		})
	}
}

func TestRV64FrozenGlobalDependenciesWarmDrainMetadataBound(t *testing.T) {
	producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
	rows := &rv64FrozenGlobalDependencyRows{}
	row := &rv64FrozenGlobalDependencyRow{holder: rows}
	rows.rows.push(row)
	rows.entries = make([]importGlobalReference, 256)
	allocations := testing.AllocsPerRun(100, func() {
		if !producer.retainResourceRoot() {
			panic("warm root")
		}
		row.root, row.state = functionDependencyRoot{producer: producer}, rv64FrozenGlobalDependencyAttached
		for i := range rows.entries {
			rows.entries[i] = importGlobalReference{owner: producer, descriptor: 11, dependency: row}
		}
		railshotReleaseGlobalReferences(rows.entries)
	})
	if allocations != 0 || producer.resourceRefs != 0 || !rows.complete() || unsafe.Sizeof(importGlobalReference{}) != 32 {
		t.Fatalf("warm allocations=%v refs=%d metadata=%d", allocations, producer.resourceRefs, unsafe.Sizeof(importGlobalReference{}))
	}
	t.Logf("256 imported frozen entries reserve %d bytes, including %d bytes for managed row pointers; warmed drain allocations=%v", 256*unsafe.Sizeof(importGlobalReference{}), 256*unsafe.Sizeof(row), allocations)
}
