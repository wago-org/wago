//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"
	"time"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// These counters/backings are synthetic. All producer retention, receipt
// claims, construction exclusion and counter operations below are real; mapped
// public regressions separately establish native teardown and callability.
func rv64MemoryDependencyUnitMemory(producer *Instance, addr64 bool, count uint32) *Memory {
	s := &memoryState{owner: producer}
	s.set(memoryStateShared, true)
	s.set(memoryStateAddr64, addr64)
	s.setImporterCount(count)
	m := &Memory{jm: &coreruntime.JobMemory{}}
	m.state.Store(s)
	return m
}

func TestRV64MemoryDependenciesCounterBoundaries(t *testing.T) {
	for _, addr64 := range []bool{false, true} {
		for _, count := range []uint32{0, 254, 255, 256, ^uint32(0) - 1, ^uint32(0)} {
			t.Run(fmt.Sprintf("memory64%t-count%d", addr64, count), func(t *testing.T) {
				producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
				g, err := newRV64FunctionDependencyGraph(1, 1)
				if err != nil {
					t.Fatal(err)
				}
				memory := rv64MemoryDependencyUnitMemory(producer, addr64, count)
				defer memory.state.Load().setImporterCount(0)
				var batch memoryImportAttachments
				var cleanup resourceCleanup
				err = rv64AcquireMemoryDependency(&batch, memory, g, &cleanup)
				cleanup.finish()
				if count == ^uint32(0) {
					if err == nil || railshotHasBuilderMemoryDependencies(&batch) || producer.resourceRefs != 0 || g.live != 0 {
						t.Fatal("overflow acquired an importer/root/record", err)
					}
					return
				}
				if err != nil || memory.state.Load().importerCount() != count+1 || producer.resourceRefs != 1 || g.live != 1 {
					t.Fatal("exact counter plus pending producer receipt", err)
				}
				batch.detachAll()
				batch.detachAll()
				if memory.state.Load().importerCount() != count || producer.resourceRefs != 0 || g.live != 0 {
					t.Fatal("counter/root unit did not drain exactly once")
				}
			})
		}
	}
}

func TestRV64MemoryDependenciesAcquisitionRevalidation(t *testing.T) {
	for _, change := range []string{"close", "owner", "backing", "state", "shape", "count"} {
		t.Run(change, func(t *testing.T) {
			producer, err := Instantiate(importedMemoryCompile(t, false, 0, false, false, 0))
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			memory, err := producer.ExportedMemory("memory")
			if err != nil {
				t.Fatal(err)
			}
			if rv64ExistingFunctionDependencyOwnership(producer) != nil {
				t.Fatal("fixture already published its acquisition sidecar")
			}
			g, err := newRV64FunctionDependencyGraph(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			g.mu.Lock()
			graphLocked := true
			defer func() {
				if graphLocked {
					g.mu.Unlock()
				}
			}()
			var batch memoryImportAttachments
			var cleanup resourceCleanup
			done := make(chan error, 1)
			go func() { done <- rv64AcquireMemoryDependency(&batch, memory, g, &cleanup) }()
			deadline := time.Now().Add(3 * time.Second)
			for rv64ExistingFunctionDependencyOwnership(producer) == nil {
				if time.Now().After(deadline) {
					t.Fatal("acquisition did not finish its initial memory snapshot")
				}
				time.Sleep(time.Millisecond)
			}
			// Sidecar creation follows the initial snapshot. Block only the fresh
			// counter check, allowing the real producer retain to finish first.
			state := memory.state.Load()
			state.mu.Lock()
			stateLocked := true
			defer func() {
				if stateLocked {
					state.mu.Unlock()
				}
			}()
			g.mu.Unlock()
			graphLocked = false
			for {
				producer.lifeMu.Lock()
				retained := producer.resourceRefs == 1
				producer.lifeMu.Unlock()
				if retained {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("acquisition did not retain the actual mapped producer")
				}
				time.Sleep(time.Millisecond)
			}
			originalBacking, originalMeta := memory.jm, state.meta
			// Fault cases change only test metadata while fresh validation is
			// excluded. They do not claim native execution or mutate mappings.
			switch change {
			case "close":
				if err := producer.Close(); err != nil || producer.resourcesClosed {
					t.Fatal("logical Close overtook the successful real retain", err)
				}
			case "owner":
				state.owner = nil
			case "backing":
				memory.jm = &coreruntime.JobMemory{}
			case "state":
				memory.state.Store(&memoryState{})
			case "shape":
				state.set(memoryStateDeclaredShared, !state.has(memoryStateDeclaredShared))
			case "count":
				state.setImporterCount(^uint32(0))
			}
			state.mu.Unlock()
			stateLocked = false
			select {
			case err = <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("fresh validation did not complete")
			}
			cleanup.finish()
			if change == "close" {
				if err != nil || state.importerCount() != 1 || producer.resourceRefs != 1 || !railshotHasBuilderMemoryDependencies(&batch) {
					t.Fatal("fresh validation vetoed the linearized acquisition", err)
				}
				batch.detachAll()
				if state.importerCount() != 0 || !producer.resourcesClosed || producer.resourceRefs != 0 || producer.jm != nil || producer.eng != nil || producer.ar != nil || g.live != 0 {
					t.Fatal("successful post-Close acquisition lost actual teardown")
				}
				return
			}
			state.mu.Lock()
			memory.state.Store(state)
			memory.jm, state.owner, state.meta = originalBacking, producer, originalMeta
			state.mu.Unlock()
			if err == nil || railshotHasBuilderMemoryDependencies(&batch) || producer.resourceRefs != 0 || g.live != 0 || state.importerCount() != 0 {
				t.Fatal("changed identity/type/fresh counter kept an acquired root", err)
			}
		})
	}
}

func TestRV64MemoryDependenciesMixedCapacityAndHandoff(t *testing.T) {
	for _, nodes := range []int{1, 3} {
		t.Run(fmt.Sprintf("nodes%d", nodes), func(t *testing.T) {
			owners := rv64FunctionDependencyUnitOwners(t, 3)
			g, err := newRV64FunctionDependencyGraph(nodes, 1)
			if err != nil {
				t.Fatal(err)
			}
			consumer := owners[2]
			consumer.beginConstruction(nil)
			defer consumer.endConstruction()
			memories := []*Memory{rv64MemoryDependencyUnitMemory(owners[0], true, 254), rv64MemoryDependencyUnitMemory(owners[1], true, 255), rv64MemoryDependencyUnitMemory(nil, false, 0)}
			var batch memoryImportAttachments
			var cleanup resourceCleanup
			for _, memory := range memories {
				if err := rv64AcquireMemoryDependency(&batch, memory, g, &cleanup); err != nil {
					t.Fatal(err)
				}
				defer memory.state.Load().setImporterCount(0)
			}
			rows := batch.dependencies.rows
			if rows.rows.n != 3 || rows.rows.inline[0].root.receipt.graph != g || rows.rows.inline[1].root.receipt.graph != nil || rows.rows.inline[1].root.producer != owners[1] || rows.rows.inline[2].root.producer != nil {
				t.Fatal("pending, exact anonymous, and host-only rows")
			}
			railshotInstallMemoryDependencies(consumer, &batch)
			if railshotHasBuilderMemoryDependencies(&batch) || rv64ExistingMemoryDependencyRows(consumer) != rows || !railshotHasInstanceMemoryDependencies(consumer) {
				t.Fatal("stable holder did not move exactly once")
			}
			if err := railshotAdoptMemoryDependencies(consumer, false); err == nil {
				t.Fatal("adopted under native construction lease")
			}
			if err := railshotAdoptMemoryDependencies(consumer, true); err != nil {
				t.Fatal(err)
			}
			for _, memory := range memories {
				consumer.transferImportedMemoryAttachment(memory)
				consumer.transferImportedMemoryAttachment(memory)
				if !consumer.ownsTransferredMemoryAttachment(memory) {
					t.Fatal("persistent exact row marker lost")
				}
			}
			if !rows.complete() || owners[0].resourceRefs != 0 || owners[1].resourceRefs != 0 || consumer.resourceRefs != 0 || g.live != 0 || memories[0].state.Load().importerCount() != 254 || memories[1].state.Load().importerCount() != 255 || memories[2].state.Load().importerCount() != 0 {
				t.Fatal("mixed rows counter/root imbalance")
			}
			if _, ok := transferredImportAttachments.Load(consumer); ok {
				t.Fatal("managed memory recreated the legacy transfer map")
			}
			batch.detachAll()
		})
	}
}

func TestRV64MemoryDependenciesReleaseBlocksAdoptionAndRecycle(t *testing.T) {
	owners := rv64FunctionDependencyUnitOwners(t, 2)
	producer, consumer := owners[0], owners[1]
	consumer.beginConstruction(nil)
	defer consumer.endConstruction()
	g, err := newRV64FunctionDependencyGraph(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	memory := rv64MemoryDependencyUnitMemory(producer, false, 0)
	var batch memoryImportAttachments
	var cleanup resourceCleanup
	if err := rv64AcquireMemoryDependency(&batch, memory, g, &cleanup); err != nil {
		t.Fatal(err)
	}
	rows, row := batch.dependencies.rows, batch.dependencies.rows.rows.inline[0]
	receipt := row.root.receipt // intentionally delayed adoption snapshot
	member, err := rv64RegisteredFunctionDependencyMember(g, consumer)
	if err != nil {
		t.Fatal(err)
	}
	state := memory.state.Load()
	state.mu.Lock() // release must claim the graph before it can consume the counter
	done := make(chan struct{})
	callbackEntered, callbackResume := make(chan struct{}), make(chan struct{})
	joined, resumed := false, false
	g.afterRelease = func(*Instance) { close(callbackEntered); <-callbackResume }
	defer func() {
		if !resumed {
			close(callbackResume)
		}
		if !joined {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("committed callback cleanup join timed out")
			}
		}
	}()
	go func() { rows.release(row, true, false); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		g.mu.Lock()
		record, _ := g.recordLocked(receipt.identity)
		releasing := record != nil && record.state == dependencyReleasing
		g.mu.Unlock()
		if releasing {
			break
		}
		if time.Now().After(deadline) {
			state.mu.Unlock()
			t.Fatal("receipt was not demoted before blocked counter consumption")
		}
		time.Sleep(time.Millisecond)
	}
	_, err = receipt.adopt(member, dependencyMemory)
	if !errors.Is(err, errDependencyState) || rows.adoptionReleaseWon(row, receipt, err) || producer.resourceRefs != 1 {
		state.mu.Unlock()
		t.Fatal("releasing counter was adopted or lost its physical root", err)
	}
	state.mu.Unlock()
	select {
	case <-callbackEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("committed physical callback did not start")
	}
	rows.mu.Lock()
	committed := row.state == rv64MemoryDependencyReleasing && row.counterCommitted
	rows.mu.Unlock()
	_, err = receipt.adopt(member, dependencyMemory)
	if !committed || !errors.Is(err, errDependencyState) || !rows.adoptionReleaseWon(row, receipt, err) {
		t.Fatal("committed but incomplete physical callback did not establish release-won", err)
	}
	close(callbackResume)
	resumed = true
	select {
	case <-done:
		joined = true
		g.afterRelease = nil
	case <-time.After(3 * time.Second):
		t.Fatal("blocked release failed to complete")
	}
	if state.importerCount() != 0 || row.state != rv64MemoryDependencyReleased || producer.resourceRefs != 0 {
		t.Fatal("counter/root release incomplete")
	}
	target := &rv64ExistingFunctionDependencyOwnership(producer).member
	next, err := g.acquirePending(target, false)
	if err != nil || next.identity == receipt.identity {
		t.Fatal("one-slot receipt did not recycle its generation", err)
	}
	defer next.release()
	_, err = receipt.adopt(member, dependencyMemory)
	if !errors.Is(err, errDependencyIdentity) || !rows.adoptionReleaseWon(row, receipt, err) {
		t.Fatal("recycled delayed snapshot resurrected the consumed row", err)
	}
	attached := &rv64MemoryDependencyRow{receiptIdentity: receipt.identity}
	if rows.adoptionReleaseWon(attached, receipt, err) || rows.adoptionReleaseWon(row, dependencyReceipt{identity: receipt.identity + 1}, err) || rows.adoptionReleaseWon(row, receipt, errDependencyMember) {
		t.Fatal("release-won handling swallowed attached/foreign/authority errors")
	}
}

func TestRV64MemoryDependenciesWarmBoundedRows(t *testing.T) {
	producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
	g, err := newRV64FunctionDependencyGraph(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	memory := rv64MemoryDependencyUnitMemory(producer, false, 0)
	failed := false
	allocs := testing.AllocsPerRun(100, func() {
		var batch memoryImportAttachments
		var cleanup resourceCleanup
		if rv64AcquireMemoryDependency(&batch, memory, g, &cleanup) != nil {
			failed = true
		}
		batch.detachAll()
		if producer.resourceRefs != 0 || memory.state.Load().importerCount() != 0 || g.live != 0 {
			failed = true
		}
	})
	if failed || allocs > 4 {
		t.Fatal("cold row ownership exceeded its bounded allocation budget", failed, allocs)
	}
}
