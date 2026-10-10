//go:build linux && (amd64 || arm64 || (riscv64 && wago_rv64_experimental)) && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func publicCleanupRelayCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_PUBLIC_INVOCATION_CLEANUP_ARTIFACT_DIR"), os.Getenv("WAGO_PUBLIC_INVOCATION_CLEANUP_ARTIFACT_MODE")
	path := filepath.Join(dir, "public-invocation-cleanup-relay.wago")
	if mode == "load" {
		if !artifact {
			t.Fatal("compiler-free runner selected source")
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		c, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "target", 0, 0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
	)
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if !artifact {
		return c
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if mode == "prepare" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, blob, 0600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })
	return loaded
}

func publicCleanupVoidCompile(t *testing.T) *Compiled {
	t.Helper()
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 0),
			wasmtest.ExportEntry("table", 1, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0xd0, 0x70, 0x26, 0, 0x0b}))),
	)
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func publicCleanupHostCompile(t *testing.T) *Compiled {
	t.Helper()
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "target", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 1),
			wasmtest.ExportEntry("table", 1, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0, 0x10, 0, 0x1a, // first host call; drop
			0x20, 0, 0x10, 0, // second host call supplies the result
			0x0b,
		}))),
	)
	config := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	if runtime.GOARCH != "riscv64" {
		config = config.WithOptimization("prepared-bounded-entry", false)
	}
	c, err := Compile(config, module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestPublicInvocationCleanupMappedReexport exercises the complete ordinary
// Invoke boundary. The relay's private reconciliation catalog and the attached
// producer's genuine exported table each own a mapped producer. Their physical
// callbacks panic after consuming their exact roots; the first callback also
// closes the active relay, making endInvocation own relay finalization and the
// attached function root's eventual producer finalization before gate release.
func TestPublicInvocationCleanupMappedReexport(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			owner := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "owner-fixed", artifact), NewImports())
			ownerTable, err := owner.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { cleanupTestPanic(func() { _ = ownerTable.Close() }) })
			writer := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "writer", artifact), NewImports().Table("env", "table", ownerTable))
			if got, err := writer.Invoke("run"); err != nil || len(got) != 1 || got[0] != 17 {
				t.Fatal("actual attached-table writer", got, err)
			}
			target, err := owner.ExportedFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			relay := preparedCleanupInstantiate(t, publicCleanupRelayCompile(t, artifact), NewImports().Function("env", "target", target))

			catalogTable, err := NewTable(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { cleanupTestPanic(func() { _ = catalogTable.Close() }) })
			catalogProducer := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "writer", artifact), NewImports().Table("env", "table", catalogTable))
			if got, err := catalogProducer.Invoke("run"); err != nil || len(got) != 1 || got[0] != 17 || !catalogTable.retainProducerInstance(catalogProducer) {
				t.Fatal("actual relay catalog root", got, err)
			}
			// A pure re-export declares no table. This explicit private handle-chain
			// seam uses a real host table and mapped producer only to make the
			// relay's completed-call reconciliation observable.
			relay.lifeMu.Lock()
			relay.table = catalogTable
			relay.lifeMu.Unlock()
			relay.markNativeControlShared()

			state := relay.ensurePluginState()
			markers := [4]any{new(int), new(int), new(int), new(int)}
			var first any
			firstSet := false
			nilPanic := os.Getenv("WAGO_PUBLIC_INVOCATION_CLEANUP_NIL_PANIC") == "1"
			counts := [4]int{}
			var closeRelay, closeOwner sync.Once
			catalogProducer.referenceLifetime().afterPhysicalRelease(func() {
				counts[0]++
				if state.invokeMu.state.Load()&invocationGateHeld == 0 || state.invocationID == 0 || relay.invocationState.Load()&instanceInvocationCount != 1 {
					panic("relay catalog callback lost public invocation authority")
				}
				closeRelay.Do(func() {
					if err := relay.Close(); err != nil || relay.resourcesClosed {
						panic("relay finalized during protected public invocation")
					}
				})
				isFirst := !firstSet
				if isFirst {
					first, firstSet = markers[0], true
				}
				if nilPanic && isFirst {
					first = nil
					panic(nil)
				}
				panic(markers[0])
			})
			writer.referenceLifetime().afterPhysicalRelease(func() {
				counts[1]++
				if state.invokeMu.state.Load()&invocationGateHeld == 0 || state.invocationID == 0 || relay.invocationState.Load()&instanceInvocationCount != 1 {
					panic("attached callback lost public invocation authority")
				}
				closeOwner.Do(func() {
					if err := owner.Close(); err != nil || owner.resourcesClosed {
						panic("attached owner finalized before relay retirement")
					}
				})
				isFirst := !firstSet
				if isFirst {
					first, firstSet = markers[1], true
				}
				if nilPanic && isFirst {
					first = nil
					panic(nil)
				}
				panic(markers[1])
			})
			relay.referenceLifetime().afterPhysicalRelease(func() {
				counts[2]++
				if state.invokeMu.state.Load()&invocationGateHeld == 0 || state.invocationID == 0 || relay.invocationState.Load()&instanceInvocationCount != 0 {
					panic("relay finalizer lost the held public gate")
				}
				isFirst := !firstSet
				if isFirst {
					first, firstSet = markers[2], true
				}
				if nilPanic && isFirst {
					first = nil
					panic(nil)
				}
				panic(markers[2])
			})
			owner.referenceLifetime().afterPhysicalRelease(func() {
				counts[3]++
				if state.invokeMu.state.Load()&invocationGateHeld == 0 || state.invocationID == 0 || relay.invocationState.Load()&instanceInvocationCount != 0 {
					panic("attached owner finalized outside relay retirement")
				}
				isFirst := !firstSet
				if isFirst {
					first, firstSet = markers[3], true
				}
				if nilPanic && isFirst {
					first = nil
					panic(nil)
				}
				panic(markers[3])
			})
			for index, in := range []*Instance{writer, catalogProducer} {
				if err := in.Close(); err != nil || in.resourcesClosed {
					t.Fatal("logical producer Close consumed retained root", index, err)
				}
			}
			nativeExecutionMu.Lock()
			catalogTable.mu.Lock()
			clear(catalogTable.desc[8:])
			catalogTable.mu.Unlock()
			nativeExecutionMu.Unlock()

			var callErr error
			value, panicked := cleanupTestPanic(func() { _, callErr = relay.Invoke("run") })
			if callErr != nil || !panicked || value != first || counts != [4]int{1, 1, 1, 1} {
				t.Fatal("public cleanup/first panic", value, first, panicked, callErr, counts)
			}
			if state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0 || relay.invocationState.Load() != instanceInvocationClosed {
				t.Fatal("public invocation authority remained after cleanup")
			}
			for _, in := range []*Instance{writer, catalogProducer, owner, relay} {
				funcrefReconcileAssertMappedDrained(t, in)
			}
		})
	}
}

func TestPublicHostScalarBodyPanicStillReconciles(t *testing.T) {
	bodyPanic, cleanupPanic := new(int), new(int)
	writerCode := preparedCleanupCompile(t, "writer", false)
	var owner, writer *Instance
	var table *Table
	hostCalls, cleanupCalls := 0, 0
	imports := NewImports()
	imports.HostFunc("env", "target", func(v int32) int32 {
		hostCalls++
		switch hostCalls {
		case 1:
			var err error
			table, err = owner.ExportedTable("table")
			if err != nil {
				panic(err)
			}
			writer, err = Instantiate(writerCode, InstantiateOptions{Imports: NewImports().Table("env", "table", table)})
			if err != nil {
				panic(err)
			}
			if got, err := writer.Invoke("run"); err != nil || len(got) != 1 || got[0] != 17 {
				panic("actual host-published writer invocation")
			}
			state := owner.ensurePluginState()
			writer.referenceLifetime().afterPhysicalRelease(func() {
				cleanupCalls++
				if state.invokeMu.state.Load()&invocationGateHeld == 0 || state.invocationID == 0 || owner.invocationState.Load()&instanceInvocationCount != 1 {
					panic("host cleanup callback lost public invocation authority")
				}
				panic(cleanupPanic)
			})
			if err := writer.Close(); err != nil || writer.resourcesClosed {
				panic("logical writer Close consumed host-published root")
			}
			// Use a private stale-catalog seam while this callback is parked: clear
			// the physical row but deliberately leave its retained-root catalog for
			// completed-call reconciliation. Avoiding guest table.set keeps mutation
			// out of the oracle; the architecture-specific assertion below records
			// which cleanup boundary consumes the stale catalog.
			table.mu.Lock()
			clear(table.desc[8:])
			table.mu.Unlock()
			return v + 1
		case 2:
			panic(bodyPanic)
		default:
			return v + 1
		}
	})
	owner = preparedCleanupInstantiate(t, publicCleanupHostCompile(t), imports)
	t.Cleanup(func() {
		if table != nil {
			cleanupTestPanic(func() { _ = table.Close() })
		}
		if writer != nil {
			cleanupTestPanic(func() { _ = writer.Close() })
		}
	})
	state := owner.ensurePluginState()
	ic := owner.findInvokeCache("run")
	if ic == nil {
		var err error
		ic, err = owner.fillInvokeCache("run")
		if err != nil {
			t.Fatal(err)
		}
	}
	executionFlags := owner.executionFlags.Load()
	privateScalar := invokePrivateEntryEnabled && ic.entryMode != preparedEntryGeneral && executionFlags&preparedFastBlocked == 0
	hostScalar := !privateScalar && ic.li >= 0 && owner.syncMode && !owner.threadedMemoryZero && owner.usesIndependentExecution() &&
		(owner.hasSingleDirectTypedScalarHost() || owner.hasSingleExpandedTypedScalarHost()) && !ic.hasFuncRefParams && !ic.hasFuncRefResults &&
		owner.table == nil && !owner.importsFuncrefStorage() && len(owner.hostLog) == 0
	if !hostScalar {
		t.Fatalf("fixture lacks independent host-scalar public entry: private=%t local=%d sync=%t threaded=%t independent=%t typed=%t refs=%t/%t table=%p imports-table=%t log=%d flags=%#x",
			privateScalar, ic.li, owner.syncMode, owner.threadedMemoryZero, owner.usesIndependentExecution(),
			owner.hasSingleDirectTypedScalarHost() || owner.hasSingleExpandedTypedScalarHost(), ic.hasFuncRefParams, ic.hasFuncRefResults,
			owner.table, owner.importsFuncrefStorage(), len(owner.hostLog), executionFlags)
	}
	var callErr error
	value, panicked := cleanupTestPanic(func() {
		_, callErr = owner.invokeEntry("run", []uint64{41}, invocationContextSet{}, false, true)
	})
	wantPanic := any(bodyPanic)
	if runtime.GOARCH == "riscv64" {
		// RV64's native-entry unwind currently performs this table reconciliation
		// before the ordinary host panic reaches the outer Go journal. Preserve and
		// assert that separate native-scope ordering here; AMD64/ARM64 exercise the
		// newly journaled post-body host-scalar reconciliation above that boundary.
		wantPanic = cleanupPanic
	}
	if callErr != nil || !panicked || value != wantPanic || hostCalls != 2 || cleanupCalls != 1 {
		t.Fatal("host body/cleanup first panic", value, wantPanic, panicked, callErr, hostCalls, cleanupCalls)
	}
	if state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0 || owner.invocationState.Load()&instanceInvocationCount != 0 {
		t.Fatal("host body panic retained public invocation authority")
	}
	if got, err := owner.Invoke("run", 41); err != nil || len(got) != 1 || got[0] != 42 || hostCalls != 4 {
		t.Fatal("host reuse after body/cleanup panic", got, err, hostCalls)
	}
	funcrefReconcileAssertMappedDrained(t, writer)
}

func TestPublicInvocationCleanupAcquisitionRollback(t *testing.T) {
	in := &Instance{refStore: new(referenceStore)}
	in.executionFlags.Store(executionFlagDynamicGCDomain)
	value, panicked := cleanupTestPanic(func() {
		_, _ = in.invokeWithToken("unused", nil, invocationContextSet{}, newInvocationID(), false, false, nil)
	})
	state := in.ensurePluginState()
	if !panicked || !strings.Contains(fmt.Sprint(value), "dynamic Runtime GC invocation has no topology") {
		t.Fatal("missing-topology acquisition panic", value, panicked)
	}
	if state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0 || in.invocationState.Load()&instanceInvocationCount != 0 {
		t.Fatal("failed public lease acquisition retained authority", state.invokeMu.state.Load(), state.invocationID, in.invocationState.Load())
	}
}

func TestLocalInvocationCleanupMappedClose(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			owner := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "owner-fixed", artifact), NewImports())
			table, err := owner.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { cleanupTestPanic(func() { _ = table.Close() }) })
			writer := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "writer", artifact), NewImports().Table("env", "table", table))
			if got, err := writer.Invoke("run"); err != nil || len(got) != 1 || got[0] != 17 {
				t.Fatal("actual local-call writer", got, err)
			}
			var first any = new(int)
			later := new(int)
			if os.Getenv("WAGO_PUBLIC_INVOCATION_CLEANUP_NIL_PANIC") == "1" {
				first = nil
			}
			counts := [2]int{}
			writer.referenceLifetime().afterPhysicalRelease(func() {
				counts[0]++
				if owner.invocationState.Load()&instanceInvocationCount != 1 {
					panic("local reconciliation lost invocation lifetime")
				}
				if err := owner.Close(); err != nil || owner.resourcesClosed {
					panic("local owner finalized during protected invocation")
				}
				if first == nil {
					panic(nil)
				}
				panic(first)
			})
			owner.referenceLifetime().afterPhysicalRelease(func() {
				counts[1]++
				if owner.invocationState.Load()&instanceInvocationCount != 0 {
					panic("local owner finalized before lifetime release")
				}
				panic(later)
			})
			if err := writer.Close(); err != nil || writer.resourcesClosed {
				t.Fatal("logical writer Close consumed local-call root", err)
			}
			var callErr error
			value, panicked := cleanupTestPanic(func() {
				_, callErr = owner.invokeLocalContext(0, nil, invocationContextSet{}, owner.trap)
			})
			if callErr != nil || !panicked || value != first || counts != [2]int{1, 1} {
				t.Fatal("local invocation cleanup/first panic", value, first, panicked, callErr, counts)
			}
			if owner.invocationState.Load() != instanceInvocationClosed {
				t.Fatal("local invocation lifetime remained after cleanup")
			}
			for _, in := range []*Instance{writer, owner} {
				funcrefReconcileAssertMappedDrained(t, in)
			}
		})
	}
}

func TestVoidInvocationCleanupMappedReconcile(t *testing.T) {
	owner := preparedCleanupInstantiate(t, publicCleanupVoidCompile(t), NewImports())
	table, err := owner.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupTestPanic(func() { _ = table.Close() }) })
	writer := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "writer", false), NewImports().Table("env", "table", table))
	if got, err := writer.Invoke("run"); err != nil || len(got) != 1 || got[0] != 17 {
		t.Fatal("actual void-entry writer", got, err)
	}
	first := new(int)
	callbacks := 0
	state := owner.lockInvocation(0)
	gateOwned := true
	t.Cleanup(func() {
		if gateOwned {
			state.unlockInvocation()
		}
	})
	if err := owner.beginInvocation(); err != nil {
		t.Fatal(err)
	}
	lifetimeOwned := true
	t.Cleanup(func() {
		if lifetimeOwned {
			cleanupTestPanic(owner.endInvocation)
		}
	})
	writer.referenceLifetime().afterPhysicalRelease(func() {
		callbacks++
		if state.invokeMu.state.Load()&invocationGateHeld == 0 || state.invocationID == 0 || owner.invocationState.Load()&instanceInvocationCount != 1 {
			panic("void reconciliation lost caller-owned authority")
		}
		panic(first)
	})
	if err := writer.Close(); err != nil || writer.resourcesClosed {
		t.Fatal("logical writer Close consumed void-entry root", err)
	}
	entry := owner.base + uintptr(owner.c.Entry[0])
	var callErr error
	value, panicked := cleanupTestPanic(func() { callErr = owner.invokeVoidEntry(nil, entry, nil) })
	if callErr != nil || !panicked || value != first || callbacks != 1 {
		t.Fatal("void invocation cleanup", value, panicked, callErr, callbacks)
	}
	if state.invokeMu.state.Load()&invocationGateHeld == 0 || state.invocationID == 0 || owner.invocationState.Load()&instanceInvocationCount != 1 {
		t.Fatal("void cleanup changed caller-owned authority")
	}
	lifetimeOwned = false
	owner.endInvocation()
	gateOwned = false
	state.unlockInvocation()
	funcrefReconcileAssertMappedDrained(t, writer)
}

func TestPublicInvocationCleanupAllocations(t *testing.T) {
	owner := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "owner-fixed", false), NewImports())
	if _, err := owner.ExportedTable("table"); err != nil {
		t.Fatal(err)
	}
	invoke := func() {
		got, err := owner.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 17 {
			panic("actual public invocation result")
		}
	}
	invoke()
	if allocations := testing.AllocsPerRun(100, invoke); allocations != 0 {
		t.Fatal("warm public invocation allocates", allocations)
	}
	local := func() {
		got, err := owner.invokeLocalContext(0, nil, invocationContextSet{}, owner.trap)
		if err != nil || len(got) != 1 || got[0] != 17 {
			panic("actual local invocation result")
		}
	}
	local()
	if allocations := testing.AllocsPerRun(100, local); allocations != 0 {
		t.Fatal("warm local invocation allocates", allocations)
	}
}
