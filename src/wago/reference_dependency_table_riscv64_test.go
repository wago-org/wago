//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

func tableDependencyTestAcquire(batch *tableImportAttachments, table *Table, store *referenceStore, graph *dependencyGraph, cleanup *resourceCleanup) error {
	exact, _ := valueTypeDescriptorFromValType(table.owner.elementType)
	return rv64AcquireTableDependency(batch, table, table.owner.elementType, exact, nil, store, nil, false, graph, cleanup)
}

func TestRV64TableDependenciesReleaseBlocksAdoptionAndRecycle(t *testing.T) {
	owners := rv64FunctionDependencyUnitOwners(t, 2)
	producer, consumer := owners[0], owners[1]
	consumer.beginConstruction(nil)
	defer consumer.endConstruction()
	g, err := newRV64FunctionDependencyGraph(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	table := counterTable(ValFuncRef, producer, nil, 0)
	var batch tableImportAttachments
	var cleanup resourceCleanup
	if err := tableDependencyTestAcquire(&batch, table, nil, g, &cleanup); err != nil {
		t.Fatal(err)
	}
	rows, row := batch.dependencies.rows, batch.dependencies.rows.rows.inline[0]
	receipt := row.root.receipt // intentionally delayed adoption snapshot
	member, err := rv64RegisteredFunctionDependencyMember(g, consumer)
	if err != nil {
		t.Fatal(err)
	}
	state := table.owner
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
	_, err = receipt.adopt(member, dependencyTableStorage)
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
	committed := row.state == rv64TableDependencyReleasing && row.counterCommitted
	rows.mu.Unlock()
	_, err = receipt.adopt(member, dependencyTableStorage)
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
	if state.importers != 0 || row.state != rv64TableDependencyReleased || producer.resourceRefs != 0 {
		t.Fatal("counter/root release incomplete")
	}
	target := &rv64ExistingFunctionDependencyOwnership(producer).member
	next, err := g.acquirePending(target, false)
	if err != nil || next.identity == receipt.identity {
		t.Fatal("one-slot receipt did not recycle its generation", err)
	}
	defer next.release()
	_, err = receipt.adopt(member, dependencyTableStorage)
	if !errors.Is(err, errDependencyIdentity) || !rows.adoptionReleaseWon(row, receipt, err) {
		t.Fatal("recycled delayed snapshot resurrected the consumed row", err)
	}
	attached := &rv64TableDependencyRow{receiptIdentity: receipt.identity}
	if rows.adoptionReleaseWon(attached, receipt, err) || rows.adoptionReleaseWon(row, dependencyReceipt{identity: receipt.identity + 1}, err) || rows.adoptionReleaseWon(row, receipt, errDependencyMember) {
		t.Fatal("release-won handling swallowed attached/foreign/authority errors")
	}
}

func TestRV64TableDependenciesCounterBoundariesAndAliases(t *testing.T) {
	for _, typ := range []ValType{ValFuncRef, ValExternRef} {
		for _, instanceOwned := range []bool{false, true} {
			for _, count := range []int{-1, 0, maxInt() - 1, maxInt()} {
				t.Run(fmt.Sprintf("%s-instance%t-count%d", typ, instanceOwned, count), func(t *testing.T) {
					// Synthetic storage/counts isolate real graph and importer operations.
					var producer *Instance
					if instanceOwned {
						producer = rv64FunctionDependencyUnitOwners(t, 1)[0]
					}
					store := newReferenceStore(true)
					table := counterTable(typ, producer, store, count)
					graph, err := newRV64FunctionDependencyGraph(1, 1)
					if err != nil {
						t.Fatal(err)
					}
					var batch tableImportAttachments
					var cleanup resourceCleanup
					err = tableDependencyTestAcquire(&batch, table, store, graph, &cleanup)
					if count < 0 || count == maxInt() {
						if err == nil || batch.dependencies.rows != nil || graph.live != 0 || producer != nil && (producer.resourceRefs != 0 || producer.importState.Load() != nil) || store.gcDomains != nil {
							t.Fatal("denied count acquired storage, graph or topology", err)
						}
						return
					}
					if err != nil || table.owner.importers != count+1 || batch.dependencies.rows == nil || producer != nil && producer.resourceRefs != 1 {
						t.Fatal("exact table acquisition", err)
					}
					if err = tableDependencyTestAcquire(&batch, table, store, graph, &cleanup); err != nil || batch.dependencies.rows.rows.n != 1 || table.owner.importers != count+1 {
						t.Fatal("alias acquired a second obligation or rejected the maximum count", err)
					}
					if err = tableDependencyTestAcquire(&batch, table, newReferenceStore(true), graph, &cleanup); err == nil || batch.dependencies.rows.rows.n != 1 {
						t.Fatal("wrong store bypassed alias validation")
					}
					batch.detachAll()
					batch.detachAll()
					if table.owner.importers != count || graph.live != 0 || producer != nil && producer.resourceRefs != 0 || len(table.owner.funcrefStores) != 0 {
						t.Fatal("table acquisition failed exact once-only drain")
					}
					table.owner.importers = 0 // discard synthetic background count
				})
			}
		}
	}
}

func TestRV64TableDependenciesMixedCapacityAndHandoff(t *testing.T) {
	owners := rv64FunctionDependencyUnitOwners(t, 3)
	a, b, consumer := owners[0], owners[1], owners[2]
	graph, err := newRV64FunctionDependencyGraph(3, 1)
	if err != nil {
		t.Fatal(err)
	}
	tables := []*Table{counterTable(ValFuncRef, a, nil, 0), counterTable(ValFuncRef, b, nil, 0), counterTable(ValFuncRef, nil, nil, 0)}
	var batch tableImportAttachments
	var cleanup resourceCleanup
	for _, table := range tables {
		if err := tableDependencyTestAcquire(&batch, table, nil, graph, &cleanup); err != nil {
			t.Fatal(err)
		}
	}
	rows := batch.dependencies.rows
	if rows.rows.n != 3 || rows.rows.inline[0].root.receipt.graph != graph || rows.rows.inline[1].root.receipt.graph != nil || rows.rows.inline[1].root.producer != b || rows.rows.inline[2].root.producer != nil {
		t.Fatal("pending/anonymous/host ownership conflated")
	}
	consumer.beginConstruction(nil)
	defer consumer.endConstruction()
	railshotInstallTableDependencies(consumer, &batch)
	if batch.dependencies.rows != nil || batch.set.n != 0 || batch.store != nil || rv64ExistingTableDependencyRows(consumer) != rows {
		t.Fatal("holder handoff lost its exact acquisition records")
	}
	if err := railshotAdoptTableDependencies(consumer, false); err == nil {
		t.Fatal("adopted while construction native lease held")
	}
	if err := railshotAdoptTableDependencies(consumer, true); err != nil {
		t.Fatal(err)
	}
	row := rows.rows.inline[0]
	consumer.transferImportedTableAttachment(tables[0])
	consumer.transferImportedTableAttachment(tables[0])
	if row.state != rv64TableDependencyReleased || !consumer.ownsTransferredTableAttachment(tables[0]) || a.resourceRefs != 0 || tables[0].owner.importers != 0 {
		t.Fatal("typed transfer failed exact once-only release")
	}
	railshotDrainInstanceTableDependencies(consumer)
	railshotDrainInstanceTableDependencies(consumer)
	consumer.transferImportedTableAttachment(tables[1])
	if !rows.complete() || graph.live != 0 || b.resourceRefs != 0 || consumer.resourceRefs != 0 {
		t.Fatal("typed/anonymous/host holder did not drain")
	}
	for _, table := range tables {
		if table.owner.importers != 0 {
			t.Fatal("counter survived terminal drain")
		}
	}
	if _, exists := transferredImportAttachments.Load(consumer); exists {
		t.Fatal("managed handoff resurrected legacy imports")
	}
}

func TestRV64TableDependenciesWarmBoundedRows(t *testing.T) {
	producer := rv64FunctionDependencyUnitOwners(t, 1)[0]
	graph, err := newRV64FunctionDependencyGraph(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	table := counterTable(ValFuncRef, producer, nil, 0)
	failed := false
	allocs := testing.AllocsPerRun(100, func() {
		var batch tableImportAttachments
		var cleanup resourceCleanup
		if tableDependencyTestAcquire(&batch, table, nil, graph, &cleanup) != nil || tableDependencyTestAcquire(&batch, table, nil, graph, &cleanup) != nil {
			failed = true
		}
		batch.detachAll()
		if producer.resourceRefs != 0 || table.owner.importers != 0 || graph.live != 0 {
			failed = true
		}
	})
	t.Logf("cold table holder plus stable row, warmed registry: %.0f allocations", allocs)
	if failed || allocs > 4 {
		t.Fatal("table holder exceeded bounded cold allocation budget", failed, allocs)
	}
}

func TestRV64TableDependenciesHostFaultRepair(t *testing.T) {
	for _, fault := range []string{"owner", "producer", "element", "store", "topology", "total"} {
		t.Run(fault, func(t *testing.T) {
			table, err := NewTable(1, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			store := newReferenceStore(true)
			var batch tableImportAttachments
			var cleanup resourceCleanup
			if err := tableDependencyTestAcquire(&batch, table, store, nil, &cleanup); err != nil {
				t.Fatal(err)
			}
			row := batch.dependencies.rows.rows.inline[0]
			owner := table.owner
			topology := store.gcDomains
			other := &tableOwner{elementType: ValFuncRef, importers: 1, funcrefStores: map[*referenceStore]uint32{store: 1}}
			switch fault {
			case "owner":
				table.owner = other
			case "producer":
				owner.instance = &Instance{}
			case "element":
				owner.elementType = ValExternRef
			case "store":
				delete(owner.funcrefStores, store)
			case "topology":
				delete(topology.funcrefTables, table)
			case "total":
				owner.importers = 0
			}
			beforeTotal, beforeStore, beforeTopology := owner.importers, owner.funcrefStores[store], topology.funcrefTables[table]
			_, panicked := cleanupTestPanic(batch.detachAll)
			if owner.importers != beforeTotal || owner.funcrefStores[store] != beforeStore || topology.funcrefTables[table] != beforeTopology {
				t.Fatal("denied host claim partly consumed a counter")
			}
			if !panicked || batch.dependencies.rows == nil || row.state != rv64TableDependencyAttached || row.counterCommitted || other.importers != 1 {
				t.Fatal("denied host counter erased its exact acquisition")
			}
			table.owner = owner
			owner.instance = nil
			owner.elementType = ValFuncRef
			owner.importers = 1
			owner.funcrefStores[store] = 1
			topology.funcrefTables[table] = 1
			batch.detachAll()
			batch.detachAll()
			if row.state != rv64TableDependencyReleased || owner.importers != 0 || len(owner.funcrefStores) != 0 || len(topology.funcrefTables) != 0 {
				t.Fatal("repaired host obligation failed drain")
			}
		})
	}
}

func TestRV64TableDependenciesAcquisitionRevalidation(t *testing.T) {
	for _, change := range []string{"close", "closed-denial", "contents", "owner", "backing", "capacity", "type", "store", "exact", "types", "max", "addr64", "count"} {
		t.Run(change, func(t *testing.T) {
			producer, err := Instantiate(tableCounterCompile(t, false, false))
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			table, err := producer.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			if ownership := rv64ExistingFunctionDependencyOwnership(producer); ownership == nil || ownership.member.graph.Load() != nil {
				t.Fatal("mapped table fixture dependency member state")
			}
			graph, err := newRV64FunctionDependencyGraph(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			callbacks, unlocked, reentered := 0, false, false
			if change == "closed-denial" {
				reentry, e := Instantiate(tableCounterCompile(t, false, false))
				if e != nil {
					t.Fatal(e)
				}
				defer reentry.Close()
				producer.referenceLifetime().afterPhysicalRelease(func() {
					callbacks++
					unlocked = nativeExecutionMu.TryLock()
					if unlocked {
						nativeExecutionMu.Unlock()
					}
					if !table.owner.mu.TryLock() {
						unlocked = false
					} else {
						table.owner.mu.Unlock()
					}
					if !table.mu.TryLock() {
						unlocked = false
					} else {
						table.mu.Unlock()
					}
					if !graph.mu.TryLock() {
						unlocked = false
					} else {
						graph.mu.Unlock()
					}
					if unlocked {
						got, e := reentry.Invoke("run")
						reentered = e == nil && slices.Equal(got, []uint64{17})
					}
				})
			}
			var batch tableImportAttachments
			var cleanup resourceCleanup
			type acquisitionResult struct {
				err      error
				value    any
				panicked bool
			}
			done := make(chan acquisitionResult, 1)
			acquired, acquireResume := make(chan struct{}), make(chan struct{})
			joined, resumed := false, false
			graph.mu.Lock()
			graph.afterAcquire = func(owner *Instance) {
				if owner == producer {
					close(acquired)
					<-acquireResume
				}
			}
			graph.mu.Unlock()
			defer func() {
				if !resumed {
					close(acquireResume)
				}
				if !joined {
					select {
					case <-done:
						joined = true
					case <-time.After(3 * time.Second):
						t.Error("acquisition worker failed to join after lock release")
					}
				}
				if joined {
					graph.mu.Lock()
					graph.afterAcquire = nil
					graph.mu.Unlock()
					var release resourceCleanup
					release.run(batch.detachAll)
					release.run(cleanup.finish)
					if release.panicked {
						t.Errorf("acquisition failure cleanup panicked: %v", release.value)
					}
				}
			}()
			go func() {
				result := acquisitionResult{}
				result.value, result.panicked = cleanupTestPanic(func() {
					result.err = tableDependencyTestAcquire(&batch, table, nil, graph, &cleanup)
				})
				done <- result
			}()
			select {
			case <-acquired:
			case <-time.After(3 * time.Second):
				t.Fatal("actual mapped table root was not retained")
			}
			owner := table.owner
			owner.mu.Lock()
			ownerLocked := true
			defer func() {
				if ownerLocked {
					owner.mu.Unlock()
				}
			}()
			if dependencyTestRefs(producer) != 1 {
				t.Fatal("actual mapped table root was not retained")
			}
			close(acquireResume)
			resumed = true
			desc, typ, store, exact, types, max, addr := table.desc, owner.elementType, owner.store, owner.valueType, owner.types, owner.declaredHasMax, owner.addr64
			cap := binary.LittleEndian.Uint32(desc[4:])
			size := binary.LittleEndian.Uint32(desc)
			switch change {
			case "close":
				if err := producer.Close(); err != nil || producer.resourcesClosed {
					t.Fatal("Close overtook actual retained table", err)
				}
			case "closed-denial":
				owner.importers = maxInt() // synthetic late metadata fault after actual retention
				if err := producer.Close(); err != nil || producer.resourcesClosed {
					t.Fatal("Close overtook retained rejection fixture", err)
				}
			case "contents":
				binary.LittleEndian.PutUint32(desc, 2) // current length is mutable, fixed backing is not
			case "owner":
				owner.instance = nil
			case "backing":
				table.desc = make([]byte, len(desc))
				copy(table.desc, desc)
			case "capacity":
				binary.LittleEndian.PutUint32(desc[4:], cap-1)
			case "type":
				owner.elementType = ValExternRef
			case "store":
				owner.store = newReferenceStore(true)
			case "exact":
				owner.valueType.Kind = ValueTypeI64
			case "types":
				owner.types = make([]DefinedTypeDescriptor, 1)
			case "max":
				owner.declaredHasMax = !max
			case "addr64":
				owner.addr64 = !addr
			case "count":
				owner.importers = maxInt()
			}
			owner.mu.Unlock()
			ownerLocked = false
			select {
			case result := <-done:
				joined = true
				if result.panicked {
					t.Fatalf("table acquisition worker panicked: %v", result.value)
				}
				err = result.err
			case <-time.After(3 * time.Second):
				t.Fatal("fresh table revalidation did not finish")
			}
			cleanup.finish()
			if change == "close" || change == "contents" {
				if err != nil || owner.importers != 1 || producer.resourceRefs != 1 || batch.dependencies.rows == nil {
					t.Fatal("valid retained or mutable table rejected", err)
				}
				if change == "contents" {
					binary.LittleEndian.PutUint32(desc, size)
				}
				// A canonical alias still validates after the successful producer retain.
				if err := tableDependencyTestAcquire(&batch, table, nil, graph, &cleanup); err != nil || owner.importers != 1 {
					t.Fatal("retained alias lost its linearized root", err)
				}
				batch.detachAll()
				if graph.live != 0 || producer.resourceRefs != 0 || owner.importers != 0 || change == "close" && (!producer.resourcesClosed || producer.jm != nil) {
					t.Fatal("successful retained table failed actual cleanup")
				}
				return
			}
			if change == "closed-denial" {
				owner.importers = 0 // discard only the synthetic count fault
				if err == nil || batch.dependencies.rows != nil || graph.live != 0 || producer.resourceRefs != 0 || !producer.resourcesClosed || producer.jm != nil || producer.ar != nil || producer.eng != nil || callbacks != 1 || !unlocked || !reentered {
					t.Fatal("rejected table retention held locks across actual producer teardown/native reentry", err, callbacks, unlocked, reentered)
				}
				return
			}
			owner.mu.Lock()
			table.desc, owner.instance, owner.elementType, owner.store, owner.valueType, owner.types, owner.declaredHasMax, owner.addr64, owner.importers = desc, producer, typ, store, exact, types, max, addr, 0
			binary.LittleEndian.PutUint32(desc[4:], cap)
			owner.mu.Unlock()
			if err == nil || batch.dependencies.rows != nil || graph.live != 0 || producer.resourceRefs != 0 {
				t.Fatal("changed identity or count retained an unrecorded root", err)
			}
		})
	}
}
