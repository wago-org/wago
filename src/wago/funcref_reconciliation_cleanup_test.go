//go:build linux && (amd64 || arm64 || (riscv64 && wago_rv64_experimental)) && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func funcrefReconcileGlobal(t *testing.T, rt *Runtime) *Global {
	t.Helper()
	g, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatal(err)
	}
	// A regression can leave later actual roots for Close. Contain only the
	// fixture's raw callback panic so failed assertions still drain the Runtime.
	t.Cleanup(func() { cleanupTestPanic(func() { _ = g.Close() }) })
	return g
}

func funcrefReconcileTable(t *testing.T) *Table {
	t.Helper()
	table, err := NewTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupTestPanic(func() { _ = table.Close() }) })
	return table
}

func funcrefReconcileAssertMappedDrained(t *testing.T, in *Instance) {
	t.Helper()
	if in.resourceRefs != 0 || !in.resourcesClosed || in.jm != nil || in.ar != nil || in.eng != nil {
		t.Fatal("reconciliation stranded actual mapped resources")
	}
}

func TestFuncrefReconcileMappedGlobalBatch(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := globalValueAccessRuntime(t)
			c := globalValueAccessCompile(t, false, artifact)
			producer := globalValueAccessInstantiate(t, rt, c, NewImports())
			export, err := producer.ExportedFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			wc := globalValueAccessCompile(t, true, artifact)
			g := funcrefReconcileGlobal(t, rt)
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("actual producer descriptor")
			}
			writeGlobalObject(g, ValFuncRef, descriptor)
			writers := make([]*Instance, 2)
			if runtime.GOARCH == "riscv64" {
				writers = writers[:1]
			}
			for i := range writers {
				writers[i] = globalValueAccessInstantiate(t, rt, wc, NewImports().Function("env", "target", export))
				if runtime.GOARCH == "riscv64" {
					// Distinct RV thunks do not reach the canonical producer row.
					// Its actual import attachment can conservatively retain one
					// proxy. Production coverage intentionally deduplicates a
					// second such proxy; do not bypass that invariant for a test.
					result := g.retainDescriptorOwnerForFinalization(nil, writers[i])
					if !result.retained || !result.unresolved {
						t.Fatal("actual proxy root acquisition", result)
					}
				} else if !g.retainProducerInstance(writers[i]) {
					t.Fatal("actual canonical writer root acquisition")
				}
				if writers[i].resourceRefs != 1 {
					t.Fatal("fixture lacks exactly one retained writer unit")
				}
			}
			if len(g.owner.retained) != len(writers) {
				t.Fatal("fixture lacks its genuine writer rows")
			}
			t.Logf("genuine retained global rows: %d; multiple canonical rows qualified on existing targets", len(writers))
			var first any
			counts := [2]int{}
			for i, writer := range writers {
				index, marker := i, new(int)
				writer.referenceLifetime().afterPhysicalRelease(func() {
					counts[index]++
					globalValueAccessAssertLocks(g, nil, rt)
					if len(g.owner.retained) != 0 || readGlobalObject(g, ValFuncRef) != 0 {
						panic("global prune has not claimed its complete root batch")
					}
					if first == nil {
						first = marker // The actual map traversal order is unspecified.
					}
					panic(marker)
				})
			}
			for _, in := range append([]*Instance{producer}, writers[:]...) {
				if err := in.Close(); err != nil || in.resourcesClosed {
					t.Fatal("logical Close consumed a retained native producer", err)
				}
			}
			// A completed guest overwrite is represented by a real mapped-cell
			// store under its actual native/container gates, before reconciliation.
			nativeExecutionMu.Lock()
			g.owner.mu.Lock()
			writeGlobalObject(g, ValFuncRef, 0)
			g.owner.mu.Unlock()
			nativeExecutionMu.Unlock()
			value, panicked := cleanupTestPanic(g.pruneRetainedInstances)
			expected := [2]int{1, 1}
			if len(writers) == 1 {
				expected[1] = 0
			}
			if !panicked || value != first || counts != expected {
				t.Fatal("global prune skipped a real proxy or lost first panic", value, first, panicked, counts)
			}
			for _, in := range append([]*Instance{producer}, writers[:]...) {
				funcrefReconcileAssertMappedDrained(t, in)
			}
			g.pruneRetainedInstances()
			if counts != expected || rt.activeOperations != 0 {
				t.Fatal("repeat global prune or Runtime accounting", counts, rt.activeOperations)
			}
		})
	}
}

func TestFuncrefReconcileMappedContainers(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := globalValueAccessRuntime(t)
			c := globalValueAccessCompile(t, false, artifact)
			in := globalValueAccessInstantiate(t, rt, c, NewImports())
			reentry := globalValueAccessInstantiate(t, rt, c, NewImports())
			reentry.markNativeControlShared()
			if err := in.beginInvocation(); err != nil {
				t.Fatal(err)
			}
			previous := in.table
			defer func() {
				in.lifeMu.Lock()
				in.table = previous
				in.lifeMu.Unlock()
				in.endInvocation()
			}()
			// This explicitly private catalog/handle-chain witness uses real host
			// containers, admitted mapped producers and actual root acquisition.
			// It does not claim multi-table or mutable-global import admission.
			containers := in.funcrefImportContainers()
			var globals []*Global
			var tables []*Table
			var producers []*Instance
			var counts [14]int
			first, later := new(int), new(int)
			for i := range counts {
				producer := globalValueAccessInstantiate(t, rt, c, NewImports())
				producers = append(producers, producer)
				descriptor, ok := producer.localFuncrefDescriptor(0)
				if !ok {
					t.Fatal("actual published producer descriptor")
				}
				var g *Global
				var table *Table
				if i < 6 {
					g = funcrefReconcileGlobal(t, rt)
					writeGlobalObject(g, ValFuncRef, descriptor)
					if !g.retainProducerInstance(producer) {
						t.Fatal("actual global root")
					}
					globals = append(globals, g)
					containers.globals.add(g)
				} else {
					table = funcrefReconcileTable(t)
					offset := descriptor - uint64(offHeapSlicePtr(producer.funcRefDescs))
					if offset > uint64(len(producer.funcRefDescs)-coreruntime.TableEntryBytes) {
						t.Fatal("actual descriptor row is out of bounds")
					}
					copy(table.desc[8:], producer.funcRefDescs[int(offset):int(offset)+coreruntime.TableEntryBytes])
					if !table.retainProducerInstance(producer) {
						t.Fatal("actual table root")
					}
					tables = append(tables, table)
					if i < 11 {
						containers.tables.add(table)
					}
				}
				if producer.resourceRefs != 1 {
					t.Fatal("fixture lacks exactly one container-owned root")
				}
				index := i
				producer.referenceLifetime().afterPhysicalRelease(func() {
					counts[index]++
					for _, mu := range []*sync.Mutex{&nativeExecutionMu, &rt.refStore.mu, &in.lifeMu} {
						if !mu.TryLock() {
							panic("reconciliation callback under native/store/lifetime lock")
						}
						mu.Unlock()
					}
					if g != nil {
						globalValueAccessAssertLocks(g, in, rt)
						if len(g.owner.retained) != 0 {
							panic("global row still owned during callback")
						}
					} else {
						for _, mu := range []*sync.Mutex{&table.mu, &table.owner.mu} {
							if !mu.TryLock() {
								panic("reconciliation callback under table lock")
							}
							mu.Unlock()
						}
						if len(table.retained) != 0 {
							panic("table row still owned during callback")
						}
					}
					got, err := reentry.Invoke("run")
					if err != nil || len(got) != 1 || got[0] != 17 {
						panic("actual shared-native reentry failed")
					}
					if index == 0 {
						panic(first)
					}
					if index == 6 || index == 11 {
						panic(later)
					}
				})
			}
			// Include one imported handle in the private local chain: it must be
			// deduplicated while every later local link is still visited.
			in.lifeMu.Lock()
			in.table = tables[0]
			tables[0].next = tables[5]
			tables[5].next = tables[6]
			tables[6].next = tables[7]
			in.lifeMu.Unlock()
			nativeExecutionMu.Lock()
			for _, g := range globals {
				g.owner.mu.Lock()
				writeGlobalObject(g, ValFuncRef, 0)
				g.owner.mu.Unlock()
			}
			for _, table := range tables {
				table.mu.Lock()
				clear(table.desc[8:])
				table.mu.Unlock()
			}
			nativeExecutionMu.Unlock()
			for _, producer := range producers {
				if err := producer.Close(); err != nil || producer.resourcesClosed {
					t.Fatal("retained logical Close", err)
				}
			}
			value, panicked := cleanupTestPanic(in.reconcileFuncrefRoots)
			if !panicked || value != first {
				t.Fatal("whole-container first panic", value, first, panicked)
			}
			for i, producer := range producers {
				if counts[i] != 1 {
					t.Fatalf("later container %d skipped: callbacks=%v", i, counts)
				}
				funcrefReconcileAssertMappedDrained(t, producer)
			}
			in.reconcileFuncrefRoots()
			if rt.activeOperations != 1 {
				t.Fatal("protecting invocation operation lost", rt.activeOperations)
			}
			if allocations := testing.AllocsPerRun(100, in.reconcileFuncrefRoots); allocations != 0 {
				t.Fatal("warm bounded reconciliation allocates", allocations)
			}
		})
	}
}

func TestFuncrefReconcileMappedNativeScan(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := globalValueAccessRuntime(t)
			c := globalValueAccessCompile(t, false, artifact)
			producer := globalValueAccessInstantiate(t, rt, c, NewImports())
			g := funcrefReconcileGlobal(t, rt)
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				t.Fatal("actual descriptor")
			}
			writeGlobalObject(g, ValFuncRef, descriptor)
			if !g.retainProducerInstance(producer) {
				t.Fatal("actual global root")
			}
			callbacks := 0
			producer.referenceLifetime().afterPhysicalRelease(func() {
				callbacks++
				globalValueAccessAssertLocks(g, nil, rt)
			})
			if err := producer.Close(); err != nil || producer.resourcesClosed {
				t.Fatal("retained logical Close", err)
			}
			started, done := make(chan struct{}), make(chan struct{})
			var workerPanic any
			var workerPanicked bool
			nativeExecutionMu.Lock()
			locked := true
			defer func() {
				if locked {
					nativeExecutionMu.Unlock()
				}
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("native scan worker failed to join")
				}
			}()
			go func() {
				defer close(done)
				close(started)
				workerPanic, workerPanicked = cleanupTestPanic(g.pruneRetainedInstances)
			}()
			<-started
			select {
			case <-done:
				t.Fatal("global scan passed an active shared-native write gate")
			case <-time.After(50 * time.Millisecond):
			}
			g.owner.mu.Lock()
			writeGlobalObject(g, ValFuncRef, 0)
			g.owner.mu.Unlock()
			locked = false
			nativeExecutionMu.Unlock()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("native scan did not drain after publication")
			}
			if callbacks != 1 || workerPanicked {
				t.Fatal("serialized scan missed the completed cell overwrite", callbacks, workerPanic, workerPanicked)
			}
			funcrefReconcileAssertMappedDrained(t, producer)
			if allocations := testing.AllocsPerRun(100, g.pruneRetainedInstances); allocations != 0 {
				t.Fatal("warm global scan allocates", allocations)
			}
		})
	}
}
