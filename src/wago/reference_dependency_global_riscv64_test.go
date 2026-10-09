//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// RV rows allocate a bounded cold holder and two stable row identities for the
// two distinct handles in the common alias test. Existing targets allocate zero.
const globalDependencyColdRowAllocationBound = 3

func globalDependencyUnitOwner(t *testing.T) *Instance {
	return rv64FunctionDependencyUnitOwners(t, 1)[0]
}

func TestRV64GlobalDependenciesCounterBoundaries(t *testing.T) {
	for _, typ := range []ValType{ValI32, ValV128, ValFuncRef, ValExternRef} {
		for _, instanceOwned := range []bool{false, true} {
			for _, count := range []int{-1, 0, maxInt() - 1, maxInt()} {
				t.Run(fmt.Sprintf("%s-instance%t-count%d", typ, instanceOwned, count), func(t *testing.T) {
					var producer *Instance
					if instanceOwned {
						producer = rv64FunctionDependencyUnitOwners(t, 1)[0]
					}
					// Cells/counts are synthetic; retention and graph/counter
					// operations are real. Mapped public tests qualify execution.
					store := newReferenceStore(true)
					global := counterGlobal(typ, producer, count, store)
					graph, err := newRV64FunctionDependencyGraph(1, 1)
					if err != nil {
						t.Fatal(err)
					}
					var batch globalImportAttachments
					var cleanup resourceCleanup
					err = rv64AcquireGlobalDependency(&batch, global, store, nil, graph, &cleanup)
					cleanup.finish()
					if count < 0 || count == maxInt() {
						if err == nil || batch.dependencies.rows != nil || graph.live != 0 || producer != nil && (producer.resourceRefs != 0 || producer.importState.Load() != nil) {
							t.Fatal("denied count acquired a row/root/graph member", err)
						}
						return
					}
					if err != nil || global.owner.importers != count+1 || batch.dependencies.rows == nil || producer != nil && (producer.resourceRefs != 1 || graph.live != 1) || producer == nil && graph.live != 0 {
						t.Fatal("exact counted cell acquisition", err)
					}
					if count == maxInt()-1 {
						if err := rv64AcquireGlobalDependency(&batch, global, store, nil, graph, &cleanup); err == nil || batch.dependencies.rows.rows.n != 1 || producer != nil && producer.resourceRefs != 1 {
							t.Fatal("maximum/+1 changed an existing acquisition", err)
						}
					}
					batch.detachAll()
					batch.detachAll()
					if global.owner.importers != count || graph.live != 0 || producer != nil && producer.resourceRefs != 0 {
						t.Fatal("exact counter/receipt did not drain once")
					}
					global.owner.importers = 0 // discard only synthetic background count
				})
			}
		}
	}
}

func TestRV64GlobalDependenciesMixedCapacityAndHandoff(t *testing.T) {
	owners := rv64FunctionDependencyUnitOwners(t, 3)
	a, b, consumer := owners[0], owners[1], owners[2]
	graph, err := newRV64FunctionDependencyGraph(3, 1)
	if err != nil {
		t.Fatal(err)
	}
	globals := []*Global{counterGlobal(ValI32, a, 0, nil), counterGlobal(ValV128, b, 0, nil), counterGlobal(ValI64, nil, 0, nil)}
	var batch globalImportAttachments
	var cleanup resourceCleanup
	for _, global := range globals {
		if err := rv64AcquireGlobalDependency(&batch, global, nil, nil, graph, &cleanup); err != nil {
			t.Fatal(err)
		}
	}
	rows := batch.dependencies.rows
	if rows.rows.n != 3 || rows.rows.inline[0].root.receipt.graph != graph || rows.rows.inline[1].root.receipt.graph != nil || rows.rows.inline[1].root.producer != b || rows.rows.inline[2].root.producer != nil {
		t.Fatal("typed/anonymous/host obligations are not distinct")
	}
	consumer.beginConstruction(nil)
	defer consumer.endConstruction()
	railshotInstallGlobalDependencies(consumer, &batch)
	if batch.dependencies.rows != nil || rv64ExistingGlobalDependencyRows(consumer) != rows || rows.consumer != consumer {
		t.Fatal("terminal handoff replaced the stable acquisition records")
	}
	if err := railshotAdoptGlobalDependencies(consumer, false); err == nil {
		t.Fatal("adoption admitted a live construction native lease")
	}
	if err := railshotAdoptGlobalDependencies(consumer, true); err != nil {
		t.Fatal(err)
	}
	row := rows.rows.inline[0]
	consumer.transferImportedGlobalAttachment(globals[0])
	consumer.transferImportedGlobalAttachment(globals[0])
	if row.state != rv64GlobalDependencyReleased || !consumer.ownsTransferredGlobalAttachment(globals[0]) || a.resourceRefs != 0 || globals[0].owner.importers != 0 {
		t.Fatal("typed transfer did not consume its exact row once")
	}
	railshotDrainInstanceGlobalDependencies(consumer)
	railshotDrainInstanceGlobalDependencies(consumer)
	consumer.transferImportedGlobalAttachment(globals[1])
	if !rows.complete() || b.resourceRefs != 0 || graph.live != 0 || consumer.resourceRefs != 0 {
		t.Fatal("terminal typed/anonymous/host drain left an obligation")
	}
	for _, global := range globals {
		if global.owner.importers != 0 {
			t.Fatal("host or anonymous counter did not drain")
		}
	}
	if _, exists := transferredImportAttachments.Load(consumer); exists {
		t.Fatal("managed transfer resurrected legacy attachment state")
	}
}

func TestRV64GlobalDependenciesAcquisitionRevalidation(t *testing.T) {
	for _, change := range []string{"close", "owner", "backing", "type", "store", "exact", "count"} {
		t.Run(change, func(t *testing.T) {
			producer, err := Instantiate(importedGlobalCompile(t, false, 0, true))
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			global, err := producer.ExportedGlobalObject("g0")
			if err != nil {
				t.Fatal(err)
			}
			if rv64ExistingFunctionDependencyOwnership(producer) != nil {
				t.Fatal("fixture already published its dependency sidecar")
			}
			graph, err := newRV64FunctionDependencyGraph(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			graph.mu.Lock()
			graphLocked := true
			defer func() {
				if graphLocked {
					graph.mu.Unlock()
				}
			}()
			var batch globalImportAttachments
			var cleanup resourceCleanup
			done := make(chan error, 1)
			go func() { done <- rv64AcquireGlobalDependency(&batch, global, nil, nil, graph, &cleanup) }()
			deadline := time.Now().Add(3 * time.Second)
			for rv64ExistingFunctionDependencyOwnership(producer) == nil {
				if time.Now().After(deadline) {
					t.Fatal("initial global snapshot did not finish")
				}
				time.Sleep(time.Millisecond)
			}
			owner := global.owner
			owner.mu.Lock() // Let actual retention finish before fresh validation.
			ownerLocked := true
			defer func() {
				if ownerLocked {
					owner.mu.Unlock()
				}
			}()
			graph.mu.Unlock()
			graphLocked = false
			for {
				producer.lifeMu.Lock()
				retained := producer.resourceRefs == 1
				producer.lifeMu.Unlock()
				if retained {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("actual mapped global producer was not retained")
				}
				time.Sleep(time.Millisecond)
			}
			cell, typ, store, exact := global.cell, owner.typ, owner.store, owner.valueType
			// These fault cases alter only private fixture metadata while fresh
			// validation is blocked. No native execution uses the changed fields.
			switch change {
			case "close":
				if err := producer.Close(); err != nil || producer.resourcesClosed {
					t.Fatal("logical Close overtook a successful real retain", err)
				}
			case "owner":
				owner.instance = nil
			case "backing":
				global.cell = make([]byte, len(cell))
			case "type":
				owner.typ = ValI64
			case "store":
				owner.store = newReferenceStore(true)
			case "exact":
				owner.valueType.Kind = ValueTypeI64
			case "count":
				owner.importers = maxInt()
			}
			owner.mu.Unlock()
			ownerLocked = false
			select {
			case err = <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("global acquisition revalidation did not finish")
			}
			cleanup.finish()
			if change == "close" {
				if err != nil || owner.importers != 1 || producer.resourceRefs != 1 || !railshotHasBuilderGlobalDependencies(&batch) {
					t.Fatal("fresh validation vetoed the linearized global acquisition", err)
				}
				batch.detachAll()
				if owner.importers != 0 || !producer.resourcesClosed || producer.resourceRefs != 0 || producer.jm != nil || producer.ar != nil || producer.eng != nil || len(global.cell) != 0 || graph.live != 0 {
					t.Fatal("post-Close receipt did not finish actual native teardown")
				}
				return
			}
			owner.mu.Lock()
			global.cell, owner.instance, owner.typ, owner.store, owner.valueType, owner.importers = cell, producer, typ, store, exact, 0
			owner.mu.Unlock()
			if err == nil || railshotHasBuilderGlobalDependencies(&batch) || producer.resourceRefs != 0 || graph.live != 0 {
				t.Fatal("changed identity/type/store/count kept an acquired root", err)
			}
		})
	}
}

func TestRV64GlobalDependenciesReleaseBlocksAdoptionAndRecycle(t *testing.T) {
	owners := rv64FunctionDependencyUnitOwners(t, 2)
	producer, consumer := owners[0], owners[1]
	consumer.beginConstruction(nil)
	defer consumer.endConstruction()
	g, err := newRV64FunctionDependencyGraph(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	global := counterGlobal(ValI32, producer, 0, nil)
	var batch globalImportAttachments
	var cleanup resourceCleanup
	if err := rv64AcquireGlobalDependency(&batch, global, nil, nil, g, &cleanup); err != nil {
		t.Fatal(err)
	}
	rows, row := batch.dependencies.rows, batch.dependencies.rows.rows.inline[0]
	receipt := row.root.receipt // intentionally delayed adoption snapshot
	member, err := rv64RegisteredFunctionDependencyMember(g, consumer)
	if err != nil {
		t.Fatal(err)
	}
	state := global.owner
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
	_, err = receipt.adopt(member, dependencyGlobalCell)
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
	committed := row.state == rv64GlobalDependencyReleasing && row.counterCommitted
	rows.mu.Unlock()
	_, err = receipt.adopt(member, dependencyGlobalCell)
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
	if state.importers != 0 || row.state != rv64GlobalDependencyReleased || producer.resourceRefs != 0 {
		t.Fatal("counter/root release incomplete")
	}
	target := &rv64ExistingFunctionDependencyOwnership(producer).member
	next, err := g.acquirePending(target, false)
	if err != nil || next.identity == receipt.identity {
		t.Fatal("one-slot receipt did not recycle its generation", err)
	}
	defer next.release()
	_, err = receipt.adopt(member, dependencyGlobalCell)
	if !errors.Is(err, errDependencyIdentity) || !rows.adoptionReleaseWon(row, receipt, err) {
		t.Fatal("recycled delayed snapshot resurrected the consumed row", err)
	}
	attached := &rv64GlobalDependencyRow{receiptIdentity: receipt.identity}
	if rows.adoptionReleaseWon(attached, receipt, err) || rows.adoptionReleaseWon(row, dependencyReceipt{identity: receipt.identity + 1}, err) || rows.adoptionReleaseWon(row, receipt, errDependencyMember) {
		t.Fatal("release-won handling swallowed attached/foreign/authority errors")
	}
}

func TestRV64GlobalDependenciesWarmBoundedRows(t *testing.T) {
	producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
	graph, err := newRV64FunctionDependencyGraph(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Synthetic cell and unmapped owner isolate allocation accounting. The
	// registry is warmed by AllocsPerRun; each batch still owns a cold stable row.
	global := counterGlobal(ValI32, producer, 0, nil)
	failed := false
	allocs := testing.AllocsPerRun(100, func() {
		var batch globalImportAttachments
		var cleanup resourceCleanup
		if rv64AcquireGlobalDependency(&batch, global, nil, nil, graph, &cleanup) != nil {
			failed = true
		}
		batch.detachAll()
		if producer.resourceRefs != 0 || global.owner.importers != 0 || graph.live != 0 {
			failed = true
		}
	})
	if failed || allocs > 4 {
		t.Fatal("cold global row exceeded its bounded allocation budget", failed, allocs)
	}
}
