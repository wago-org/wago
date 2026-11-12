//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"

	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64CompactExternHostGlobalSource(vector bool) []byte {
	save := []byte{0x20, 0, 0xfb, 0, 0, 0xfb, 27, 0x24, 0, 0x0b}
	read := []byte{0x23, 0, 0xfb, 26, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b}
	if vector {
		prefix := append([]byte{0xfd, 12}, make([]byte, 16)...)
		save = append(append(append([]byte{}, prefix...), 0x1a), save...)
		read = append(append(prefix, 0x1a), read...)
	}
	globalImport := append(append(wasmtest.Name("env"), wasmtest.Name("value")...), 3, 0x6f, 1)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(globalImport)), wasmtest.Section(3, []byte{2, 1, 2}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("save", 0, 0), wasmtest.ExportEntry("read", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(save), wasmtest.Code(read))))
}

func TestRV64CompactExternHostGlobalPrivate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c, err := NewRuntimeConfig().Compile(rv64CompactExternHostGlobalSource(vector))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()

		for artifact, candidate := range []*Compiled{c} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact != 0, profile), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					global, err := rt.NewExternRefGlobal(ExternRef{}, true)
					if err != nil {
						t.Fatal(err)
					}
					defer global.Close()
					mod, err := rt.Module(candidate)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					cfg := GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}
					first, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Global("env", "value", global)), WithGC(cfg))
					if err != nil {
						t.Fatal(err)
					}
					defer first.Close()
					if _, err = first.Invoke("save", 42); err != nil {
						t.Fatal(err)
					}
					if err := global.Close(); err == nil {
						t.Fatal("host global closed with live importer")
					}
					collector := first.gc
					if err := first.Close(); err != nil {
						t.Fatal(err)
					}
					if first.hasPhysicalResources() {
						t.Fatal("host global pinned the closed first importer")
					}
					second, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Global("env", "value", global)), WithGC(cfg))
					if err != nil {
						t.Fatal("second importer", err)
					}
					defer second.Close()
					if second.gc != collector {
						t.Fatal("host global did not preserve collector domain")
					}
					if err := second.CollectGC(); err != nil {
						t.Fatal(err)
					}
					got, err := second.Invoke("read")
					if err != nil || len(got) != 1 || got[0] != 42 {
						t.Fatal("retained host global", got, err)
					}
					public, err := global.GetValue()
					if err != nil || public.bits == 0 {
						t.Fatal("public converted global getter", public, err)
					}

					foreign, err := rt.NewExternRef("host global ordinary value")
					if err != nil {
						t.Fatal(err)
					}
					if err := global.SetValue(ValueExternRef(foreign)); err != nil {
						t.Fatal(err)
					}
					allocations := testing.AllocsPerRun(1000, func() {
						if err := global.SetValue(ValueExternRef(foreign)); err != nil {
							panic(err)
						}
					})
					if allocations != 0 {
						t.Fatal("warm host global Set allocations", allocations)
					}
					if rt.ReleaseExternRef(foreign) {
						t.Fatal("host global lost ordinary extern anchor")
					}
					if err := global.SetValue(ValueExternRef(ExternRef{0xdeadbeef})); err == nil {
						t.Fatal("forged token changed host global")
					}
					current, err := global.GetValue()
					if err != nil || current.bits != foreign.token {
						t.Fatal("failed setter changed value", current, err)
					}
					if err := global.SetValue(ValueExternRef(ExternRef{})); err != nil {
						t.Fatal(err)
					}
					if err := second.CollectGC(); err != nil {
						t.Fatal(err)
					}
					if !rt.ReleaseExternRef(foreign) {
						t.Fatal("cleared host global retained ordinary token")
					}
					if err := global.SetValue(ValueExternRef(ExternRef{public.bits})); err != nil {
						t.Fatal(err)
					}
					getterAllocations := testing.AllocsPerRun(100, func() {
						got, err := global.GetValue()
						if err != nil {
							panic(err)
						}
						if !rt.ReleaseExternRef(ExternRef{got.bits}) {
							panic("release host getter")
						}
					})
					sidecar, _ := compactExternCollectors.Load(collector)
					if sidecar.hostSlots == nil || len(sidecar.hostSlots.publicFree) != 1 {
						t.Fatal("public host root slots did not reuse indexes")
					}
					t.Logf("warm Set=%g Go allocations; Get+Release=%g Go allocations per owned public result; reused public free roots=%d", allocations, getterAllocations, len(sidecar.hostSlots.publicFree))
					if err := second.Close(); err != nil {
						t.Fatal(err)
					}
					if err := global.Close(); err != nil {
						t.Fatal(err)
					}
					if err := global.Close(); err != nil {
						t.Fatal(err)
					}
					domain := rt.refStore.gcDomainForCollector(collector)
					if domain == nil {
						t.Fatal("host global result lost its independent domain")
					}
					value, valid := rt.refStore.resolveExternref(public.bits)
					if !valid {
						t.Fatal("host result identity vanished")
					}
					word, ok := value.(uint64)
					if !ok {
						t.Fatal("unexpected host result value", value)
					}
					domain.mu.Lock()
					actual, shell := compactExternWordRoot(rt.refStore, word)
					state, exists := compactExternCollectors.Load(collector)
					if actual != collector || !exists {
						domain.mu.Unlock()
						t.Fatal("host result lost collector identity")
					}
					if err := collector.CollectFull(gc.EmptyRoots{}); err != nil {
						domain.mu.Unlock()
						t.Fatal(err)
					}
					object := state.shells[shell].value
					field, err := collector.StructGet(object, 0)
					domain.mu.Unlock()
					if err != nil || field.I32() != 42 {
						t.Fatal("host result lost value after physical close", field, err)
					}
					if !rt.ReleaseExternRef(ExternRef{public.bits}) {
						t.Fatal("release final host global result")
					}
					if rt.refStore.gcDomainForCollector(collector) != nil {
						t.Fatal("released host result retained domain")
					}
				})
			}
		}
	}
}

func rv64CompactExternHostGlobalCycleSource(vector bool) []byte {
	return rv64CompactExternHostGlobalCycleInput(vector, false)
}

func rv64CompactExternHostGlobalCycleInput(vector, callback bool) []byte {
	target := []byte{0xfb, 1, 1, 0x1a, 0x23, 0, 0xfb, 26, 0xfb, 22, 1, 0xfb, 2, 1, 1, 0x0b}
	globalImport := append(append(wasmtest.Name("env"), wasmtest.Name("value")...), 3, 0x6f, 1)
	imports := [][]byte{globalImport}
	base := byte(0)
	if callback {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("callback")...), 0, 2))
		target = append([]byte{0x10, 0}, target...)
		base = 1
	}
	save := []byte{0xd2, base, 0x41, 42, 0xfb, 0, 1, 0xfb, 27, 0x24, 0, 0x0b}
	if vector {
		prefix := append([]byte{0xfd, 12}, make([]byte, 16)...)
		target = append(append(prefix, 0x1a), target...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), []byte{0x5f, 2, 0x63, 0, 0, 0x7f, 0}, wasmtest.FuncType(nil, nil), []byte{0x60, 0, 1, 0x63, 0})),
		wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{3, 0, 2, 3}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("target", 0, uint32(base)), wasmtest.ExportEntry("save", 0, uint32(base+1)), wasmtest.ExportEntry("function", 0, uint32(base+2)))),
		wasmtest.Section(9, []byte{1, 3, 0, 1, base}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(target), wasmtest.Code(save), wasmtest.Code([]byte{0xd2, base, 0x0b}))),
	)
}

func TestRV64CompactExternHostGlobalCyclePrivate(t *testing.T) {
	callerCode := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), []byte{0x60, 1, 0x63, 0, 1, 0x7f})), wasmtest.Section(3, []byte{1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x14, 0, 0x0b}))))
	caller, err := NewRuntimeConfig().Compile(callerCode)
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	for _, vector := range []bool{false, true} {
		source, err := NewRuntimeConfig().Compile(rv64CompactExternHostGlobalCycleSource(vector))
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()

		for artifact, candidate := range []*Compiled{source} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				for _, held := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d/held%t", vector, artifact != 0, profile, held), func(t *testing.T) {
						rt := NewRuntime()
						defer rt.Close()
						global, err := rt.NewExternRefGlobal(ExternRef{}, true)
						if err != nil {
							t.Fatal(err)
						}
						defer global.Close()
						mod, err := rt.Module(candidate)
						if err != nil {
							t.Fatal(err)
						}
						defer mod.Close()
						producer, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Global("env", "value", global)), WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}))
						if err != nil {
							t.Fatal(err)
						}
						defer producer.Close()
						if _, err := producer.Invoke("save"); err != nil {
							t.Fatal(err)
						}
						if err := global.Close(); err == nil {
							t.Fatal("closed global with live importer")
						}
						var token uint64
						if held {
							result, err := producer.Invoke("function")
							if err != nil {
								t.Fatal(err)
							}
							token = result[0]
						}
						if err := producer.Close(); err != nil {
							t.Fatal(err)
						}
						if err := global.Close(); err != nil {
							t.Fatal("closed global function cycle", err)
						}
						if held {
							row := compactExternHostGlobalRow(global.owner)
							if row == nil || !row.logicalClosed.Load() || global.owner.closed {
								t.Fatal("held function lost global storage")
							}
							if _, err := global.GetValue(); err == nil {
								t.Fatal("public getter accepted logically closed global")
							}
							if _, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Global("env", "value", global))); err == nil {
								t.Fatal("new importer attached after logical close")
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
									t.Fatal("retained closed global read", got, err)
								}
							}
							releaseCompactExternTestFunction(rt.refStore, token)
							drainManagedFunctionStoreReclaims(rt.refStore)
						}
						if err := global.Close(); err != nil {
							t.Fatal(err)
						}
						if producer.hasPhysicalResources() || compactExternHostGlobalRow(global.owner) != nil {
							t.Fatal("closed global retained cycle")
						}
					})
				}
			}
		}
	}
}

func TestRV64CompactExternHostGlobalActiveClosePrivate(t *testing.T) {
	callerCode := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), []byte{0x60, 1, 0x63, 0, 1, 0x7f})), wasmtest.Section(3, []byte{1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x14, 0, 0x0b}))))
	caller, err := NewRuntimeConfig().Compile(callerCode)
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	for _, vector := range []bool{false, true} {
		source, err := NewRuntimeConfig().Compile(rv64CompactExternHostGlobalCycleInput(vector, true))
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()

		for artifact, candidate := range []*Compiled{source} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact != 0, profile), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					global, err := rt.NewExternRefGlobal(ExternRef{}, true)
					if err != nil {
						t.Fatal(err)
					}
					defer global.Close()
					mod, err := rt.Module(candidate)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					var producer *Instance
					calls := 0
					imports := NewImports().Global("env", "value", global).Function("env", "callback", slotHostFunc(func(host HostModule, _ []uint64, _ []uint64) {
						calls++
						if err := producer.Close(); err != nil {
							panic(err)
						}
						if err := global.Close(); err == nil {
							panic("host global closed during active invocation")
						}
						if err := host.(GCHostModule).CollectGC(); err != nil {
							panic(err)
						}
					}))
					producer, err = rt.Instantiate(context.Background(), mod, WithImports(imports), WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}))
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					if _, err := producer.Invoke("save"); err != nil {
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
					got, err := consumer.Invoke("call", token)
					if err != nil || len(got) != 1 || got[0] != 42 || calls != 1 {
						t.Fatal("active close changed native global", got, err, calls)
					}
					if err := global.Close(); err != nil {
						t.Fatal(err)
					}
					releaseCompactExternTestFunction(rt.refStore, token)
					drainManagedFunctionStoreReclaims(rt.refStore)
					if producer.hasPhysicalResources() || compactExternHostGlobalRow(global.owner) != nil {
						t.Fatal("quiescent global cycle was retained")
					}
				})
			}
		}
	}
}

func TestRV64CompactExternHostGlobalSequentialCyclesPrivate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		source, err := NewRuntimeConfig().Compile(rv64CompactExternHostGlobalCycleSource(vector))
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
					var globals [2]*Global
					var owners [2]*Instance
					var collector *gc.Collector
					for i := range globals {
						globals[i], err = rt.NewExternRefGlobal(ExternRef{}, true)
						if err != nil {
							t.Fatal(err)
						}
						defer globals[i].Close()
						if collector != nil {
							if err := railshotAttachCompactExternHostGlobal(globals[i], rt.refStore, collector); err != nil {
								t.Fatal(err)
							}
						}
						owners[i], err = rt.Instantiate(context.Background(), mod, WithImports(NewImports().Global("env", "value", globals[i])), WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}))
						if err != nil {
							t.Fatal(err)
						}
						defer owners[i].Close()
						collector = owners[i].gc
						if _, err := owners[i].Invoke("save"); err != nil {
							t.Fatal(err)
						}
						if err := owners[i].Close(); err != nil {
							t.Fatal(err)
						}
					}
					for i, global := range globals {
						if err := global.Close(); err != nil {
							t.Fatal(err)
						}
						if err := global.Close(); err != nil {
							t.Fatal(err)
						}
						if owners[i].hasPhysicalResources() || compactExternHostGlobalRow(global.owner) != nil {
							t.Fatal("closed cycle retained", i)
						}
						if i == 0 && (!owners[1].hasPhysicalResources() || compactExternHostGlobalRow(globals[1].owner) == nil) {
							t.Fatal("first close reclaimed second global")
						}
					}
					if rt.refStore.gcDomainForCollector(collector) != nil {
						t.Fatal("last global cycle retained collector claim")
					}
				})
			}
		}
	}
}

func BenchmarkRV64CompactExternHostGlobalOwnedGet(b *testing.B) {
	c, err := NewRuntimeConfig().Compile(rv64CompactExternHostGlobalSource(false))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	rt := NewRuntime()
	defer rt.Close()
	global, err := rt.NewExternRefGlobal(ExternRef{}, true)
	if err != nil {
		b.Fatal(err)
	}
	defer global.Close()
	mod, err := rt.Module(c)
	if err != nil {
		b.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Global("env", "value", global)))
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("save", 42); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		value, err := global.GetValue()
		if err != nil {
			b.Fatal(err)
		}
		if !rt.ReleaseExternRef(ExternRef{value.bits}) {
			b.Fatal("release owned host result")
		}
	}
}

func TestRV64CompactExternHostGlobalRuntimeClosePrivate(t *testing.T) {
	for _, artifact := range []bool{false} {
		for _, otherOpen := range []bool{false, true} {
			for _, pinned := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t/otherOpen%t/pinned%t", artifact, otherOpen, pinned), func(t *testing.T) {
					source, err := NewRuntimeConfig().Compile(rv64CompactExternHostGlobalCycleSource(false))
					if err != nil {
						t.Fatal(err)
					}
					defer source.Close()

					rt := NewRuntime()
					defer rt.Close()
					global, err := rt.NewExternRefGlobal(ExternRef{}, true)
					if err != nil {
						t.Fatal(err)
					}
					defer global.Close()
					mod, err := rt.Module(source)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Global("env", "value", global)))
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if _, err := in.Invoke("save"); err != nil {
						t.Fatal(err)
					}
					public, err := global.GetValue()
					if err != nil {
						t.Fatal(err)
					}
					defer rt.refStore.releaseExternref(public.bits)
					var other *Global
					if otherOpen {
						other, err = rt.NewExternRefGlobal(ExternRef{}, true)
						if err != nil {
							t.Fatal(err)
						}
						defer other.Close()
					}
					collector := in.gc
					if pinned && !rt.refStore.pinExternref(public.bits) {
						t.Fatal("public token pin failed")
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					if err := global.Close(); err != nil {
						t.Fatal(err)
					}
					if err := rt.CloseContext(context.Background()); err != nil {
						t.Fatal(err)
					}
					if otherOpen {
						if !in.hasPhysicalResources() {
							t.Fatal("open host resource permitted early public root drain")
						}
						if _, valid := rt.refStore.resolveExternref(public.bits); !valid {
							t.Fatal("open host resource lost public token")
						}
						if err := other.Close(); err != nil {
							t.Fatal(err)
						}
					}
					if pinned {
						if !in.hasPhysicalResources() {
							t.Fatal("Runtime.Close released a pinned public root")
						}
						if _, valid := rt.refStore.resolveExternref(public.bits); !valid {
							t.Fatal("Runtime.Close invalidated a native pin")
						}
						if !rt.refStore.unpinExternref(public.bits) {
							t.Fatal("public token unpin failed")
						}
					}
					drainManagedFunctionStoreReclaims(rt.refStore)
					if in.hasPhysicalResources() || compactExternHostGlobalRow(global.owner) != nil || rt.refStore.gcDomainForCollector(collector) != nil {
						t.Fatal("Runtime.Close retained logically closed host global cycle")
					}
					if _, valid := rt.refStore.resolveExternref(public.bits); valid {
						t.Fatal("Runtime.Close preserved public token after all host resources logically closed")
					}
				})
			}
		}
	}
}

func TestRV64CompactExternHostGlobalForeignRuntimeClosePrivate(t *testing.T) {
	callerSource := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))))
	caller, err := NewRuntimeConfig().Compile(callerSource)
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	for _, vector := range []bool{false, true} {
		target := []byte{0x10, 0, 0xfb, 1, 1, 0x1a, 0x23, 0, 0xfb, 26, 0xfb, 22, 1, 0xfb, 2, 1, 0, 0x0b}
		if vector {
			target = append(append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x1a), target...)
		}
		globalImport := append(append(wasmtest.Name("env"), wasmtest.Name("value")...), 3, 0x6f, 1)
		callbackImport := append(append(wasmtest.Name("env"), wasmtest.Name("callback")...), 0, 2)
		producerSource := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), []byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, nil))),
			wasmtest.Section(2, wasmtest.Vec(globalImport, callbackImport)),
			wasmtest.Section(3, []byte{2, 0, 2}),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("target", 0, 1), wasmtest.ExportEntry("save", 0, 2))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(target), wasmtest.Code([]byte{0x41, 42, 0xfb, 0, 1, 0xfb, 27, 0x24, 0, 0x0b}))))
		c, err := NewRuntimeConfig().Compile(producerSource)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()

		for artifact, candidate := range []*Compiled{c} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact != 0, profile), func(t *testing.T) {
					rt, other := NewRuntime(), NewRuntime()
					defer rt.Close()
					defer other.Close()
					global, err := rt.NewExternRefGlobal(ExternRef{}, true)
					if err != nil {
						t.Fatal(err)
					}
					defer global.Close()
					mod, err := rt.Module(candidate)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					calls := 0
					imports := NewImports().Global("env", "value", global).Function("env", "callback", slotHostFunc(func(host HostModule, _, _ []uint64) {
						calls++
						if calls == 1 {
							if err := global.Close(); err == nil {
								panic("foreign active invocation allowed host global close")
							}
						}
						if err := host.(GCHostModule).CollectGC(); err != nil {
							panic(err)
						}
					}))
					producer, err := rt.Instantiate(context.Background(), mod, WithImports(imports), WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}))
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					if _, err := producer.Invoke("save"); err != nil {
						t.Fatal(err)
					}
					target, err := producer.ExportedFunc("target")
					if err != nil {
						t.Fatal(err)
					}
					cm, err := other.Module(caller)
					if err != nil {
						t.Fatal(err)
					}
					defer cm.Close()
					consumer, err := other.Instantiate(context.Background(), cm, WithImports(NewImports().Function("env", "target", target)))
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					if got, err := consumer.Invoke("call"); err != nil || len(got) != 1 || got[0] != 42 {
						t.Fatal("foreign active owner", got, err)
					}
					collector := producer.gc
					if err := global.Close(); err != nil {
						t.Fatal(err)
					}
					if err := rt.CloseContext(context.Background()); err != nil {
						t.Fatal(err)
					}
					if got, err := consumer.Invoke("call"); err != nil || len(got) != 1 || got[0] != 42 || calls != 2 {
						t.Fatal("foreign owner after Runtime close", got, err, calls)
					}
					if err := consumer.Close(); err != nil {
						t.Fatal(err)
					}
					if err := other.CloseContext(context.Background()); err != nil {
						t.Fatal(err)
					}
					drainManagedFunctionStoreReclaims(rt.refStore)
					if producer.hasPhysicalResources() || compactExternHostGlobalRow(global.owner) != nil || rt.refStore.gcDomainForCollector(collector) != nil {
						t.Fatal("last foreign consumer retained closed host global")
					}
				})
			}
		}
	}
}
