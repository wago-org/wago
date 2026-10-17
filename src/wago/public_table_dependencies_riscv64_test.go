//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64TableDependencyCompile(t *testing.T, imported, artifact bool) *Compiled {
	return rv64NativeTailCompile(t, fmt.Sprintf("table-dep-storage-import%t", imported), tableCounterModule(imported), artifact)
}

func rv64TableDependencyInvoke(t *testing.T, in *Instance) {
	t.Helper()
	if got, err := in.Invoke("run"); err != nil || !slices.Equal(got, []uint64{17}) {
		t.Fatal("actual RV64 table fixture execution", got, err)
	}
}

func TestRV64PublicTableDependenciesSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, host := range []bool{false, true} {
			for _, runtimeMode := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-host%t-runtime%t", artifact, host, runtimeMode), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(ownedTableConfig()))
					defer rt.Close()
					var producer *Instance
					var table *Table
					var err error
					if host {
						table, err = NewTable(1, 2)
					} else {
						if runtimeMode {
							producer = rv64NativeTokenInstantiate(t, rt, rv64TableDependencyCompile(t, false, artifact), NewImports())
						} else {
							producer, err = Instantiate(rv64TableDependencyCompile(t, false, artifact))
						}
						if err != nil {
							t.Fatal(err)
						}
						defer producer.Close()
						table, err = producer.ExportedTable("table")
					}
					if err != nil {
						t.Fatal(err)
					}
					defer table.Close()
					cc := rv64TableDependencyCompile(t, true, artifact)
					var consumer *Instance
					var acquiredStore *referenceStore
					imports := NewImports().Table("env", "table", table)
					if runtimeMode {
						consumer = rv64NativeTokenInstantiate(t, rt, cc, imports)
						acquiredStore = rt.refStore
					} else {
						consumer, err = Instantiate(cc, imports)
						if err != nil {
							t.Fatal(err)
						}
					}
					defer consumer.Close()
					rows := rv64ExistingTableDependencyRows(consumer)
					if rows == nil || rows.rows.n != 1 || rows.consumer != consumer || table.owner.importers != 1 {
						t.Fatal("actual constructor lost the exact table row")
					}
					row := rows.rows.inline[0]
					if row.store != acquiredStore || row.snapshot.owner != table.owner || row.root.producer != producer {
						t.Fatal("table store/owner identity was inferred from another boundary")
					}
					if producer != nil {
						receipt := row.root.receipt
						receipt.graph.mu.Lock()
						record, _ := receipt.graph.recordLocked(receipt.identity)
						valid := record != nil && record.state == dependencyTyped && record.kind == dependencyTableStorage
						receipt.graph.mu.Unlock()
						if !valid || producer.resourceRefs != 1 {
							t.Fatal("actual constructor did not adopt its storage edge")
						}
						if err := producer.Close(); err != nil || producer.resourcesClosed {
							t.Fatal("table acquisition lost actual storage after logical Close", err)
						}
					} else if row.root.receipt.graph != nil || table.owner.funcrefStores[acquiredStore] != 1 {
						t.Fatal("host topology was mislabeled as a producer edge")
					}
					rv64TableDependencyInvoke(t, consumer)
					alias, err := consumer.ExportedTable("table")
					if err != nil || alias != table {
						t.Fatal("reexport changed acquired storage", err)
					}
					if !runtimeMode {
						// Actual private boundary creation qualifies acquired-store tracking;
						// this is not a claim that the fixture issues a public funcref token.
						later, err := consumer.referenceStoreForBoundary()
						if err != nil || later == nil || row.store != nil {
							t.Fatal("later boundary store replaced acquired nil store", err)
						}
					}
					if err := consumer.Close(); err != nil || !rows.complete() || table.owner.importers != 0 {
						t.Fatal("actual terminal table cleanup", err)
					}
					if host {
						if len(table.owner.funcrefStores) != 0 || acquiredStore != nil && len(acquiredStore.gcDomains.funcrefTables) != 0 {
							t.Fatal("host topology leaked its acquired store")
						}
						if err := table.Close(); err != nil {
							t.Fatal(err)
						}
					} else {
						rv64CheckGlobalDependencyRetirement(t, producer)
						if !producer.resourcesClosed || producer.resourceRefs != 0 || producer.jm != nil || producer.ar != nil || producer.eng != nil {
							t.Fatal("actual table producer storage leaked")
						}
					}
					if host {
						o := rv64ExistingFunctionDependencyOwnership(consumer)
						if o == nil || !o.cleanupCompleted.Load() || o.member.graph.Load() != nil || o.member.identity != 0 {
							t.Fatal("host-only holder invented a graph member or missed physical completion")
						}
					} else {
						rv64CheckGlobalDependencyRetirement(t, consumer)
					}
				})
			}
		}
	}
}

// Admission performs a temporary table scan under the native mutex. Complete
// that scan first, then exercise the unchanged production builder boundary; a
// held publication mutex proves constructor retention preceded its native lease.
// SourceArtifact separately qualifies the complete public entry path.
func TestRV64PublicTableDependenciesEarlyRetain(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			pc := rv64TableDependencyCompile(t, false, artifact)
			cc := rv64TableDependencyCompile(t, true, artifact)
			producer, err := Instantiate(pc)
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			reentry, err := Instantiate(pc)
			if err != nil {
				t.Fatal(err)
			}
			defer reentry.Close()
			table, err := producer.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			callbacks, unlocked, reentered := 0, false, false
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
				if unlocked {
					got, err := reentry.Invoke("run")
					reentered = err == nil && slices.Equal(got, []uint64{17})
				}
			})
			imports := NewImports().Table("env", "table", table)
			resolved, err := imports.snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if err = railshotValidateResolvedImports(cc, resolved, nil); err != nil {
				t.Fatal(err)
			}
			if err = cc.prepareCodeMapping(); err != nil {
				t.Fatal(err)
			}
			view, err := cc.freezeExecution(0)
			if err != nil {
				t.Fatal(err)
			}
			if err = view.preflightImportBindings(resolved); err != nil {
				t.Fatal(err)
			}
			builder := instanceBuilder{c: view, opts: InstantiateOptions{Imports: imports}, imports: resolved}
			nativeExecutionMu.Lock()
			locked := true
			done := make(chan struct{})
			var consumer *Instance
			var instantiateErr error
			joined := false
			var closeDone chan error
			closeJoined := false
			defer func() {
				if locked {
					nativeExecutionMu.Unlock()
				}
				if !joined {
					select {
					case <-done:
					case <-time.After(3 * time.Second):
						t.Error("construction worker did not join")
					}
				}
				if closeDone != nil && !closeJoined {
					select {
					case <-closeDone:
					case <-time.After(3 * time.Second):
						t.Error("logical Close worker did not join")
					}
				}
				if consumer != nil {
					consumer.Close()
				}
			}()
			go func() {
				consumer, instantiateErr = builder.instantiate(cc)
				close(done)
			}()
			deadline := time.Now().Add(time.Second)
			hasPending := func() bool {
				o := rv64ExistingFunctionDependencyOwnership(producer)
				if o == nil {
					return false
				}
				g := o.member.graph.Load()
				if g == nil {
					return false
				}
				g.mu.Lock()
				pending := o.member.receipts == 1
				g.mu.Unlock()
				table.owner.mu.Lock()
				counted := table.owner.importers == 1
				table.owner.mu.Unlock()
				return pending && counted && dependencyTestRefs(producer) == 1
			}
			for !hasPending() && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			retained := hasPending()
			closeDone = make(chan error, 1)
			go func() { closeDone <- producer.Close() }()
			for {
				producer.lifeMu.Lock()
				closed := producer.closed
				producer.lifeMu.Unlock()
				if closed {
					break
				}
				if time.Now().After(deadline.Add(time.Second)) {
					t.Fatal("producer logical Close did not start")
				}
				time.Sleep(time.Millisecond)
			}
			nativeExecutionMu.Unlock()
			locked = false
			select {
			case <-done:
				joined = true
			case <-time.After(3 * time.Second):
				t.Fatal("native table construction failed to complete")
			}
			select {
			case err = <-closeDone:
				closeJoined = true
			case <-time.After(3 * time.Second):
				t.Fatal("producer Close worker did not join")
			}
			if !retained || err != nil || instantiateErr != nil || consumer == nil || producer.resourcesClosed || producer.resourceRefs != 1 {
				t.Fatal("constructor did not retain table storage before native lease and logical Close", retained, err, instantiateErr)
			}
			rv64TableDependencyInvoke(t, consumer)
			if err := consumer.Close(); err != nil || !producer.resourcesClosed || callbacks != 1 || !unlocked || !reentered {
				t.Fatal("actual producer teardown held a table/native lock or failed native reentry", err, callbacks, unlocked, reentered)
			}
		})
	}
}

func TestRV64PublicTableDependenciesDescriptorOwnerSeparation(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			params := []wasm.ValType{wasm.I32}
			bc := rv64NativeTailCompile(t, "table-dep-descriptor-provider", rv64PublicNativeIdentityModule(params), artifact)
			b, err := Instantiate(bc)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			export, err := b.ExportedFunc("identity")
			if err != nil {
				t.Fatal(err)
			}
			ac := rv64NativeTailCompile(t, "table-dep-storage-with-foreign-target", rv64TableFunctionImportModule(params, false, false, nil, false, true), artifact)
			a, err := Instantiate(ac, NewImports().Function("env", "host", export))
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			table, err := a.ExportedTable("table")
			if err != nil || table.instanceOwner() != a {
				t.Fatal("table storage is not owned by A", err)
			}
			cc := rv64NativeTailCompile(t, "table-dep-descriptor-consumer", rv64ImportedTableModule(params, 7, false, false, true, 2), artifact)
			consumer, err := Instantiate(cc, NewImports().Table("env", "table", table))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			rows := rv64ExistingTableDependencyRows(consumer)
			if rows == nil || rows.rows.n != 1 || rows.rows.inline[0].root.producer != a || rows.rows.inline[0].root.producer == b || a.resourceRefs != 1 || b.resourceRefs != 1 {
				t.Fatal("table storage owner A was conflated with installed descriptor owner B")
			}
			if err := b.Close(); err != nil || b.resourcesClosed {
				t.Fatal(err)
			}
			if err := a.Close(); err != nil || a.resourcesClosed {
				t.Fatal(err)
			}
			if got, err := consumer.Invoke("call", 0xfeedface80000001, 0); err != nil || !slices.Equal(got, []uint64{0x80000001, 7}) {
				t.Fatal("actual B descriptor after A/B logical Close", got, err)
			}
			if err := consumer.Close(); err != nil || !rows.complete() || table.owner.importers != 0 {
				t.Fatal("A storage receipt cleanup", err)
			}
			for _, in := range []*Instance{a, b, consumer} {
				rv64CheckGlobalDependencyRetirement(t, in)
				if !in.resourcesClosed || in.jm != nil || in.ar != nil || in.eng != nil || in.resourceRefs != 0 {
					t.Fatal("independent A/B native storage survived terminal cleanup")
				}
			}
		})
	}
}

func TestRV64PublicTableDependenciesObserverFailure(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, observerPanic := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-panic%t", artifact, observerPanic), func(t *testing.T) {
				producer, err := Instantiate(rv64TableDependencyCompile(t, false, artifact))
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				table, err := producer.ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				primary := errors.New("table observer failure")
				callbacks := 0
				producer.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic("table producer cleanup") })
				var captured, result *Instance
				_, panicked := cleanupTestPanic(func() {
					result, err = Instantiate(rv64TableDependencyCompile(t, true, artifact), InstantiateOptions{Imports: NewImports().Table("env", "table", table), afterCreate: func(in *Instance) error {
						captured = in
						if !in.constructionIsActive() || !nativeExecutionMu.TryLock() {
							return errors.New("table adoption preceded native lease release")
						}
						nativeExecutionMu.Unlock()
						rows := rv64ExistingTableDependencyRows(in)
						if rows == nil || rows.rows.n != 1 {
							return errors.New("observer lost table terminal holder")
						}
						in.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic("table consumer cleanup") })
						if err := producer.Close(); err != nil {
							return err
						}
						if observerPanic {
							panic(primary)
						}
						return primary
					}})
				})
				if panicked || result != nil || captured == nil || !errors.Is(err, primary) || !errors.Is(err, ErrCallbackPanic) || callbacks != 2 || !rv64ExistingTableDependencyRows(captured).complete() || table.owner.importers != 0 {
					t.Fatal("observer rollback lost primary error or concrete cleanup", panicked, result, err, callbacks)
				}
				for _, in := range []*Instance{producer, captured} {
					rv64CheckGlobalDependencyRetirement(t, in)
					if !in.resourcesClosed || in.resourceRefs != 0 || in.jm != nil || in.ar != nil || in.eng != nil {
						t.Fatal("observer rollback leaked actual native storage")
					}
				}
			})
		}
	}
}

func TestRV64PublicTableDependenciesMappedRepairRetry(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, mode := range []string{"pending", "typed", "external", "anonymous"} {
			for _, fault := range []string{"missing", "same-producer-owner", "foreign-producer"} {
				t.Run(fmt.Sprintf("artifact%t-%s-%s", artifact, mode, fault), func(t *testing.T) {
					pc := rv64TableDependencyCompile(t, false, artifact)
					producer, err := Instantiate(pc)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					reentry, err := Instantiate(pc)
					if err != nil {
						t.Fatal(err)
					}
					defer reentry.Close()
					rv64TableDependencyInvoke(t, producer)
					table, err := producer.ExportedTable("table")
					if err != nil {
						t.Fatal(err)
					}
					graph, err := newRV64FunctionDependencyGraph(2, 1)
					if err != nil {
						t.Fatal(err)
					}
					if mode == "anonymous" {
						graph, err = newRV64FunctionDependencyGraph(1, 1)
						if err != nil {
							t.Fatal(err)
						}
						if _, err = rv64RegisteredFunctionDependencyMember(graph, rv64FunctionDependencyUnitOwners(t, 1)[0]); err != nil {
							t.Fatal(err)
						}
					}
					var batch tableImportAttachments
					var cleanup resourceCleanup
					if err := tableDependencyTestAcquire(&batch, table, nil, graph, &cleanup); err != nil {
						t.Fatal(err)
					}
					rows, row := batch.dependencies.rows, batch.dependencies.rows.rows.inline[0]
					var consumer *Instance
					if mode == "typed" || mode == "external" {
						// A real mapped consumer, with this exact holder manually installed,
						// isolates receipt states; it does not claim unsupported module shapes.
						consumer, err = Instantiate(pc)
						if err != nil {
							t.Fatal(err)
						}
						defer consumer.Close()
						consumer.beginConstruction(nil)
						railshotInstallTableDependencies(consumer, &batch)
						if mode == "external" {
							// Direct cross-graph receipt adoption qualifies the
							// common release primitive, not process-graph linking.
							other, _ := newRV64FunctionDependencyGraph(1, 1)
							member, e := rv64RegisteredFunctionDependencyMember(other, consumer)
							if e != nil {
								t.Fatal(e)
							}
							if _, err = row.root.receipt.adopt(member, dependencyTableStorage); err != nil {
								t.Fatal(err)
							}
						} else if err = railshotAdoptTableDependencies(consumer, true); err != nil {
							t.Fatal(err)
						}
						consumer.endConstruction()
					}
					drain := batch.detachAll
					if consumer != nil {
						drain = func() { railshotDrainInstanceTableDependencies(consumer) }
					}
					owner := table.owner
					foreign := &Instance{}
					other := &tableOwner{instance: producer, elementType: ValFuncRef, importers: 1}
					callbacks, unlocked, reentered := 0, false, false
					marker := new(int)
					producer.referenceLifetime().afterPhysicalRelease(func() {
						callbacks++
						unlocked = nativeExecutionMu.TryLock()
						if unlocked {
							nativeExecutionMu.Unlock()
						}
						if !owner.mu.TryLock() {
							unlocked = false
						} else {
							owner.mu.Unlock()
						}
						if !graph.mu.TryLock() {
							unlocked = false
						} else {
							graph.mu.Unlock()
						}
						if unlocked {
							got, err := reentry.Invoke("run")
							reentered = err == nil && slices.Equal(got, []uint64{17})
						}
						panic(marker)
					})
					if err := producer.Close(); err != nil || producer.resourcesClosed {
						t.Fatal("logical Close lost acquired mapped table", err)
					}
					switch fault {
					case "missing":
						owner.importers = 0
					case "same-producer-owner":
						table.owner = other
					case "foreign-producer":
						owner.instance = foreign
					}
					repair := func() { table.owner = owner; owner.instance = producer; owner.importers = 1 }
					repaired := false
					defer func() {
						if !repaired {
							repair()
							_, _ = cleanupTestPanic(drain)
						}
					}()
					beforeTotal, beforeOther := owner.importers, other.importers
					_, panicked := cleanupTestPanic(drain)
					if !panicked || row.state != rv64TableDependencyAttached || row.counterCommitted || row.root.producer != producer || rows.complete() || owner.importers != beforeTotal || other.importers != beforeOther || producer.resourceRefs != 1 || producer.resourcesClosed || producer.jm == nil || producer.ar == nil || callbacks != 0 || foreign.resourceRefs != 0 {
						t.Fatal("denied table counter lost its concrete receipt or mapped root")
					}
					if mode != "anonymous" {
						graph.mu.Lock()
						record, _ := graph.recordLocked(row.root.receipt.identity)
						want := dependencyPending
						if mode == "typed" {
							want = dependencyTyped
						} else if mode == "external" {
							want = dependencyExternal
						}
						valid := record != nil && record.state == want
						graph.mu.Unlock()
						if !valid {
							t.Fatal("denied table counter did not restore its exact prior receipt state")
						}
					}
					repair()
					repaired = true
					value, panicked := cleanupTestPanic(drain)
					if !panicked || value != marker || callbacks != 1 || !unlocked || !reentered || !rows.complete() || producer.resourceRefs != 0 || !producer.resourcesClosed || producer.jm != nil || producer.ar != nil || producer.eng != nil || graph.live != 0 || owner.importers != 0 {
						t.Fatal("repaired exact table obligation failed teardown/reentry or replaced panic", value, panicked, callbacks, unlocked, reentered)
					}
					drain()
					if callbacks != 1 {
						t.Fatal("duplicate table drain repeated physical callback")
					}
				})
			}
		}
	}
}

// Manually installed holders on actual mapped instances isolate mixed receipt
// and operation-root states; they do not expand supported module shapes.
func TestRV64PublicTableDependenciesOperationLease(t *testing.T) {
	for _, selected := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("row%d", selected), func(t *testing.T) {
			g, err := newRV64FunctionDependencyGraph(3, 1)
			if err != nil {
				t.Fatal(err)
			}
			pc := rv64TableDependencyCompile(t, false, false)
			var producers [2]*Instance
			var tables [3]*Table
			for i := range producers {
				producers[i], err = Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer producers[i].Close()
				tables[i], err = producers[i].ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
			}
			tables[2] = tableDependencyHostFixture(t)
			defer tables[2].Close()
			consumer, err := Instantiate(pc)
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			var batch tableImportAttachments
			var cleanup resourceCleanup
			for _, table := range tables {
				if err := tableDependencyTestAcquire(&batch, table, nil, g, &cleanup); err != nil {
					t.Fatal(err)
				}
			}
			consumer.beginConstruction(nil)
			railshotInstallTableDependencies(consumer, &batch)
			if err := railshotAdoptTableDependencies(consumer, true); err != nil {
				t.Fatal(err)
			}
			consumer.endConstruction()
			rows := rv64ExistingTableDependencyRows(consumer)
			for _, producer := range producers {
				if err := producer.Close(); err != nil || producer.resourcesClosed {
					t.Fatal("actual imported root failed to retain producer", err)
				}
			}
			state := tables[selected].owner
			state.mu.Lock()
			locked := true
			done := make(chan struct{})
			joined := false
			defer func() {
				if locked {
					state.mu.Unlock()
				}
				if !joined {
					select {
					case <-done:
					case <-time.After(3 * time.Second):
						t.Error("counter transfer worker did not join")
					}
				}
			}()
			go func() { consumer.transferImportedTableAttachment(tables[selected]); close(done) }()
			deadline := time.Now().Add(3 * time.Second)
			for {
				rows.mu.Lock()
				releasing := rows.rows.inline[selected].state == rv64TableDependencyReleasing
				rows.mu.Unlock()
				if releasing {
					break
				}
				if time.Now().After(deadline) {
					state.mu.Unlock()
					locked = false
					t.Fatal("operation did not claim its exact row")
				}
				time.Sleep(time.Millisecond)
			}
			if err := consumer.Close(); err != nil || consumer.resourcesClosed || consumer.finalizing || consumer.resourceRefs != 1 || consumer.jm == nil || consumer.ar == nil || consumer.eng == nil {
				state.mu.Unlock()
				locked = false
				t.Fatal("counter-in-flight operation lost native consumer lifetime", err)
			}
			state.mu.Unlock()
			locked = false
			select {
			case <-done:
				joined = true
			case <-time.After(3 * time.Second):
				t.Fatal("operation root failed to complete terminal drain")
			}
			if !rows.complete() || !consumer.resourcesClosed || consumer.resourceRefs != 0 || consumer.jm != nil || consumer.ar != nil || consumer.eng != nil || g.live != 0 {
				t.Fatal("operation publication/root drop failed to complete physical cleanup")
			}
			for _, table := range tables {
				table.owner.mu.Lock()
				count := table.owner.importers
				table.owner.mu.Unlock()
				if count != 0 {
					t.Fatal("typed/anonymous/host counter leak", count)
				}
			}
			for _, in := range append(producers[:], consumer) {
				rv64CheckGlobalDependencyRetirement(t, in)
			}
			if _, ok := transferredImportAttachments.Load(consumer); ok {
				t.Fatal("concurrent transfer resurrected legacy state")
			}
			consumer.transferImportedTableAttachment(tables[selected])
		})
	}
}

func TestRV64PublicTableDependenciesProducerCallbackLease(t *testing.T) {
	for _, selected := range []int{0, 1} { // typed receipt, capacity fallback
		for _, callbackPanic := range []bool{false, true} {
			t.Run(fmt.Sprintf("row%d-panic%t", selected, callbackPanic), func(t *testing.T) {
				graph, err := newRV64FunctionDependencyGraph(3, 1)
				if err != nil {
					t.Fatal(err)
				}
				pc := rv64TableDependencyCompile(t, false, false)
				var producers [2]*Instance
				var tables [3]*Table
				for i := range producers {
					producers[i], err = Instantiate(pc)
					if err != nil {
						t.Fatal(err)
					}
					defer producers[i].Close()
					tables[i], err = producers[i].ExportedTable("table")
					if err != nil {
						t.Fatal(err)
					}
				}
				tables[2] = tableDependencyHostFixture(t)
				defer tables[2].Close()
				consumer, err := Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				var batch tableImportAttachments
				var cleanup resourceCleanup
				for _, table := range tables {
					if err := tableDependencyTestAcquire(&batch, table, nil, graph, &cleanup); err != nil {
						t.Fatal(err)
					}
				}
				consumer.beginConstruction(nil)
				railshotInstallTableDependencies(consumer, &batch)
				if err := railshotAdoptTableDependencies(consumer, true); err != nil {
					t.Fatal(err)
				}
				consumer.endConstruction()
				rows := rv64ExistingTableDependencyRows(consumer)
				if (rows.rows.inline[selected].root.receipt.graph != nil) != (selected == 0) {
					t.Fatal("fixture did not establish typed and anonymous rows")
				}
				entered, resume := make(chan struct{}), make(chan struct{})
				marker := new(int)
				producers[selected].referenceLifetime().afterPhysicalRelease(func() {
					close(entered)
					<-resume
					if callbackPanic {
						panic(marker)
					}
				})
				for _, producer := range producers {
					if err := producer.Close(); err != nil || producer.resourcesClosed {
						t.Fatal("actual table root failed to retain producer", err)
					}
				}
				type outcome struct {
					value    any
					panicked bool
				}
				done := make(chan outcome, 1)
				joined, resumed := false, false
				defer func() {
					if !resumed {
						close(resume)
					}
					if !joined {
						select {
						case <-done:
						case <-time.After(3 * time.Second):
							t.Error("callback transfer worker did not join")
						}
					}
				}()
				go func() {
					value, panicked := cleanupTestPanic(func() { consumer.transferImportedTableAttachment(tables[selected]) })
					done <- outcome{value, panicked}
				}()
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					t.Fatal("producer physical callback did not begin")
				}
				rows.mu.Lock()
				releasing := rows.rows.inline[selected].state == rv64TableDependencyReleasing
				rows.mu.Unlock()
				tables[selected].owner.mu.Lock()
				count := tables[selected].owner.importers
				tables[selected].owner.mu.Unlock()
				if !releasing || count != 0 {
					t.Fatal("callback must run after counter consumption and before row publication")
				}
				if err := consumer.Close(); err != nil || consumer.resourcesClosed || consumer.finalizing || consumer.resourceRefs != 1 || consumer.jm == nil || consumer.ar == nil || consumer.eng == nil || consumer.nativeContext == 0 {
					t.Fatal("producer callback overtook consumer operation lifetime", err)
				}
				close(resume)
				resumed = true
				select {
				case out := <-done:
					joined = true
					if out.panicked != callbackPanic || callbackPanic && out.value != marker {
						t.Fatal("operation replaced its first physical callback panic", out)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("producer callback did not finish terminal cleanup")
				}
				if !rows.complete() || graph.live != 0 || !consumer.resourcesClosed || consumer.resourceRefs != 0 || consumer.jm != nil || consumer.ar != nil || consumer.eng != nil || consumer.nativeContext != 0 {
					t.Fatal("operation root was dropped before publication or lost terminal cleanup")
				}
				for i, table := range tables {
					if table.owner.importers != 0 || i < 2 && (!producers[i].resourcesClosed || producers[i].resourceRefs != 0 || producers[i].jm != nil || producers[i].ar != nil || producers[i].eng != nil) {
						t.Fatal("callback transfer stranded a later counter or real native producer")
					}
				}
				for _, in := range append(producers[:], consumer) {
					rv64CheckGlobalDependencyRetirement(t, in)
				}
				consumer.transferImportedTableAttachment(tables[selected])
				if _, exists := transferredImportAttachments.Load(consumer); exists {
					t.Fatal("callback transfer resurrected legacy state")
				}
			})
		}
	}
}
func TestRV64PublicTableDependenciesOverflowCleanup(t *testing.T) {
	graph, err := newRV64FunctionDependencyGraph(10, 4)
	if err != nil {
		t.Fatal(err)
	}
	pc := rv64TableDependencyCompile(t, false, false)
	var producers [9]*Instance
	var tables [10]*Table
	var visited [9]int
	first := new(int)
	for i := range producers {
		producers[i], err = Instantiate(pc)
		if err != nil {
			t.Fatal(err)
		}
		defer producers[i].Close()
		tables[i], err = producers[i].ExportedTable("table")
		if err != nil {
			t.Fatal(err)
		}
		index, marker := i, new(int)
		if i == 0 {
			marker = first
		}
		producers[i].referenceLifetime().afterPhysicalRelease(func() { visited[index]++; panic(marker) })
	}
	tables[9] = tableDependencyHostFixture(t)
	defer tables[9].Close()
	consumer, err := Instantiate(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	// Additional actual mapped attachments exercise overflow and mixed capacity;
	// the source/artifact fixture separately establishes native imported execution.
	var batch tableImportAttachments
	var cleanup resourceCleanup
	for _, table := range tables {
		if err := tableDependencyTestAcquire(&batch, table, nil, graph, &cleanup); err != nil {
			t.Fatal(err)
		}
	}
	consumer.beginConstruction(nil)
	railshotInstallTableDependencies(consumer, &batch)
	if err := railshotAdoptTableDependencies(consumer, true); err != nil {
		t.Fatal(err)
	}
	consumer.endConstruction()
	rows := rv64ExistingTableDependencyRows(consumer)
	if rows.rows.n != 10 || rows.rows.index == nil || len(rows.rows.extra) != 6 {
		t.Fatal("fixture lacks actual inline, overflow and indexed rows")
	}
	for _, producer := range producers {
		if err := producer.Close(); err != nil || producer.resourcesClosed || producer.resourceRefs != 1 {
			t.Fatal("mixed receipt failed to retain a real producer", err)
		}
	}
	// Close reports callback panics as errors. Pin its real storage until a
	// final raw release so this oracle can compare the first panic itself.
	if !consumer.retainResourceRoot() {
		t.Fatal("cannot retain actual consumer before logical Close")
	}
	if err := consumer.Close(); err != nil || consumer.resourcesClosed {
		t.Fatal("logical Close overtook the final physical root", err)
	}
	value, panicked := cleanupTestPanic(consumer.releaseResourceRoot)
	if !panicked || value != first || !rows.complete() || graph.live != 0 || !consumer.resourcesClosed || consumer.jm != nil || consumer.ar != nil || consumer.eng != nil {
		t.Fatal("overflow drain skipped an ownership unit or replaced first panic", value, panicked)
	}
	rv64CheckGlobalDependencyRetirement(t, consumer)
	for i, producer := range producers {
		rv64CheckGlobalDependencyRetirement(t, producer)
		if visited[i] != 1 || !producer.resourcesClosed || producer.finalizing || producer.resourceRefs != 0 || producer.jm != nil || producer.ar != nil || producer.eng != nil || tables[i].owner.importers != 0 {
			t.Fatal("later overflow producer did not complete native cleanup", i, visited)
		}
	}
	if tables[9].owner.importers != 0 {
		t.Fatal("host count was lost after an earlier physical panic")
	}
	consumer.transferImportedTableAttachment(tables[0])
	if _, exists := transferredImportAttachments.Load(consumer); exists {
		t.Fatal("repeated terminal transfer resurrected legacy state")
	}
}

func tableDependencyHostFixture(t *testing.T) *Table {
	t.Helper()
	table, err := NewTable(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	return table
}
func TestRV64PublicTableDependenciesStartFailure(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producer, err := Instantiate(rv64TableDependencyCompile(t, false, artifact))
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			table, err := producer.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			source := tableCounterModule(true)
			source = rv64ReplaceTableStageSection(t, source, 1, wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))))
			source = rv64ReplaceTableStageSection(t, source, 3, wasmtest.Section(3, []byte{2, 0, 1}))
			source = rv64ReplaceTableStageSection(t, source, 10, append(wasmtest.Section(8, []byte{1}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 17, 0x0b}), wasmtest.Code([]byte{0, 0x0b})))...))
			cc := rv64NativeTailCompile(t, "table-dep-start-failure", source, artifact)
			callbacks := 0
			producer.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic("table producer start cleanup") })
			var consumer *Instance
			result, err := Instantiate(cc, InstantiateOptions{Imports: NewImports().Table("env", "table", table), afterCreate: func(in *Instance) error {
				consumer = in
				in.referenceLifetime().afterPhysicalRelease(func() { callbacks++; panic("table consumer start cleanup") })
				return producer.Close()
			}})
			var trap *TrapError
			if result != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || !errors.Is(err, ErrCallbackPanic) || callbacks != 2 || consumer == nil || !rv64ExistingTableDependencyRows(consumer).complete() {
				t.Fatal("start trap was replaced or table ownership cleanup skipped", err, callbacks)
			}
			for _, in := range []*Instance{producer, consumer} {
				rv64CheckGlobalDependencyRetirement(t, in)
				if !in.resourcesClosed || in.resourceRefs != 0 || in.jm != nil || in.ar != nil || in.eng != nil {
					t.Fatal("failed start stranded native table ownership")
				}
			}
		})
	}
}
func TestRV64PublicTableDependenciesConstructorRollback(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			pc := rv64TableDependencyCompile(t, false, artifact)
			cc := rv64TableDependencyCompile(t, true, artifact)
			producer, err := Instantiate(pc)
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			reentry, err := Instantiate(pc)
			if err != nil {
				t.Fatal(err)
			}
			defer reentry.Close()
			table, err := producer.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			imports := NewImports().Table("env", "table", table)
			resolved, err := imports.snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if err = railshotValidateResolvedImports(cc, resolved, nil); err != nil {
				t.Fatal(err)
			}
			if err = cc.prepareCodeMapping(); err != nil {
				t.Fatal(err)
			}
			view, err := cc.freezeExecution(0)
			if err != nil {
				t.Fatal(err)
			}
			rt := NewRuntime()
			if err := rt.Close(); err != nil {
				t.Fatal(err)
			}
			builder := instanceBuilder{c: view, opts: InstantiateOptions{Imports: imports, runtime: rt}, imports: resolved}
			marker := new(int)
			callbacks, unlocked, reentered := 0, false, false
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
				if unlocked {
					got, err := reentry.Invoke("run")
					reentered = err == nil && slices.Equal(got, []uint64{17})
				}
				panic(marker)
			})
			nativeExecutionMu.Lock()
			locked := true
			type outcome struct {
				value    any
				panicked bool
				in       *Instance
				err      error
			}
			done := make(chan outcome, 1)
			joined := false
			defer func() {
				if locked {
					nativeExecutionMu.Unlock()
				}
				if !joined {
					select {
					case <-done:
					case <-time.After(3 * time.Second):
						t.Error("failed registration worker did not join")
					}
				}
			}()
			go func() {
				var out outcome
				out.value, out.panicked = cleanupTestPanic(func() { out.in, out.err = builder.instantiate(cc) })
				done <- out
			}()
			deadline := time.Now().Add(3 * time.Second)
			for {
				table.owner.mu.Lock()
				count := table.owner.importers
				table.owner.mu.Unlock()
				if count == 1 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("constructor did not acquire table before native lease")
				}
				time.Sleep(time.Millisecond)
			}
			if err := producer.Close(); err != nil || producer.resourcesClosed {
				t.Fatal("early rollback lost concrete root", err)
			}
			nativeExecutionMu.Unlock()
			locked = false
			select {
			case out := <-done:
				joined = true
				if !out.panicked || out.value != marker || out.in != nil || callbacks != 1 || !unlocked || !reentered || railshotHasBuilderTableDependencies(&builder.tableAttachments) {
					t.Fatal("builder rollback replaced first actual panic or retained a table obligation", out, callbacks, unlocked, reentered)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("constructor rollback did not complete")
			}
			rv64CheckGlobalDependencyRetirement(t, producer)
			if !producer.resourcesClosed || producer.resourceRefs != 0 || producer.jm != nil || producer.ar != nil || producer.eng != nil || table.owner.importers != 0 {
				t.Fatal("failed registration stranded actual table storage")
			}
		})
	}
}
func TestRV64PublicTableDependenciesReexport(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producer, err := Instantiate(rv64TableDependencyCompile(t, false, artifact))
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			table, err := producer.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			cc := rv64TableDependencyCompile(t, true, artifact)
			first, err := Instantiate(cc, NewImports().Table("env", "table", table))
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			alias, err := first.ExportedTable("table")
			if err != nil || alias != table {
				t.Fatal("table reexport lost canonical handle", err)
			}
			second, err := Instantiate(cc, NewImports().Table("env", "table", alias))
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()
			one, two := rv64ExistingTableDependencyRows(first), rv64ExistingTableDependencyRows(second)
			if one == nil || two == nil || one == two || one.rows.inline[0].root.producer != producer || two.rows.inline[0].root.producer != producer || one.rows.inline[0].root.receipt.identity == two.rows.inline[0].root.receipt.identity || producer.resourceRefs != 2 || table.owner.importers != 2 {
				t.Fatal("reexport inferred the relay as owner or aliased concrete acquisitions")
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := first.Close(); err != nil || !one.complete() || two.complete() || producer.resourcesClosed || producer.resourceRefs != 1 || table.owner.importers != 1 {
				t.Fatal("relay Close consumed downstream table acquisition", err)
			}
			rv64TableDependencyInvoke(t, second)
			if err := second.Close(); err != nil || !two.complete() || table.owner.importers != 0 {
				t.Fatal("downstream table receipt failed cleanup", err)
			}
			for _, in := range []*Instance{producer, first, second} {
				rv64CheckGlobalDependencyRetirement(t, in)
			}
		})
	}
}

func TestRV64PublicTableDependenciesSameHome(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			memory, err := NewSharedMemory(1, 4)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			params := []wasm.ValType{wasm.I32}
			pc := rv64NativeTailCompile(t, "table-dep-same-home-provider", rv64ForeignTableCallModule(params, 9, true, false), artifact)
			cc := rv64NativeTailCompile(t, "table-dep-same-home-consumer", rv64ImportedTableModule(params, 7, true, false, true, 1), artifact)
			var producers, consumers [2]*Instance
			var tables [2]*Table
			for i := range producers {
				producers[i], err = Instantiate(pc, NewImports().Memory("env", "memory", memory))
				if err != nil {
					t.Fatal(err)
				}
				defer producers[i].Close()
				tables[i], err = producers[i].ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				consumers[i], err = Instantiate(cc, NewImports().Memory("env", "memory", memory).Table("env", "table", tables[i]))
				if err != nil {
					t.Fatal(err)
				}
				defer consumers[i].Close()
				rows := rv64ExistingTableDependencyRows(consumers[i])
				if rows == nil || rows.rows.n != 1 || rows.rows.inline[0].root.producer != producers[i] || rows.rows.inline[0].snapshot.owner != tables[i].owner {
					t.Fatal("same-home table storage receipt inferred an owner from code")
				}
			}
			if producers[0].jm != producers[1].jm || producers[0].base != producers[1].base || producers[0].nativeContext == producers[1].nativeContext || tables[0].owner == tables[1].owner {
				t.Fatal("fixture lacks shared home/code and distinct actual table owners")
			}
			for i := range producers {
				if err := producers[i].Close(); err != nil || producers[i].resourceRefs != 1 || producers[i].resourcesClosed {
					t.Fatal("same home collapsed physical table obligations", err)
				}
				if got, err := consumers[i].Invoke("call", 5, 0); err != nil || !slices.Equal(got, []uint64{14, 7}) {
					t.Fatal("same-home actual table call", got, err)
				}
			}
			for i := range consumers {
				if err := consumers[i].Close(); err != nil || !rv64ExistingTableDependencyRows(consumers[i]).complete() || !producers[i].resourcesClosed || tables[i].owner.importers != 0 {
					t.Fatal("exact same-home owner cleanup", err)
				}
				if i == 0 && producers[1].resourcesClosed {
					t.Fatal("same-home cleanup consumed the other physical instance")
				}
				rv64CheckGlobalDependencyRetirement(t, producers[i])
				rv64CheckGlobalDependencyRetirement(t, consumers[i])
			}
		})
	}
}
