//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"slices"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicManyImportedTableLeases(t *testing.T) {
	for _, count := range []int{258, 1024} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("tables%d/artifact%t", count, artifact), func(t *testing.T) {
				entries := make([][]byte, count)
				imports := NewImports()
				tables := make([]*Table, count)
				for i := range entries {
					name := fmt.Sprintf("t%d", i)
					entries[i] = append(append(wasmtest.Name("env"), wasmtest.Name(name)...), 1, 0x70, 1, 0, 0)
					table, err := NewTable(0, 0)
					if err != nil {
						t.Fatal(err)
					}
					tables[i] = table
					t.Cleanup(func() { table.Close() })
					imports.Table("env", name, table)
				}
				source := wasmtest.Module(
					wasmtest.Section(1, []byte{1, 0x60, 0, 1, 0x7f}),
					wasmtest.Section(2, wasmtest.Vec(entries...)),
					wasmtest.Section(3, []byte{1, 0}),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x0b}))),
				)
				c := rv64TableStageCompile(t, fmt.Sprintf("many-table-leases-%d", count), source, artifact)
				in, err := Instantiate(c, imports)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				for cycle := 0; cycle < 2; cycle++ {
					out, err := in.Invoke("run")
					if err != nil || len(out) != 1 || out[0] != 42 {
						t.Fatal("many table invocation", out, err)
					}
					for i, table := range tables {
						table.owner.mu.Lock()
						leases := table.owner.nativeLeases
						table.owner.mu.Unlock()
						table.mu.Lock()
						retained := len(table.retained)
						table.mu.Unlock()
						if leases != 0 || retained != 0 {
							t.Fatalf("table%d leaked lease%d retained%d", i, leases, retained)
						}
					}
				}
			})
		}
	}
}

func TestRV64TableScopeFixedFootprint(t *testing.T) {
	type priorScope struct {
		owners          [257]*Instance
		writers         [257]*Table
		writerOwners    [257]uint16
		storage         [257]*Table
		count           int
		tailBudget      rv64NativeTailGenerationBudget
		tailGenerations []*rv64NativeTailGeneration
		tailActive      []*rv64NativeTailGeneration
		tailOwnerCount  int
		tailRetiring    bool
	}
	current, prior := unsafe.Sizeof(rv64NativeTableTargets{}), unsafe.Sizeof(priorScope{})
	t.Logf("retained table scope fixed bytes: prior=%d current=%d", prior, current)
	const samples = 128
	oldValues := make([]*priorScope, samples)
	newValues := make([]*rv64NativeTableTargets, samples)
	previousGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previousGC)
	var before, middle, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := range oldValues {
		oldValues[i] = new(priorScope)
	}
	runtime.ReadMemStats(&middle)
	for i := range newValues {
		newValues[i] = new(rv64NativeTableTargets)
	}
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(oldValues)
	runtime.KeepAlive(newValues)
	oldBytes, newBytes := middle.TotalAlloc-before.TotalAlloc, after.TotalAlloc-middle.TotalAlloc
	t.Logf("scope allocation samples=%d prior-total=%dB current-total=%dB prior/slot=%dB current/slot=%dB", samples, oldBytes, newBytes, oldBytes/samples, newBytes/samples)
	if newBytes >= oldBytes {
		t.Fatal("scope allocation cost did not decrease", oldBytes, newBytes)
	}
	if current >= prior {
		t.Fatalf("table scope fixed footprint increased: prior=%d current=%d", prior, current)
	}
}

func TestRV64PublicManyNativeTableOwners(t *testing.T) {
	const count = 258
	producerSource := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x0b}))),
	)

	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			p := rv64TableStageCompile(t, "many-native-owner-producer", producerSource, artifact)
			c := rv64TableStageCompile(t, "many-native-owner-consumer", manyNativeTableOwnerSource(2), artifact)
			relayCode := rv64TableStageCompile(t, "many-native-owner-relay", manyNativeTableOwnerSource(count/2), artifact)
			imports := NewImports()
			owners := make([]*Instance, count)
			for i := range owners {
				var err error
				owners[i], err = Instantiate(p)
				if err != nil {
					t.Fatal(err)
				}
				defer owners[i].Close()

			}
			for group := 0; group < 2; group++ {
				relayImports := NewImports()
				for i := 0; i < count/2; i++ {
					fn, err := owners[group*(count/2)+i].ExportedFunc("run")
					if err != nil {
						t.Fatal(err)
					}
					relayImports.Function("env", fmt.Sprint(i), fn)
				}
				relay, err := Instantiate(relayCode, relayImports)
				if err != nil {
					t.Fatal(err)
				}
				defer relay.Close()
				owners = append(owners, relay)
				fn, err := relay.ExportedFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				imports.Function("env", fmt.Sprint(group), fn)
			}

			consumer, err := Instantiate(c, imports)
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			roots := make([]int, len(owners))
			for i, owner := range owners {
				roots[i] = owner.resourceRefs
			}
			for i := 0; i < 2; i++ {
				if out, err := consumer.Invoke("run"); err != nil || !slices.Equal(out, []uint64{42}) {
					t.Fatal("many native table owners", out, err)
				}
				for index, owner := range owners {
					if owner.resourceRefs != roots[index] {
						t.Fatal("native owner scope leaked a root", index, owner.resourceRefs, roots[index])
					}
				}
			}
		})
	}
}

func manyNativeTableOwnerSource(count int) []byte {
	var declarations [][]byte
	for i := 0; i < count; i++ {
		declarations = append(declarations, append(append(wasmtest.Name("env"), wasmtest.Name(fmt.Sprint(i))...), 0, 0))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(declarations...)),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, uint32(count)))),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, uint32(count-1)))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x11, 0, 0, 0x0b}))),
	)
}

func TestRV64NativeOwnerPreflightLongChainAndCycle(t *testing.T) {
	owners := make([]Instance, 300)
	for i := range owners {
		owners[i].c = &Compiled{}
		if i != 0 {
			owners[i-1].c.Imports = []string{"env.next"}
			owners[i-1].c.NumImports = 1
			owners[i-1].imports = resolvedImports{importBindingMapKey("env", "next"): &InstanceExport{inst: &owners[i]}}
		}
	}
	if needed, err := rv64NeedsTableEntry(&owners[0]); err != nil || needed {
		t.Fatal("numeric chain requires no owner scope", needed, err)
	}
	last := &owners[len(owners)-1]
	last.c.nativeReferenceFunctionABI = true
	if needed, err := rv64NeedsTableEntry(&owners[0]); err != nil || !needed {
		t.Fatal("last owner was not inspected", needed, err)
	}
	last.c.nativeReferenceFunctionABI = false
	last.c.Imports = []string{"env.next"}
	last.c.NumImports = 1
	last.imports = resolvedImports{importBindingMapKey("env", "next"): &InstanceExport{inst: &owners[0]}}
	if needed, err := rv64NeedsTableEntry(&owners[0]); err != nil || needed {
		t.Fatal("numeric attachment cycle", needed, err)
	}
}

// Construction publishes borrowed batches without adding an activation lease.
// More than two inline batches must retain every distinct producer through Close.
func TestRV64PublicImportedGlobalTableOwnerBatches(t *testing.T) {
	const count = 17
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			table, err := NewTable(count, count)
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			bindings := NewImports().Table("env", "table", table)
			declarations := [][]byte{append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, count, count)}
			producers := make([]*Instance, count)
			globals := make([]*Global, count)
			element := []byte{6, 0, 0x41, 0, 0x0b, 0x70, count}
			for i := range count {
				source := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
					wasmtest.Section(3, []byte{1, 0}),
					wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd2, 0, 0x0b}))),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("reference", 3, 0))),
					wasmtest.Section(9, []byte{1, 3, 0, 1, 0}),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x41, byte(i), 0x6a, 0x0b}))),
				)
				c := rv64TableStageCompile(t, fmt.Sprintf("global-table-batch-producer%d", i), source, artifact)
				producer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				producers[i] = producer
				global, err := producer.ExportedGlobalObject("reference")
				if err != nil {
					t.Fatal(err)
				}
				globals[i] = global
				name := fmt.Sprintf("g%d", i)
				bindings.Global("env", name, global)
				declarations = append(declarations, wasmtest.GlobalImportEntry("env", name, wasm.FuncRef, false))
				element = append(element, 0x23, byte(i), 0x0b)
			}
			c := rv64TableStageCompile(t, "global-table-batch-writer", wasmtest.Module(
				wasmtest.Section(2, wasmtest.Vec(declarations...)),
				wasmtest.Section(9, wasmtest.Vec(element)),
			), artifact)
			writer := rv64NativeTokenInstantiate(t, rt, c, bindings)
			for i, producer := range producers {
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := globals[i].Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			for i, producer := range producers {
				if !producer.hasPhysicalResources() || table.retained[producer] == nil {
					t.Fatal("published owner missing after writer close", i)
				}
			}
			readerSource := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(
					wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
					wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
					wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
				)),
				wasmtest.Section(2, wasmtest.Vec(declarations[0])),
				wasmtest.Section(3, []byte{2, 1, 2}),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0), wasmtest.ExportEntry("clear", 0, 1))),
				wasmtest.Section(10, wasmtest.Vec(
					wasmtest.Code([]byte{0x20, 1, 0x20, 0, 0x11, 0, 0, 0x0b}),
					wasmtest.Code([]byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0x0b}),
				)),
			)
			rc := rv64TableStageCompile(t, "global-table-batch-reader", readerSource, artifact)
			reader := rv64NativeTokenInstantiate(t, rt, rc, NewImports().Table("env", "table", table))
			defer reader.Close()
			for i := range count {
				importedGlobalInvoke(t, reader, "call", []uint64{uint64(42 + i)}, uint64(i), 42)
			}
			for i := range count {
				importedGlobalInvoke(t, reader, "clear", nil, uint64(i))
			}
			for i, producer := range producers {
				if producer.hasPhysicalResources() || table.retained[producer] != nil {
					t.Fatal("cleared table retained owner", i)
				}
			}
		})
	}
}

// Result conversion must inspect the whole retained attachment graph before
// minting a producer's first token, including after that producer closes.
func TestRV64PublicFuncrefResultBeyond256Owners(t *testing.T) {
	const count = 258
	leafSource := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}))),
		wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 1))),
		wasmtest.Section(9, []byte{1, 3, 0, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x0b}), wasmtest.Code([]byte{0xd2, 0, 0x0b}))),
	)
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			leafCode := rv64TableStageCompile(t, "many-funcref-result-leaf", leafSource, artifact)
			relayCode := rv64TableStageCompile(t, "many-funcref-result-relay", manyFuncrefResultSource(count/2), artifact)
			callerCode := rv64TableStageCompile(t, "many-funcref-result-caller", manyFuncrefResultSource(2), artifact)
			leaves := make([]*Instance, count)
			callerImports := NewImports()
			for group := 0; group < 2; group++ {
				relayImports := NewImports()
				for index := 0; index < count/2; index++ {
					leaf := rv64NativeTokenInstantiate(t, rt, leafCode, NewImports())
					leaves[group*(count/2)+index] = leaf
					fn, err := leaf.ExportedFunc("get")
					if err != nil {
						t.Fatal(err)
					}
					relayImports.Function("env", fmt.Sprint(index), fn)
				}
				relay := rv64NativeTokenInstantiate(t, rt, relayCode, relayImports)
				fn, err := relay.ExportedFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				callerImports.Function("env", fmt.Sprint(group), fn)
			}
			caller := rv64NativeTokenInstantiate(t, rt, callerCode, callerImports)
			last := leaves[count-1]
			descriptor, ok := last.localFuncrefDescriptor(0)
			if !ok || len(caller.refStore.byToken) != 0 {
				t.Fatal("fixture must have no issued token", ok, len(caller.refStore.byToken))
			}
			if err := last.Close(); err != nil || !last.hasPhysicalResources() {
				t.Fatal("closed producer lost its attachment root", err)
			}
			if token, err := last.refStore.issue(last, descriptor); err == nil || token != 0 || len(last.refStore.byToken) != 0 {
				t.Fatal("direct issuance revived a closed producer", token, err)
			}
			for cycle := 0; cycle < 2; cycle++ {
				out, err := caller.Invoke("run")
				if err != nil || len(out) != 1 || out[0] == 0 {
					t.Fatal("late owner result", out, err)
				}
				resolved, ok := caller.refStore.resolve(out[0])
				if !ok || resolved != descriptor || len(caller.refStore.byToken) != 1 {
					t.Fatal("late owner result identity", resolved, descriptor, ok, len(caller.refStore.byToken))
				}
			}
			if err := rt.Close(); err != nil {
				t.Fatal(err)
			}
			if err := rt.WaitClosed(context.Background()); err != nil {
				t.Fatal(err)
			}
			for index, leaf := range leaves {
				if leaf.hasPhysicalResources() {
					t.Fatalf("Runtime close retained producer%d refs=%d closed=%t resourcesClosed=%t storeTokens=%d callerRefs=%d callerClosed=%t runtimeInstances=%d", index, leaf.resourceRefs, leaf.closed, leaf.resourcesClosed, len(rt.refStore.byToken), caller.resourceRefs, caller.closed, len(rt.instances))
				}
			}
		})
	}
}

func manyFuncrefResultSource(count int) []byte {
	imports := make([][]byte, count)
	for index := range imports {
		imports[index] = append(append(append(wasmtest.Name("env"), wasmtest.Name(fmt.Sprint(index))...), 0), 0)
	}
	body := append([]byte{0x10}, wasmtest.ULEB(uint32(count-1))...)
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, uint32(count)))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}
