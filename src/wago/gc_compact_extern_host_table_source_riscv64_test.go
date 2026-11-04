//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
	"unsafe"
)

func TestRV64CompactExternHostTablePrivate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c, err := NewRuntimeConfig().Compile(rv64CompactExternBulkModule(vector, true))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()

		for artifact, candidate := range []*Compiled{c} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact != 0, profile), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					mod, err := rt.Module(candidate)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					table, err := rt.NewExternRefTable(4, 8)
					if err != nil {
						t.Fatal(err)
					}
					defer table.Close()
					cfg := GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}
					instantiate := func(config GCConfig) (*Instance, error) {
						return rt.Instantiate(context.Background(), mod, WithImports(NewImports().Table("env", "table", table)), WithGC(config))
					}
					first, err := instantiate(cfg)
					if err != nil {
						t.Fatal(err)
					}
					got, err := first.Invoke("make", 42)
					if err != nil || len(got) != 1 {
						t.Fatal(got, err)
					}
					token := got[0]
					if _, err = first.Invoke("fill", 0, token, 4); err != nil {
						t.Fatal(err)
					}
					if !rt.ReleaseExternRef(ExternRef{token}) {
						t.Fatal("release converted public token")
					}
					row := compactExternHostTableRow(table.owner)
					if row == nil || row.domain.collector != first.gc {
						t.Fatal("host table lacks exact collector claim")
					}
					collector, domain := first.gc, row.domain
					if err := table.Close(); err == nil {
						t.Fatal("host table closed with a live importer")
					}
					if err := first.Close(); err != nil {
						t.Fatal(err)
					}
					first.lifeMu.Lock()
					closed := first.resourcesClosed
					first.lifeMu.Unlock()
					if !closed {
						t.Fatal("host table retained its closed importer")
					}
					if rt.refStore.gcDomainForCollector(collector) != domain {
						t.Fatal("host table lost its collector after importer Close")
					}
					second, err := instantiate(cfg)
					if err != nil {
						t.Fatal(err)
					}
					defer second.Close()
					if second.gc != collector {
						t.Fatal("later importer changed host table collector")
					}
					if err := second.CollectGC(); err != nil {
						t.Fatal(err)
					}
					got, err = second.Invoke("read", 3)
					if err != nil || len(got) != 1 || got[0] != 42 {
						t.Fatal("retained host table object", got, err)
					}
					bad := cfg
					if profile == gc.ProfileTiny {
						bad.Profile = gc.ProfileThroughput
					} else {
						bad.Profile = gc.ProfileTiny
					}
					table.owner.mu.Lock()
					before := table.owner.importers
					table.owner.mu.Unlock()
					if rejected, err := instantiate(bad); err == nil {
						rejected.Close()
						t.Fatal("incompatible host table collector was accepted")
					}
					table.owner.mu.Lock()
					after := table.owner.importers
					table.owner.mu.Unlock()
					if before != after || compactExternHostTableRow(table.owner) != row {
						t.Fatal("failed attachment changed the host table claim")
					}
					foreign, err := rt.NewExternRef("host table foreign value")
					if err != nil {
						t.Fatal(err)
					}
					if _, err = second.Invoke("fill", 0, foreign.token, 4); err != nil {
						t.Fatal(err)
					}
					if rt.ReleaseExternRef(foreign) {
						t.Fatal("host table lost ordinary extern anchor")
					}
					function, err := second.WasmFunc("fill")
					if err != nil {
						t.Fatal(err)
					}
					session, err := function.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					allocations := testing.AllocsPerRun(1000, func() {
						if _, err := session.Invoke3(0, foreign.token, 4); err != nil {
							panic(err)
						}
					})
					session.Close()
					if allocations != 0 {
						t.Fatal("warm host extern table fill allocations", allocations)
					}

					if _, err = second.Invoke("copy", 1, 0, 3); err != nil {
						t.Fatal(err)
					}
					if _, err = second.Invoke("fill", 0, 0, 4); err != nil {
						t.Fatal(err)
					}
					if err := second.CollectGC(); err != nil {
						t.Fatal(err)
					}
					if !rt.ReleaseExternRef(foreign) {
						t.Fatal("cleared host table retained foreign value")
					}
					if err := second.Close(); err != nil {
						t.Fatal(err)
					}
					// Hold a test-only domain claim while successive host resources
					// reuse their root indexes; no extra Instance selects another domain.
					rt.refStore.mu.Lock()
					domain.claims++
					rt.refStore.mu.Unlock()
					heldClaim := true
					defer func() {
						if heldClaim {
							rt.refStore.releaseUnclaimedGCCollector(collector)
						}
					}()

					table.owner.mu.Lock()
					table.owner.nativeLeases++
					table.owner.mu.Unlock()
					if err := table.Close(); err == nil {
						t.Fatal("host table closed with native storage lease")
					}
					table.owner.mu.Lock()
					table.owner.nativeLeases--
					table.owner.mu.Unlock()
					if err := table.Close(); err != nil {
						t.Fatal(err)
					}
					state, _ := compactExternCollectors.Load(collector)
					if state.hostSlots == nil || len(state.hostSlots.free) != 4 {
						t.Fatal("closed host table slots were not returned")
					}
					pool := state.hostSlots
					for cycle := 0; cycle < 8; cycle++ {
						next, err := rt.NewExternRefTable(4, 8)
						if err != nil {
							t.Fatal(err)
						}
						if err := railshotAttachCompactExternHostTable(next, rt.refStore, collector); err != nil {
							t.Fatal(err)
						}
						child, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Table("env", "table", next)), WithGC(cfg))
						if err != nil {
							t.Fatal(err)
						}
						value, err := child.Invoke("make", 42)
						if err != nil {
							t.Fatal(err)
						}
						token := value[0]
						if _, err = child.Invoke("fill", 0, token, 4); err != nil {
							t.Fatal(err)
						}
						if !rt.ReleaseExternRef(ExternRef{token}) {
							t.Fatal("release reuse token")
						}
						if len(pool.free) != 0 {
							t.Fatal("host table failed to reuse free root slots")
						}
						if err := child.Close(); err != nil {
							t.Fatal(err)
						}
						if err := next.Close(); err != nil {
							t.Fatal(err)
						}
						if len(pool.free) != 4 {
							t.Fatal("host root pool grew with table lifetimes")
						}
					}
					heldClaim = false
					rt.refStore.releaseUnclaimedGCCollector(collector)

					if err := table.Close(); err != nil {
						t.Fatal(err)
					}
					if compactExternHostTableRow(table.owner) != nil || rt.refStore.gcDomainForCollector(collector) != nil {
						t.Fatal("closed host table retained its domain claim")
					}
				})
			}
		}
	}
}

func TestRV64CompactExternHostTablePlainConsumerPrivate(t *testing.T) {
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.ExternRef}))),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x6f, 1, 4, 8))),
		wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x25, 0, 0x0b}))))
	plain, err := NewRuntimeConfig().Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	producer, err := NewRuntimeConfig().Compile(rv64CompactExternBulkModule(false, true))
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	rt := NewRuntime()
	defer rt.Close()
	table, err := rt.NewExternRefTable(4, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	pm, err := rt.Module(producer)
	if err != nil {
		t.Fatal(err)
	}
	defer pm.Close()
	first, err := rt.Instantiate(context.Background(), pm, WithImports(NewImports().Table("env", "table", table)))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	got, err := first.Invoke("make", 42)
	if err != nil {
		t.Fatal(err)
	}
	token := got[0]
	if _, err = first.Invoke("fill", 0, token, 4); err != nil {
		t.Fatal(err)
	}
	if !rt.ReleaseExternRef(ExternRef{token}) {
		t.Fatal("release converted token")
	}
	cm, err := rt.Module(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer cm.Close()
	consumer, err := rt.Instantiate(context.Background(), cm, WithImports(NewImports().Table("env", "table", table)))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	result, err := consumer.Invoke("get", 0)
	if err != nil {
		t.Fatalf("plain extern consumer could not return converted table value: %v (result=%v)", err, result)
	}
	if len(result) != 1 || result[0] == 0 {
		t.Fatal("plain extern consumer result", result)
	}
	if !rt.ReleaseExternRef(ExternRef{result[0]}) {
		t.Fatal("release plain consumer result")
	}
}

func rv64CompactExternHostCycleSource(vector bool) []byte {
	target := []byte{0xfb, 1, 1, 0x1a, 0x41, 0, 0x25, 0, 0xfb, 26, 0xfb, 22, 1, 0xfb, 2, 1, 1, 0x0b}
	save := []byte{0x41, 0, 0xd2, 0, 0x41, 42, 0xfb, 0, 1, 0xfb, 27, 0x26, 0, 0x0b}
	if vector {
		prefix := append([]byte{0xfd, 12}, make([]byte, 16)...)
		target = append(append(prefix, 0x1a), target...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), []byte{0x5f, 2, 0x63, 0, 0, 0x7f, 0}, wasmtest.FuncType(nil, nil), []byte{0x60, 0, 1, 0x63, 0})),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x6f, 1, 4, 8))),
		wasmtest.Section(3, []byte{3, 0, 2, 3}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("target", 0, 0), wasmtest.ExportEntry("save", 0, 1), wasmtest.ExportEntry("function", 0, 2))),
		wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(target), wasmtest.Code(save), wasmtest.Code([]byte{0xd2, 0, 0x0b}))),
	)
}

func TestRV64CompactExternHostTableFunctionCyclePrivate(t *testing.T) {
	source := rv64CompactExternHostCycleSource(false)
	c, err := NewRuntimeConfig().Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	rt := NewRuntime()
	defer rt.Close()
	table, err := rt.NewExternRefTable(4, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	mod, err := rt.Module(c)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Table("env", "table", table)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if row := compactExternHostTableRow(table.owner); row != nil {
			row.domain.mu.Lock()
			for _, slot := range row.slots {
				_ = in.gc.SetGlobalSlot(slot, gc.Null())
			}
			row.domain.mu.Unlock()
			lease := in.lockManagedFunctionCollectionInvocation(newInvocationID())
			_, _ = collectManagedFunctionGraph(in, in.publicGCState(), true)
			lease.unlock()
			drainManagedFunctionReclaims(in.gc)
		}
	}()
	if _, err = in.Invoke("save"); err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := table.Close(); err != nil {
		t.Fatalf("closed host-table function cycle could not be released: %v", err)
	}
	in.lifeMu.Lock()
	closed := in.resourcesClosed
	in.lifeMu.Unlock()
	if !closed {
		t.Fatal("closed host table retained cyclic importer")
	}
}

func TestRV64CompactExternHostTableHeldFunctionPrivate(t *testing.T) {
	callerSource := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), []byte{0x60, 1, 0x63, 0, 1, 0x7f})), wasmtest.Section(3, []byte{1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x14, 0, 0x0b}))))
	caller, err := NewRuntimeConfig().Compile(callerSource)
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	for _, vector := range []bool{false, true} {
		source, err := NewRuntimeConfig().Compile(rv64CompactExternHostCycleSource(vector))
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()

		for artifact, candidate := range []*Compiled{source} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact != 0, profile), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					table, err := rt.NewExternRefTable(4, 8)
					if err != nil {
						t.Fatal(err)
					}
					defer table.Close()
					mod, err := rt.Module(candidate)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					producer, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Table("env", "table", table)), WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}))
					if err != nil {
						t.Fatal(err)
					}
					if _, err = producer.Invoke("save"); err != nil {
						t.Fatal(err)
					}
					result, err := producer.Invoke("function")
					if err != nil {
						t.Fatal(err)
					}
					token := result[0]
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					if !vector && artifact == 0 && profile == gc.ProfileThroughput {
						rv64CompactExternRaceHostTableClose(t, table, rt.refStore, producer.gc)
					} else if err := table.Close(); err != nil {
						t.Fatal(err)
					}
					row := compactExternHostTableRow(table.owner)
					if row == nil || !row.logicalClosed.Load() || table.owner.closed {
						t.Fatal("held function lost physical table")
					}
					cm, err := rt.Module(caller)
					if err != nil {
						t.Fatal(err)
					}
					defer cm.Close()
					consumer, err := rt.Instantiate(context.Background(), cm)
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					for i := 0; i < 4; i++ {
						got, err := consumer.Invoke("call", token)
						if err != nil || len(got) != 1 || got[0] != 42 {
							t.Fatal("closed table retained function read", got, err)
						}
					}
					if _, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Table("env", "table", table))); err == nil {
						t.Fatal("new importer attached to logically closed table")
					}
					releaseCompactExternTestFunction(rt.refStore, token)
					drainManagedFunctionStoreReclaims(rt.refStore)
					producer.lifeMu.Lock()
					closed := producer.resourcesClosed
					producer.lifeMu.Unlock()
					if !closed || compactExternHostTableRow(table.owner) != nil {
						t.Fatal("final function token retained logical table cycle")
					}
				})
			}
		}
	}
}

func TestRV64CompactExternHostTableRuntimeClosePrivate(t *testing.T) {
	c, err := NewRuntimeConfig().Compile(rv64CompactExternHostCycleSource(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	rt := NewRuntime()
	defer rt.Close()
	table, err := rt.NewExternRefTable(4, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	mod, err := rt.Module(c)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Table("env", "table", table)))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("save"); err != nil {
		t.Fatal(err)
	}
	result, err := in.Invoke("function")
	if err != nil || len(result) != 1 {
		t.Fatal(result, err)
	}
	collector := in.gc
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := table.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.CloseContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	drainManagedFunctionStoreReclaims(rt.refStore)
	if in.hasPhysicalResources() || compactExternHostTableRow(table.owner) != nil || rt.refStore.gcDomainForCollector(collector) != nil {
		t.Fatal("Runtime.Close retained logically closed host table cycle")
	}
}

// Function tokens normally live until Runtime.Close. This test removes the
// external root early to check that the remaining native cycle is collectible.
func releaseCompactExternTestFunction(store *referenceStore, token uint64) {
	store.mu.Lock()
	entry := store.byToken[token]
	delete(store.byToken, token)
	for identity, alias := range store.byIdentity {
		if alias == entry {
			delete(store.byIdentity, identity)
		}
	}
	store.mu.Unlock()
	if entry != nil {
		var cleanup resourceCleanup
		cleanup.run(func() { releaseFuncrefTokenMetadata(entry) })
		cleanup.run(entry.owner.releaseResourceRoot)
		cleanup.finish()
	}
}

func rv64CompactExternRaceHostTableClose(t *testing.T, table *Table, store *referenceStore, collector *gc.Collector) {
	t.Helper()
	start := make(chan struct{})
	done := make(chan bool, 1)
	go func() {
		<-start
		invalid := false
		for i := 0; i < 128; i++ {
			table.owner.mu.Lock()
			exact := table.owner.valueType
			table.owner.mu.Unlock()
			err := table.attachImporterWithCollector(ValExternRef, exact, nil, store, collector, false)
			if err != nil {
				continue
			}
			table.owner.mu.Lock()
			invalid = invalid || !railshotCompactExternHostTableOpen(table.owner)
			table.owner.mu.Unlock()
			table.detachImporter(store)
		}
		done <- invalid
	}()
	close(start)
	for i := 0; i < 128; i++ {
		_ = table.Close()
	}
	if <-done {
		t.Fatal("new attachment committed after logical close")
	}
	if err := table.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRV64CompactExternHostTableSequentialCyclesPrivate(t *testing.T) {
	t.Logf("cold host row=%d, slot=%d, pool=%d, collector sidecar=%d Go bytes, excluding map backing", unsafe.Sizeof(compactExternHostTable{}), unsafe.Sizeof(compactExternHostSlot{}), unsafe.Sizeof(compactExternHostSlotPool{}), unsafe.Sizeof(compactExternCollector{}))
	for _, vector := range []bool{false, true} {
		source, err := NewRuntimeConfig().Compile(rv64CompactExternHostCycleSource(vector))
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()

		for artifact, candidate := range []*Compiled{source} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact != 0, profile), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					mod, err := rt.Module(candidate)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					var tables [2]*Table
					var owners [2]*Instance
					var collector *gc.Collector
					for i := range tables {
						tables[i], err = rt.NewExternRefTable(4, 8)
						if err != nil {
							t.Fatal(err)
						}
						defer tables[i].Close()
						if collector != nil {
							if err := railshotAttachCompactExternHostTable(tables[i], rt.refStore, collector); err != nil {
								t.Fatal(err)
							}
						}
						owners[i], err = rt.Instantiate(context.Background(), mod, WithImports(NewImports().Table("env", "table", tables[i])), WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}))
						if err != nil {
							t.Fatal(err)
						}
						defer owners[i].Close()
						collector = owners[i].gc
						if _, err = owners[i].Invoke("save"); err != nil {
							t.Fatal(err)
						}
						if err = owners[i].Close(); err != nil {
							t.Fatal(err)
						}
					}
					for i, table := range tables {
						if err := table.Close(); err != nil {
							t.Fatal(err)
						}
						if err := table.Close(); err != nil {
							t.Fatal(err)
						}
						owners[i].lifeMu.Lock()
						closed := owners[i].resourcesClosed
						owners[i].lifeMu.Unlock()
						if !closed || compactExternHostTableRow(table.owner) != nil {
							t.Fatal("closed cycle was retained", i)
						}
						if i == 0 {
							owners[1].lifeMu.Lock()
							live := !owners[1].resourcesClosed
							owners[1].lifeMu.Unlock()
							if !live || compactExternHostTableRow(tables[1].owner) == nil {
								t.Fatal("first close reclaimed the second live host root")
							}
						}
					}
					if rt.refStore.gcDomainForCollector(collector) != nil {
						t.Fatal("last cycle retained collector claim")
					}
				})
			}
		}
	}
}

func rv64CompactExternPlainReentrySource(vector bool) []byte {
	body := []byte{0x20, 0, 0x25, 0, 0x21, 1, 0x20, 0, 0xd0, 0x6f, 0x26, 0, 0x10, 0, 0x20, 1, 0x0b}
	if vector {
		prefix := append([]byte{0xfd, 12}, make([]byte, 16)...)
		body = append(append(prefix, 0x1a), body...)
	}
	raw := append([]byte{1, 1, 0x6f}, body...)
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x6f, 1, 4, 8)
	collectImport := append(append(wasmtest.Name("env"), wasmtest.Name("collect")...), 0, 1)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.ExternRef}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(collectImport, tableImport)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 1))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(raw))), raw...))))
}

func TestRV64CompactExternPlainReentryPrivate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c, err := NewRuntimeConfig().Compile(rv64CompactExternPlainReentrySource(vector))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()

		producerCode, err := NewRuntimeConfig().Compile(rv64CompactExternBulkModule(vector, true))
		if err != nil {
			t.Fatal(err)
		}
		defer producerCode.Close()
		for artifact, candidate := range []*Compiled{c} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact != 0, profile), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					table, err := rt.NewExternRefTable(4, 8)
					if err != nil {
						t.Fatal(err)
					}
					defer table.Close()
					pm, err := rt.Module(producerCode)
					if err != nil {
						t.Fatal(err)
					}
					defer pm.Close()
					cfg := GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}
					producer, err := rt.Instantiate(context.Background(), pm, WithImports(NewImports().Table("env", "table", table)), WithGC(cfg))
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					cm, err := rt.Module(candidate)
					if err != nil {
						t.Fatal(err)
					}
					defer cm.Close()
					calls := 0
					panicHost := false
					panicValue := &struct{ message string }{"plain extern host panic"}
					imports := NewImports().Table("env", "table", table).Function("env", "collect", slotHostFunc(func(host HostModule, _ []uint64, _ []uint64) {
						calls++
						if err := host.(GCHostModule).CollectGC(); err != nil {
							panic(err)
						}
						if panicHost {
							panic(panicValue)
						}
					}))
					consumer, err := rt.Instantiate(context.Background(), cm, WithImports(imports), WithGC(cfg))
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					if consumer.gc != producer.gc || candidate.nativeGCABIRequirement() != gc.NativeABIVersion || candidate.genericGCFrameRoots() == nil {
						t.Fatal("plain extern frame domain missing")
					}
					for i := 0; i < 8; i++ {
						result, err := producer.Invoke("make", 42)
						if err != nil {
							t.Fatal(err)
						}
						if _, err = producer.Invoke("fill", 0, result[0], 1); err != nil {
							t.Fatal(err)
						}
						if !rt.ReleaseExternRef(ExternRef{result[0]}) {
							t.Fatal("release initial token")
						}
						got, err := consumer.Invoke("get", 0)
						if err != nil || len(got) != 1 || got[0] == 0 {
							t.Fatal("local across host collection", got, err)
						}
						if _, err = producer.Invoke("fill", 0, got[0], 1); err != nil {
							t.Fatal(err)
						}
						if !rt.ReleaseExternRef(ExternRef{got[0]}) {
							t.Fatal("release returned token")
						}
						value, err := producer.Invoke("read", 0)
						if err != nil || len(value) != 1 || value[0] != 42 {
							t.Fatal("collected value changed", value, err)
						}
						if _, err = producer.Invoke("fill", 0, 0, 4); err != nil {
							t.Fatal(err)
						}
						if err := consumer.CollectGC(); err != nil {
							t.Fatal(err)
						}
					}
					if calls != 8 {
						t.Fatal("host calls", calls)
					}
					result, err := producer.Invoke("make", 42)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = producer.Invoke("fill", 0, result[0], 1); err != nil {
						t.Fatal(err)
					}
					if !rt.ReleaseExternRef(ExternRef{result[0]}) {
						t.Fatal("release panic seed")
					}
					panicHost = true
					caught, panicked := cleanupTestPanic(func() { _, _ = consumer.Invoke("get", 0) })
					if !panicked || caught != panicValue {
						t.Fatal("host panic changed", caught)
					}
					panicHost = false
					if err := consumer.CollectGC(); err != nil {
						t.Fatal(err)
					}
					state, _ := compactExternCollectors.Load(consumer.gc)
					if state != nil && (len(state.shells) != 0 || len(state.foreign) != 0) {
						t.Fatal("panic retained dead extern locals")
					}
					got, err := consumer.Invoke("get", 0)
					if err != nil || len(got) != 1 || got[0] != 0 {
						t.Fatal("resume after panic", got, err)
					}
				})
			}
		}
	}
}
