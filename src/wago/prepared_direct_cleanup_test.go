//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"math"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func preparedDirectCleanupCompile(t *testing.T, kind string, artifact bool) *Compiled {
	t.Helper()
	var params, results []wasm.ValType
	var body []byte
	switch kind {
	case "int":
		results = []wasm.ValType{wasm.I32}
		body = []byte{0x41, 17, 0x0b}
	case "float":
		params, results = []wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}
		body = []byte{0x20, 0, 0x0b}
	case "mixed":
		params, results = []wasm.ValType{wasm.I32, wasm.F64}, []wasm.ValType{wasm.F64}
		body = []byte{0x20, 1, 0x0b}
	case "wide":
		params, results = []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}
		body = []byte{0x20, 0, 0x0b}
	default:
		t.Fatal("unknown direct cleanup kind", kind)
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	return c

}

func preparedDirectCleanupCatalog(t *testing.T, owner, probe *Instance, ownerTable *Table, artifact, linkLocal bool) (*Instance, *Table, any, *[2]int) {
	t.Helper()
	catalogTable, err := NewTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupTestPanic(func() { _ = catalogTable.Close() }) })
	producer := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "writer", artifact), NewImports().Table("env", "table", catalogTable))
	if got, err := producer.Invoke("run"); err != nil || len(got) != 1 || got[0] != 17 || !catalogTable.retainProducerInstance(producer) {
		t.Fatal("actual direct-fallback catalog root", got, err)
	}
	owner.funcrefImportContainers().tables.add(catalogTable)
	if linkLocal {
		// Direct eligibility requires a pure numeric module with no declared
		// table. This explicit private handle-chain seam uses a real host table
		// and mapped producer solely to make completed-call reconciliation run;
		// it does not claim public table admission for the numeric module.
		owner.lifeMu.Lock()
		owner.table = catalogTable
		owner.lifeMu.Unlock()
		owner.markNativeControlShared()
	}
	probeTable := ownerTable
	if probeTable == nil {
		probeTable = catalogTable
	}
	state := owner.ensurePluginState()
	var first any = new(int)
	if os.Getenv("WAGO_PREPARED_DIRECT_CLEANUP_NIL_PANIC") == "1" {
		first = nil
	}
	later := new(int)
	counts := new([2]int)
	var closeOnce sync.Once
	producer.referenceLifetime().afterPhysicalRelease(func() {
		counts[0]++
		preparedCleanupProbe(probe, owner, catalogTable)
		if state.invokeMu.state.Load()&invocationGateHeld == 0 || state.invocationID == 0 || owner.invocationState.Load()&instanceInvocationCount != 1 {
			panic("direct fallback producer lost invocation authority")
		}
		closeOnce.Do(func() {
			if err := owner.Close(); err != nil || owner.resourcesClosed {
				panic("direct fallback callback finalized protected owner")
			}
		})
		if first == nil {
			panic(nil)
		}
		panic(first)
	})
	owner.referenceLifetime().afterPhysicalRelease(func() {
		counts[1]++
		preparedCleanupProbe(probe, owner, probeTable)
		if state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0 || owner.invocationState.Load()&instanceInvocationCount != 0 {
			panic("direct fallback owner finalized before authority release")
		}
		panic(later)
	})
	if err := producer.Close(); err != nil || producer.resourcesClosed {
		t.Fatal("logical Close consumed direct-fallback producer", err)
	}
	nativeExecutionMu.Lock()
	catalogTable.mu.Lock()
	clear(catalogTable.desc[8:])
	catalogTable.mu.Unlock()
	nativeExecutionMu.Unlock()
	return producer, catalogTable, first, counts
}

func TestPreparedScalarHostShortcutAllocations(t *testing.T) {
	imports := NewImports()
	imports.HostFunc("env", "target", func(v int32) int32 { return v + 1 })
	owner := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "host", false), imports)
	fn, err := owner.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	invoke := func() {
		got, err := fn.Invoke(41)
		if err != nil || len(got) != 1 || got[0] != 42 {
			panic("actual scalar host shortcut result")
		}
	}
	invoke()
	if fn.hostPrepared == nil || fn.hostFixed == nil {
		t.Fatal("warm call did not prepare scalar host shortcut")
	}
	if allocations := testing.AllocsPerRun(100, invoke); allocations != 0 {
		t.Fatal("warm scalar host shortcut allocates", allocations)
	}
}

func TestPreparedScalarHostShortcutPostLockRevocation(t *testing.T) {
	imports := NewImports()
	imports.HostFunc("env", "target", func(v int32) int32 { return v + 1 })
	owner := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "host", false), imports)
	fn, err := owner.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := fn.Invoke(41); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatal("warm scalar host shortcut", got, err)
	}
	if fn.hostPrepared == nil || fn.hostFixed == nil || !owner.usesIndependentExecution() || !owner.preparedFastStateValid() {
		t.Fatal("fixture lacks warmed scalar host shortcut")
	}

	state := owner.ensurePluginState()
	state.invokeMu.Lock()
	gateOwned := true
	type shortcutResult struct {
		out      []uint64
		err      error
		admitted bool
		panic    any
		panicked bool
	}
	done := make(chan shortcutResult, 1)
	go func() {
		var result shortcutResult
		result.panic, result.panicked = cleanupTestPanic(func() {
			result.out, result.err, result.admitted = fn.tryInvokeScalarHost1([]uint64{41})
		})
		done <- result
	}()
	var table *Table
	workerJoined := false
	t.Cleanup(func() {
		if gateOwned {
			state.invokeMu.Unlock()
			gateOwned = false
		}
		if !workerJoined {
			select {
			case got := <-done:
				workerJoined = true
				if got.panicked {
					t.Errorf("post-lock shortcut worker panic: %v", got.panic)
				}
			case <-time.After(time.Second):
				t.Error("post-lock shortcut worker did not finish during cleanup")
			}
		}
		if table != nil {
			cleanupTestPanic(func() { _ = table.Close() })
		}
	})
	deadline := time.Now().Add(time.Second)
	for owner.invocationState.Load()&instanceInvocationCount != 1 {
		if time.Now().After(deadline) {
			t.Fatal("host shortcut did not acquire its lifetime lease")
		}
		time.Sleep(time.Millisecond)
	}

	// Export the genuine local table while the shortcut waits for the same
	// ordinary gate. Publication revokes independent prepared execution before
	// the shortcut can perform its post-lock eligibility check.
	table, err = owner.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	if owner.table != table || owner.usesIndependentExecution() || owner.preparedFastStateValid() {
		t.Fatal("local table publication did not revoke the shortcut")
	}
	state.invokeMu.Unlock()
	gateOwned = false

	select {
	case got := <-done:
		workerJoined = true
		if got.panicked || got.out != nil || got.err != nil || got.admitted {
			t.Fatal("post-lock revocation result", got.out, got.err, got.admitted, got.panic, got.panicked)
		}
	case <-time.After(time.Second):
		t.Fatal("post-lock revoked shortcut did not return")
	}
	if state.invokeMu.state.Load()&invocationGateHeld != 0 || state.invocationID != 0 || owner.invocationState.Load()&instanceInvocationCount != 0 {
		t.Fatal("post-lock revocation retained invocation authority")
	}
	if got, err := fn.Invoke(41); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatal("public fallback after post-lock revocation", got, err)
	}
}

func TestPreparedDirectFallbackCleanupMappedClose(t *testing.T) {
	for _, kind := range []string{"int", "float", "mixed", "wide"} {
		t.Run(kind, func(t *testing.T) {
			artifact := false
			owner := preparedCleanupInstantiate(t, preparedDirectCleanupCompile(t, kind, artifact), NewImports())
			probe := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "numeric", artifact), NewImports())
			probe.markNativeControlShared()
			fn, err := owner.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "int":
				if !fn.directIntFast {
					t.Fatal("integer direct route not selected")
				}
			case "float":
				if !fn.directFloatFast {
					t.Fatal("float direct route not selected")
				}
			case "mixed":
				if fn.directMixedInfo == 0 {
					t.Fatal("mixed direct route not selected")
				}
			case "wide":
				if !fn.directIntFast || fn.paramSlots <= 4 {
					t.Fatal("wide direct route not selected")
				}
			}
			if !fn.directIsolated {
				t.Fatal("fixture lacks revocable isolated direct admission")
			}
			producer, _, first, counts := preparedDirectCleanupCatalog(t, owner, probe, nil, artifact, true)
			args := []uint64(nil)
			switch kind {
			case "float":
				args = []uint64{math.Float64bits(3.5)}
			case "mixed":
				args = []uint64{7, math.Float64bits(3.5)}
			case "wide":
				args = []uint64{17, 2, 3, 4, 5}
			}
			var callErr error
			value, panicked := cleanupTestPanic(func() { _, callErr = fn.Invoke(args...) })
			if callErr != nil || !panicked || value != first || *counts != [2]int{1, 1} {
				t.Fatal("direct fallback cleanup/first panic", value, first, panicked, callErr, *counts)
			}
			funcrefReconcileAssertMappedDrained(t, producer)
			funcrefReconcileAssertMappedDrained(t, owner)
		})
	}
}

func TestPreparedScalarHostShortcutCleanupMappedClose(t *testing.T) {
	for _, artifact := range []bool{false} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			var owner *Instance
			armed := false
			imports := NewImports()
			imports.HostFunc("env", "target", func(v int32) int32 {
				if armed {
					if _, err := owner.ExportedTable("table"); err != nil {
						panic(HostTrap{Err: err})
					}
				}
				return v + 1
			})
			owner = preparedCleanupInstantiate(t, preparedCleanupCompile(t, "host", artifact), imports)
			probe := preparedCleanupInstantiate(t, preparedCleanupCompile(t, "numeric", artifact), NewImports())
			probe.markNativeControlShared()
			fn, err := owner.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			if got, err := fn.Invoke(41); err != nil || len(got) != 1 || got[0] != 42 {
				t.Fatal("warm scalar host shortcut", got, err)
			}
			if !fn.scalarFast || fn.hostPrepared == nil || fn.hostFixed == nil || owner.gc != nil || !owner.usesIndependentExecution() || !owner.preparedFastStateValid() {
				t.Fatalf("fixture lacks scalar host shortcut admission: scalar=%t prepared=%t fixed=%t gc=%t independent=%t valid=%t sync=%t bounded=%t",
					fn.scalarFast, fn.hostPrepared != nil, fn.hostFixed != nil, owner.gc != nil, owner.usesIndependentExecution(), owner.preparedFastStateValid(), owner.syncMode, fn.boundedNumericHost)
			}
			producer, _, first, counts := preparedDirectCleanupCatalog(t, owner, probe, nil, artifact, false)
			armed = true
			var callErr error
			value, panicked := cleanupTestPanic(func() { _, callErr = fn.Invoke(41) })
			if callErr != nil || !panicked || value != first || *counts != [2]int{1, 1} {
				t.Fatal("scalar host shortcut cleanup/first panic", value, first, panicked, callErr, *counts)
			}
			if owner.table == nil {
				t.Fatal("host callback did not publish the genuine local table")
			}
			t.Cleanup(func() { cleanupTestPanic(func() { _ = owner.table.Close() }) })
			funcrefReconcileAssertMappedDrained(t, producer)
			funcrefReconcileAssertMappedDrained(t, owner)
		})
	}
}
