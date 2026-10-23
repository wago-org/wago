//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcScalarAdapterCallerModule(call string, ownObject, unusedCollectorSignature bool) []byte {
	types := [][]byte{}
	functionType := byte(0)
	if ownObject {
		types = append(types, []byte{0x5f, 1, 0x7f, 1})
		functionType++
	}
	types = append(types, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))
	if unusedCollectorSignature {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.AnyRef}))
	}
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("call")...)
	importEntry = append(importEntry, 0, functionType)
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec([]byte{functionType})),
	}
	if call != "direct" {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 1})))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))))
	if call == "indirect" || call == "ref" {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 1, 0})))
	}
	body := []byte{0}
	if ownObject {
		body = []byte{1, 1, 0x63, 0, 0x41, 0xc9, 0, 0xfb, 0, 0, 0x21, 0}
	}
	switch call {
	case "direct", "unused-table":
		body = append(body, 0x10, 0)
	case "indirect":
		body = append(body, 0x41, 0, 0x11, functionType, 0)
	case "ref":
		body = append(body, 0xd2, 0, 0x14, functionType)
	default:
		panic("unknown scalar adapter call")
	}
	if ownObject {
		body = append(body, 0x1a, 0x20, 0, 0xfb, 2, 0, 0)
	}
	body = append(body, 0x0b)
	body = append(wasmtest.ULEB(uint32(len(body))), body...)
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(body)))
	return wasmtest.Module(sections...)
}

// An unrelated table or declared collector signature cannot change the scalar
// call's ownership contract. The producer's host call keeps its adapter/root
// callsite live; its earlier allocation therefore walks that adapter too.
func TestGCScalarAdapterCallerOwnership(t *testing.T) {
	for _, saved := range []bool{false, true} {
		t.Run(fmt.Sprintf("saved=%t", saved), func(t *testing.T) {
			for _, registerABI := range []bool{false, true} {
				for _, call := range []string{"direct", "unused-table", "indirect", "ref"} {
					for _, ownObject := range []bool{false, true} {
						for _, unusedSignature := range []bool{false, true} {
							t.Run(fmt.Sprintf("register=%t/%s/object=%t/unusedSignature=%t", registerABI, call, ownObject, unusedSignature), func(t *testing.T) {
								cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("reg-abi", registerABI)
								rt := NewRuntime(WithRuntimeConfig(cfg))
								defer rt.Close()
								producerCode, err := rt.Compile(gcAllocatingHostScalarProducerModule())
								if err != nil {
									t.Fatal(err)
								}
								producerCode = gcForeignFrameArtifact(t, rt, producerCode, saved)
								defer producerCode.Close()
								settings := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
								producer, err := rt.Instantiate(context.Background(), producerCode, WithGC(settings), WithImports(testImports("env.host", slotHostFunc(func(_ HostModule, _ []uint64, results []uint64) { results[0] = 7 }))))
								if err != nil {
									t.Fatal(err)
								}
								defer producer.Close()
								target, err := producer.ExportedFunc("call")
								if err != nil {
									t.Fatal(err)
								}
								callerCode, err := rt.Compile(gcScalarAdapterCallerModule(call, ownObject, unusedSignature))
								if err != nil {
									t.Fatal(err)
								}
								callerCode = gcForeignFrameArtifact(t, rt, callerCode, saved)
								defer callerCode.Close()
								caller, err := rt.Instantiate(context.Background(), callerCode, WithGC(settings), WithImports(testImports("env.call", target)))
								if err != nil {
									t.Fatal(err)
								}
								defer caller.Close()
								if caller.gc == producer.gc || (caller.gc != nil) != ownObject {
									t.Fatal("fixture must have separate collector domains and the requested caller roots")
								}
								want := uint64(7)
								if ownObject {
									want = 73
								}
								beforeCollections := producer.gc.Stats().FullCollections
								for i := 0; i < 3; i++ {
									got, err := caller.Invoke("run")
									if err != nil || len(got) != 1 || got[0] != want {
										t.Fatalf("scalar boundary = %v, %v; want [%d], nil", got, err, want)
									}
								}
								if producer.gc.Stats().FullCollections <= beforeCollections {
									t.Fatal("producer did not perform the forced full collection")
								}
							})
						}
					}
				}
			}
		})
	}
}

func TestGCScalarAdapterAllocOnlyCallee(t *testing.T) {
	for _, saved := range []bool{false, true} {
		t.Run(fmt.Sprintf("saved=%t", saved), func(t *testing.T) {
			for _, registerABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("register=%t", registerABI), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("reg-abi", registerABI)
					rt := NewRuntime(WithRuntimeConfig(cfg))
					defer rt.Close()
					producerCode, err := rt.Compile(gcAllocatingScalarProducerModule())
					if err != nil {
						t.Fatal(err)
					}
					producerCode = gcForeignFrameArtifact(t, rt, producerCode, saved)
					defer producerCode.Close()
					settings := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
					producer, err := rt.Instantiate(context.Background(), producerCode, WithGC(settings))
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					if roots := producer.c.genericGCFrameRoots(); roots == nil || len(roots.callsites) != 0 {
						t.Fatal("fixture must allocate without any module-local callsites")
					}
					target, err := producer.ExportedFunc("allocate")
					if err != nil {
						t.Fatal(err)
					}
					callerCode, err := rt.Compile(gcScalarAdapterCallerModule("direct", true, false))
					if err != nil {
						t.Fatal(err)
					}
					callerCode = gcForeignFrameArtifact(t, rt, callerCode, saved)
					defer callerCode.Close()
					caller, err := rt.Instantiate(context.Background(), callerCode, WithGC(settings), WithImports(testImports("env.call", target)))
					if err != nil {
						t.Fatal(err)
					}
					defer caller.Close()
					if caller.gc == nil || caller.gc != producer.gc {
						t.Fatal("fixture must retain caller objects in the collecting domain")
					}
					for i := 0; i < 3; i++ {
						got, err := caller.Invoke("run")
						if err != nil || len(got) != 1 || got[0] != 73 {
							t.Fatalf("caller root across alloc-only callee = %v, %v; want [73], nil", got, err)
						}
					}
				})
			}
		})
	}
}

// Scalar edges can revisit the collecting domain through a mutable funcref
// table. The outer frame still owns a root even though the intervening relay
// transfers no objects and has an incompatible private collector.
func TestGCScalarAdapterRevisitedDomain(t *testing.T) {
	for _, tiny := range []bool{false, true} {
		t.Run(fmt.Sprintf("tiny=%t", tiny), func(t *testing.T) {
			for _, relayObject := range []bool{false, true} {
				t.Run(fmt.Sprintf("relayObject=%t", relayObject), func(t *testing.T) {
					for _, saved := range []bool{false, true} {
						t.Run(fmt.Sprintf("saved=%t", saved), func(t *testing.T) {
							for _, registerABI := range []bool{false, true} {
								t.Run(fmt.Sprintf("register=%t", registerABI), func(t *testing.T) {
									cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("reg-abi", registerABI)
									rt := NewRuntime(WithRuntimeConfig(cfg))
									defer rt.Close()
									settings := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
									if tiny {
										settings = GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 512, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}
									}
									table, err := NewTable(1, 1)
									if err != nil {
										t.Fatal(err)
									}
									defer table.Close()
									tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 1, 1, 1)
									wantRelay := byte(7)
									if relayObject {
										wantRelay = 47
									}
									outer := []byte{1, 1, 0x63, 0, 0x41, 0xc9, 0, 0xfb, 0, 0, 0x21, 0, 0x41, 0, 0x11, 1, 0, 0x41, wantRelay, 0x47, 0x04, 0x40, 0x00, 0x0b, 0x20, 0, 0xfb, 2, 0, 0, 0x0b}
									outer = append(wasmtest.ULEB(uint32(len(outer))), outer...)
									aCode, err := rt.Compile(wasmtest.Module(
										wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
										wasmtest.Section(2, wasmtest.Vec(tableImport)),
										wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1})),
										wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("outer", 0, 0), wasmtest.ExportEntry("inner", 0, 1))),
										wasmtest.Section(10, wasmtest.Vec(
											outer,
											wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x41, 7, 0x0b}),
										)),
									))
									if err != nil {
										t.Fatal(err)
									}
									aCode = gcForeignFrameArtifact(t, rt, aCode, saved)
									defer aCode.Close()
									a, err := rt.Instantiate(context.Background(), aCode, WithGC(settings), WithImports(testImports("env.table", table)))
									if err != nil {
										t.Fatal(err)
									}
									defer a.Close()
									inner, err := a.ExportedFunc("inner")
									if err != nil {
										t.Fatal(err)
									}
									relayTypes := [][]byte{wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})}
									relaySignature := byte(0)
									relayBody := []byte{0, 0x10, 0, 0x0b}
									var relayGlobals [][]byte
									if relayObject {
										relayTypes = append([][]byte{{0x5f, 1, 0x7e, 1}}, relayTypes...)
										relaySignature = 1
										// Keep handle one in a foreign global, and handle two in
										// the relay frame. Scanning this word in A would admit the
										// wrong handle rather than A.outer's handle-one root.
										relayGlobals = [][]byte{wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b}))}
										relayBody = []byte{1, 1, 0x63, 0, 0x42, 0, 0xfb, 0, 0, 0x24, 0, 0x42, 47, 0xfb, 0, 0, 0x21, 0, 0x10, 0, 0x1a, 0x20, 0, 0xfb, 2, 0, 0, 0xa7, 0x0b}
									}
									relayBody = append(wasmtest.ULEB(uint32(len(relayBody))), relayBody...)
									innerImport := append(append(wasmtest.Name("env"), wasmtest.Name("inner")...), 0, relaySignature)
									sections := [][]byte{
										wasmtest.Section(1, wasmtest.Vec(relayTypes...)),
										wasmtest.Section(2, wasmtest.Vec(innerImport, tableImport)),
										wasmtest.Section(3, wasmtest.Vec([]byte{relaySignature})),
									}
									sections = append(sections, relayGlobals...)
									sections = append(sections,
										wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 1, 1})),
										wasmtest.Section(10, wasmtest.Vec(relayBody)),
									)
									bCode, err := rt.Compile(wasmtest.Module(sections...))
									if err != nil {
										t.Fatal(err)
									}
									bCode = gcForeignFrameArtifact(t, rt, bCode, saved)
									defer bCode.Close()
									b, err := rt.Instantiate(context.Background(), bCode, WithGC(settings), WithImports(testImports("env.inner", inner, "env.table", table)))
									if err != nil {
										t.Fatal(err)
									}
									defer b.Close()
									if a.gc == nil || (b.gc != nil) != relayObject || a.gc == b.gc {
										t.Fatal("fixture requires different live collectors")
									}
									beforeCollections := a.gc.Stats().FullCollections
									for i := 0; i < 3; i++ {
										got, err := a.Invoke("outer")
										if err != nil || len(got) != 1 || got[0] != 73 {
											t.Fatalf("revisited collecting domain = %v, %v; want [73], nil", got, err)
										}
									}
									if a.gc.Stats().FullCollections <= beforeCollections {
										t.Fatal("revisited domain did not perform the forced full collection")
									}

								})
							}
						})
					}
				})
			}
		})
	}
}

func gcForeignFrameArtifact(t *testing.T, rt *Runtime, module *Module, saved bool) *Module {
	t.Helper()
	if !saved {
		return module
	}
	blob, err := module.Compiled().MarshalBinary()
	if err != nil {
		t.Fatal("save native frame maps", err)
	}
	if err := module.Close(); err != nil {
		t.Fatal(err)
	}
	loaded := new(Compiled)
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatal("reload native frame maps", err)
	}
	t.Cleanup(func() { _ = loaded.Close() })
	result, err := rt.Module(loaded)
	if err != nil {
		t.Fatal("publish saved frame maps", err)
	}
	return result
}

func TestGCCollectorFreeReturnMapsCannotPublishObjectRoots(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcScalarAdapterCallerModule("unused-table", false, true))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	plan := c.genericGCFrameRoots()
	if !c.collectorFreeNativeUnwind(plan) || c.GCNativeRootAdmission().Required || c.nativeGCABIRequirement() != 0 {
		t.Fatal("collector-free caller did not retain only its physical return maps")
	}
	if err := validateCompiledGCFrameRoots(c, plan); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"root", "safepoint"} {
		t.Run(kind, func(t *testing.T) {
			forged := *plan
			forged.callsites = append([]compiledGCFrameCallsite(nil), plan.callsites...)
			if kind == "root" {
				forged.callsites[0].offsets = []uint32{0}
			} else {
				forged.safepoints = []compiledGCFrameSafepoint{{id: 1, frameBytes: forged.callsites[0].frameBytes}}
			}
			if err := validateCompiledGCFrameRoots(c, &forged); err == nil {
				t.Fatalf("collector-free %s metadata granted object traversal", kind)
			} else {
				t.Logf("rejected invalid %s metadata: %v", kind, err)
			}
		})
	}
}

// An immutable scalar import path can leave and reenter a collector through
// another Runtime without a mutable cross-store funcref container.
func TestGCScalarAdapterRevisitedDomainAcrossStores(t *testing.T) {
	for _, depth := range []int{1, 40} {
		t.Run(fmt.Sprintf("depth=%d", depth), func(t *testing.T) {
			for _, registerABI := range []bool{false, true} {
				for _, saved := range []bool{false, true} {
					for _, retired := range []bool{false, true} {
						t.Run(fmt.Sprintf("register=%t/saved=%t/retired=%t", registerABI, saved, retired), func(t *testing.T) {
							cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("reg-abi", registerABI)
							aRuntime, bRuntime := NewRuntime(WithRuntimeConfig(cfg)), NewRuntime(WithRuntimeConfig(cfg))
							defer aRuntime.Close()
							defer bRuntime.Close()
							settings := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
							innerModule, err := aRuntime.Compile(gcAllocatingScalarProducerModule())
							if err != nil {
								t.Fatal(err)
							}
							innerModule = gcForeignFrameArtifact(t, aRuntime, innerModule, saved)
							defer innerModule.Close()
							inner, err := aRuntime.Instantiate(context.Background(), innerModule, WithGC(settings))
							if err != nil {
								t.Fatal(err)
							}
							defer inner.Close()
							target, err := inner.ExportedFunc("allocate")
							if err != nil {
								t.Fatal(err)
							}
							relaySource := wasmtest.Module(
								wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
								wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("env", "inner", 0))),
								wasmtest.Section(3, wasmtest.Vec([]byte{0})),
								wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("relay", 0, 1))),
								wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x41, 1, 0x6a, 0x0b}))),
							)
							relayModule, err := bRuntime.Compile(relaySource)
							if err != nil {
								t.Fatal(err)
							}
							relayModule = gcForeignFrameArtifact(t, bRuntime, relayModule, saved)
							defer relayModule.Close()
							var relays []*Instance
							for range depth {
								relay, err := bRuntime.Instantiate(context.Background(), relayModule, WithImports(testImports("env.inner", target)))
								if err != nil {
									t.Fatal(err)
								}
								defer relay.Close()
								relays = append(relays, relay)
								target, err = relay.ExportedFunc("relay")
								if err != nil {
									t.Fatal(err)
								}
							}
							relay := relays[len(relays)-1]
							outerModule, err := aRuntime.Compile(gcScalarAdapterCallerModule("direct", true, false))
							if err != nil {
								t.Fatal(err)
							}
							outerModule = gcForeignFrameArtifact(t, aRuntime, outerModule, saved)
							defer outerModule.Close()
							outer, err := aRuntime.Instantiate(context.Background(), outerModule, WithGC(settings), WithImports(testImports("env.call", target)))
							if err != nil {
								t.Fatal(err)
							}
							defer outer.Close()
							if outer.gc == nil || outer.gc != inner.gc || relay.gc != nil || outer.refStore == relay.refStore {
								t.Fatal("fixture did not return to the same collector through a foreign-store numeric relay")
							}
							if retired {
								for _, relay := range relays {
									if err := relay.Close(); err != nil {
										t.Fatal(err)
									}
								}
								if err := inner.Close(); err != nil {
									t.Fatal(err)
								}
							}
							before := outer.gc.Stats().FullCollections
							for range 3 {
								got, err := outer.Invoke("run")
								if err != nil || len(got) != 1 || got[0] != 73 {
									t.Fatalf("root across foreign-store relay = %v, %v; want [73], nil", got, err)
								}
							}
							if outer.gc.Stats().FullCollections <= before {
								t.Fatal("foreign-store relay did not perform forced collection")
							}
						})
					}
				}
			}
		})
	}
}

// A collector-free native relay can park the older collecting frame in Go.
// The parked chain still belongs to the collector of that older caller.
func TestGCScalarAdapterForeignHostCollection(t *testing.T) {
	for _, foreignStore := range []bool{false, true} {
		for _, registerABI := range []bool{false, true} {
			t.Run(fmt.Sprintf("foreignStore=%t/register=%t", foreignStore, registerABI), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("reg-abi", registerABI)
				rt := NewRuntime(WithRuntimeConfig(cfg))
				defer rt.Close()
				relayRuntime := rt
				if foreignStore {
					relayRuntime = NewRuntime(WithRuntimeConfig(cfg))
					defer relayRuntime.Close()
				}
				relayCode, err := relayRuntime.Compile(gcScalarAdapterCallerModule("direct", false, false))
				if err != nil {
					t.Fatal(err)
				}
				defer relayCode.Close()
				var outer *Instance
				relay, err := relayRuntime.Instantiate(context.Background(), relayCode, WithImports(testImports("env.call", slotHostFunc(func(_ HostModule, _ []uint64, results []uint64) {
					if err := outer.CollectGC(); err != nil {
						panic(err)
					}
					results[0] = 7
				}))))
				if err != nil {
					t.Fatal(err)
				}
				defer relay.Close()
				target, err := relay.ExportedFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				outerCode, err := rt.Compile(gcScalarAdapterCallerModule("direct", true, false))
				if err != nil {
					t.Fatal(err)
				}
				defer outerCode.Close()
				outer, err = rt.Instantiate(context.Background(), outerCode, WithGC(GCConfig{VerifyAfterCollect: true}), WithImports(testImports("env.call", target)))
				if err != nil {
					t.Fatal(err)
				}
				defer outer.Close()
				before := outer.gc.Stats().FullCollections
				got, err := outer.Invoke("run")
				if err != nil || len(got) != 1 || got[0] != 73 {
					t.Fatalf("root across numeric host relay = %v, %v; want [73], nil", got, err)
				}
				if outer.gc.Stats().FullCollections <= before {
					t.Fatal("host did not collect")
				}
			})
		}
	}
}
