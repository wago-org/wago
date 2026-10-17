//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// TestPreparedFunctionCleanupMappedClose exercises the ordinary WasmFunc
// boundary rather than a caller-owned PreparedSession. The guest clears its
// genuine exported table, while a second mapped producer is reachable only
// through the instance's already-materialized reconciliation catalog. Every
// physical callback panics after consuming its exact ownership unit.
func TestPreparedFunctionCleanupMappedClose(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, general := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-general%t", artifact, general), func(t *testing.T) {
				owner := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "owner-fixed", artifact), NewImports())
				probe := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "numeric", artifact), NewImports())
				probe.markNativeControlShared()

				fn, err := owner.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				if !fn.scalarFast || fn.isolatedFast || fn.directIntFast || owner.gc != nil {
					t.Fatal("fixture lacks ordinary scalar no-GC admission", fn.scalarFast, fn.isolatedFast, fn.directIntFast, owner.gc)
				}
				ownerTable, err := owner.ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { cleanupTestPanic(func() { _ = ownerTable.Close() }) })
				writerWithRoot := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "writer", artifact), NewImports().Table("env", "table", ownerTable))
				if got, err := writerWithRoot.Invoke("run"); err != nil || len(got) != 1 || got[0] != 17 {
					t.Fatal("actual active initializer writer", got, err)
				}

				catalogTable, err := NewTable(1, 1)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { cleanupTestPanic(func() { _ = catalogTable.Close() }) })
				catalogProducer := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "writer", artifact), NewImports().Table("env", "table", catalogTable))
				if got, err := catalogProducer.Invoke("run"); err != nil || len(got) != 1 || got[0] != 17 {
					t.Fatal("actual catalog producer root", got, err)
				}
				if !catalogTable.retainProducerInstance(catalogProducer) {
					t.Fatal("catalog table does not contain its actual mapped producer")
				}
				owner.funcrefImportContainers().tables.add(catalogTable)

				var closeOnce sync.Once
				state := owner.ensurePluginState()
				markers := [3]any{new(int), new(int), new(int)}
				counts := [3]int{}
				sequence, nextSequence := [3]int{}, 0
				var first any
				firstSet := false
				nilPanic := os.Getenv("WAGO_PREPARED_FUNCTION_CLEANUP_NIL_PANIC") == "1"
				producerCallback := func(index int, table *Table) func() {
					return func() {
						counts[index]++
						sequence[nextSequence], nextSequence = index, nextSequence+1
						preparedCleanupProbe(probe, owner, table)
						if state.invokeMu.state.Load()&invocationGateHeld == 0 || state.invocationID == 0 || owner.invocationState.Load()&instanceInvocationCount != 1 {
							panic("producer callback lost ordinary invocation ownership")
						}
						closeOnce.Do(func() {
							if err := owner.Close(); err != nil || owner.resourcesClosed {
								panic("callback Close finalized a protected ordinary invocation")
							}
						})
						isFirst := !firstSet
						if isFirst {
							first = markers[index]
							firstSet = true
						}
						if nilPanic && isFirst {
							first = nil
							panic(nil)
						}
						panic(markers[index])
					}
				}
				writerWithRoot.referenceLifetime().afterPhysicalRelease(producerCallback(0, ownerTable))
				catalogProducer.referenceLifetime().afterPhysicalRelease(producerCallback(1, catalogTable))
				owner.referenceLifetime().afterPhysicalRelease(func() {
					counts[2]++
					sequence[nextSequence], nextSequence = 2, nextSequence+1
					preparedCleanupProbe(probe, owner, ownerTable)
					if state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0 || owner.invocationState.Load()&instanceInvocationCount != 0 {
						panic("owner finalized before ordinary invocation authority was released")
					}
					isFirst := !firstSet
					if isFirst {
						first = markers[2]
						firstSet = true
					}
					if nilPanic && isFirst {
						first = nil
						panic(nil)
					}
					panic(markers[2])
				})
				for index, in := range []*Instance{writerWithRoot, catalogProducer} {
					if err := in.Close(); err != nil || in.resourcesClosed {
						t.Fatal("logical Close consumed a retained mapped producer", index, err)
					}
				}
				nativeExecutionMu.Lock()
				catalogTable.mu.Lock()
				clear(catalogTable.desc[8:])
				catalogTable.mu.Unlock()
				nativeExecutionMu.Unlock()

				var callErr error
				value, panicked := cleanupTestPanic(func() {
					if general {
						_, callErr = fn.invokeGeneral(nil)
					} else {
						_, callErr = fn.Invoke()
					}
				})
				if callErr != nil || !panicked || value != first || counts != [3]int{1, 1, 1} {
					t.Fatal("ordinary prepared cleanup/first panic", value, first, panicked, callErr, counts)
				}
				wantSequence := [3]int{1, 0, 2}
				if runtime.GOARCH == "riscv64" {
					wantSequence = [3]int{0, 1, 2}
				}
				if sequence != wantSequence {
					t.Fatal("ordinary prepared cleanup order", sequence, wantSequence)
				}
				if state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0 || owner.invocationState.Load()&instanceInvocationCount != 0 {
					t.Fatal("ordinary prepared invocation authority remains held")
				}
				for _, in := range []*Instance{writerWithRoot, catalogProducer, owner} {
					funcrefReconcileAssertMappedDrained(t, in)
				}
			})
		}
	}
}

func TestPreparedFunctionCleanupLeaseAcquisitionRollback(t *testing.T) {
	for _, inPlace := range []bool{false, true} {
		t.Run(fmt.Sprintf("in-place%t", inPlace), func(t *testing.T) {
			in := &Instance{refStore: new(referenceStore)}
			in.executionFlags.Store(executionFlagDynamicGCDomain)
			value, panicked := cleanupTestPanic(func() {
				if inPlace {
					var lease preparedInvocationLease
					in.lockPreparedScalarInvocationInto(&lease)
				} else {
					_ = in.lockPreparedInvocation()
				}
			})
			state := in.ensurePluginState()
			if !panicked || !strings.Contains(fmt.Sprint(value), "dynamic Runtime GC invocation has no topology") || state.invocationID != 0 || state.invokeMu.state.Load()&invocationGateHeld != 0 {
				t.Fatal("failed lease acquisition stranded invocation authority", value, panicked, state.invocationID, state.invokeMu.state.Load())
			}
		})
	}
}

func TestPreparedFunctionCleanupNumericAllocations(t *testing.T) {
	owner := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "owner-fixed", false), NewImports())
	fn, err := owner.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ExportedTable("table"); err != nil {
		t.Fatal(err)
	}
	for _, general := range []bool{false, true} {
		t.Run(fmt.Sprintf("general%t", general), func(t *testing.T) {
			invoke := func() {
				var got []uint64
				var err error
				if general {
					got, err = fn.invokeGeneral(nil)
				} else {
					got, err = fn.invokeScalar(nil)
				}
				if err != nil || len(got) != 1 || got[0] != 17 {
					panic("actual ordinary prepared result")
				}
			}
			invoke()
			if allocations := testing.AllocsPerRun(100, invoke); allocations != 0 {
				t.Fatal("warm ordinary prepared invocation allocates", allocations)
			}
		})
	}
}
