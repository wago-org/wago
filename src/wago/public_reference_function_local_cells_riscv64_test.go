//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"slices"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceTargetRowValid(in *Instance, index int) bool {
	row := in.funcRefDescs[(index+1)*40 : (index+2)*40]
	if rv64PrimitiveReferenceContract(in.c) {
		if railshotNonCallableFunction(in.c, index) {
			return false
		}
		_, valid := rv64PublishedDescriptor(in, uint64(uintptr(unsafe.Pointer(&row[0]))))
		return valid
	}
	return railshotNonCallableFunction(in.c, index) && bytes.Equal(row, make([]byte, 40))
}

func rv64ReferenceLocalCellsRows(in *Instance) bool {
	if len(in.funcRefDescs) != 12*coreruntime.FuncRefDescBytes {
		return false
	}
	for _, index := range []int{4, 8, 9} {
		if !rv64ReferenceTargetRowValid(in, index) {
			return false
		}
	}
	return true
}

func rv64ReferenceLocalCellsCheck(t *testing.T, in *Instance, vector bool, wantIndex int) {
	t.Helper()
	if !rv64ReferenceLocalCellsRows(in) {
		t.Fatal("private local-cell descriptor holes")
	}
	for _, index := range []int{2, 3} {
		if readGlobalObject(in.globalCells[index], ValFuncRef) != 0 {
			t.Fatal("callback did not clear current reference cell", index)
		}
	}
	null := uint64(0)
	for _, cell := range []int{4, 5} {
		value := readGlobalObject(in.globalCells[cell], ValFuncRef)
		indexWant := wantIndex
		if cell == 4 {
			indexWant = 0
		}
		if indexWant < 0 {
			null = 1
			if value != 0 {
				t.Fatal("saved/lower null changed", cell, value)
			}
		} else if index, ok := in.funcrefDescriptorIndex(value); !ok || index != indexWant || value>>32 == 0 {
			t.Fatal("saved/lower full-width identity changed", cell, value, index, ok)
		}
	}
	want := []uint64{0x7ff0000000000042}
	if vector {
		want = append(want, 0x80000000deadbeef)
	}
	want = append(want, 0xfffffffe, null, 0, 1, 1)
	if got, err := in.Invoke("get"); err != nil || !slices.Equal(got, want) {
		t.Fatal("private local-cell continuation", got, want, err)
	}
}

func TestRV64PublicReferenceFunctionLocalCellsSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, tails := range []bool{false, true} {
				for _, wide := range []bool{false, true} {
					for _, start := range []int{0, 1, 2} {
						for _, owned := range []bool{false, true} {
							t.Run(fmt.Sprintf("artifact%t-vector%t-tails%t-wide%t-start%d-owned%t", artifact, vector, tails, wide, start, owned), func(t *testing.T) {
								rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
								defer rv64HostCertificateCloseRuntime(t, rt)
								provider, grower, _, im, _ := rv64ReferenceStartProvider(t, rt, artifact, vector, wide, false)
								pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-local-cells-v%t-t%t-w%t-s%d", vector, tails, wide, start), wasmtest.ClosedReferenceLocalCells(vector, tails, wide, start), artifact)
								if vector && len(pc.ValueTypes) != 6 {
									t.Fatal("six exact global carrier kinds", pc.ValueTypes)
								}
								nc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
								native := rv64NativeTokenInstantiate(t, rt, nc, NewImports())
								nf, err := native.ExportedFunc("identity")
								if err != nil {
									t.Fatal(err)
								}
								gate := newPluginCallGate("rv64-private-local-cells")
								rt.storeHooks(&hookRegistry{operationGates: []*pluginCallGate{gate}})
								calls := 0
								var saved Caller
								probe := CallerHostCallFunc(func(caller Caller, call HostCall) {
									calls++
									saved = caller
									in := caller.in
									if call.ParamCount() != 0 || call.ResultCount() != 0 || !rv64ReferenceLocalCellsRows(in) || caller.Memory()[48] != 7 {
										panic("private cell callback context")
									}
									if readGlobalObject(in.globalCells[6], ValI32) == 0 {
										if !in.c.StartIsImport || !in.constructionIsActive() || caller.reservation == nil {
											panic("imported local-cell start context")
										}
										if _, err := in.InvokeFromHost(context.Background(), caller, "initialize"); err != nil {
											panic(HostTrap{Err: err})
										}
										return
									}
									before := readGlobalObject(in.globalCells[2], ValFuncRef)
									if index, ok := in.funcrefDescriptorIndex(before); !ok || index != 0 || before>>32 == 0 {
										panic("lower cell is not the own imported numeric slot")
									}
									if _, err := in.InvokeFromHost(context.Background(), caller, "clear"); err != nil {
										panic(HostTrap{Err: err})
									}
									if readGlobalObject(in.globalCells[2], ValFuncRef) != 0 || readGlobalObject(in.globalCells[3], ValFuncRef) != 0 || !caller.valid() {
										panic("callback cell clear/scope restoration")
									}
									runtime.GC()
								})
								binding := rv64DynamicHostBinding(t, rt, owned, probe, pc.importFuncSigs[1])
								im.Function("env.calls", "unused.fn", nf).Function("env.calls", "start.fn", binding).Function("env.calls", "finish.fn", func(v int32) int32 { return v + 1 })
								in := rv64NativeTokenInstantiate(t, rt, pc, im)
								if start == 0 {
									if calls != 0 || readGlobalObject(in.globalCells[6], ValI32) != 0 {
										t.Fatal("local cells executed during construction")
									}
									seed := readGlobalObject(in.globalCells[3], ValFuncRef)
									if index, ok := in.funcrefDescriptorIndex(seed); !ok || index != 3 || seed>>32 == 0 {
										t.Fatal("initializer-only full descriptor", seed, index, ok)
									}
									if _, err := in.Invoke("run"); err != nil {
										t.Fatal(err)
									}
								}
								rv64ReferenceLocalCellsCheck(t, in, vector, 3)
								wantCalls := 1
								if start == 2 {
									wantCalls++
								}
								if calls != wantCalls || saved.valid() || gate.state.Load() != 0 || readGlobalObject(provider.globalCells[1], ValI32) != 12 {
									t.Fatal("local-cell start once/scope", calls, wantCalls)
								}
								if err := native.Close(); err != nil {
									t.Fatal(err)
								}
								if err := nc.Close(); err != nil {
									t.Fatal(err)
								}
								provider.Close()
								pc.Close()
								if native.resourcesClosed || in.resourcesClosed || provider.resourcesClosed {
									t.Fatal("local-cell imports lost physical owners")
								}
								for attempt, choice := range []uint64{0, 1, 2, 0} {
									if got, err := in.Invoke("select", 0xdeadbeef00000000|choice); err != nil || !slices.Equal(got, []uint64{0}) {
										t.Fatal("local-cell selector", got, err)
									}
									if _, err := in.Invoke("run"); err != nil {
										t.Fatal(err)
									}
									wantIndex := 0
									if choice == 1 {
										wantIndex = 3
									} else if choice == 2 {
										wantIndex = -1
									}
									rv64ReferenceLocalCellsCheck(t, in, vector, wantIndex)
									if readGlobalObject(in.globalCells[6], ValI32) != uint64(2+attempt) || readGlobalObject(provider.globalCells[1], ValI32) != uint64(13+attempt) {
										t.Fatal("cell store/reread repeated start")
									}
								}
								for _, name := range []string{"2", "3", "4", "5", "current", "snapshot"} {
									if _, err := in.ExportedGlobalObject(name); err == nil {
										t.Fatal("private reference cell exported as object", name)
									}
									if _, err := in.GlobalValue(name); err == nil {
										t.Fatal("private reference cell readable", name)
									}
									if err := in.SetGlobalValue(name, Value{typ: ValFuncRef}); err == nil {
										t.Fatal("private reference cell mutable by host", name)
									}
								}
								in.Close()
								rv64ReferenceImportedStartRetired(t, native, nc)
								if owned {
									if err := binding.(*HostFuncRef).Close(); err != nil {
										t.Fatal(err)
									}
								}
								grower.Close()
								if !provider.resourcesClosed {
									t.Fatal("local-cell provider retained after final borrower")
								}
							})
						}
					}
				}
			}
		}
	}
}

func rv64ReferenceLocalCellInitializerModule(vector, nonnull bool) []byte {
	params := []wasm.ValType{wasm.FuncRef}
	check := []byte{0x23, 0}
	if vector {
		params = append(params, wasm.V128)
		check = append(check, 0xfd, 12)
		check = append(check, make([]byte, 16)...)
	}
	check = append(check, 0x10, 0, 0x24, 0, 0x23, 0, 0xd1, 0x0b)
	init := []byte{0xd0, 0x70, 0x0b}
	if nonnull {
		init = []byte{0xd2, 1, 0x0b}
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.FuncRef}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, init))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("check", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code(check))))
}

func TestRV64PublicReferenceFunctionLocalCellsInitializerOnly(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, nonnull := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-nonnull%t", artifact, vector, nonnull), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rv64HostCertificateCloseRuntime(t, rt)
					c := rv64NativeTailCompile(t, fmt.Sprintf("reference-local-cell-init-v%t-n%t", vector, nonnull), rv64ReferenceLocalCellInitializerModule(vector, nonnull), artifact)
					if c.nativeReferenceCallABI || c.nativeReferenceControlABI || c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) {
						t.Fatal("plain local cell manufactured typed control/call permission")
					}
					in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
					want, rows := uint64(1), 0
					if nonnull {
						want, rows = 0, 3
					}
					if len(in.funcRefDescs) != rows*coreruntime.FuncRefDescBytes {
						t.Fatal("initializer-only descriptor extent", len(in.funcRefDescs), rows)
					}
					check := func() {
						got, err := in.Invoke("check")
						if err != nil || !slices.Equal(got, []uint64{want}) {
							panic("initializer-only private reference call")
						}
					}
					check()
					value := readGlobalObject(in.globalCells[0], ValFuncRef)
					if nonnull {
						if index, ok := in.funcrefDescriptorIndex(value); !ok || index != 1 || value>>32 == 0 {
							t.Fatal("initializer-only full reference", value, index, ok)
						}
					} else if value != 0 {
						t.Fatal("null-only cell changed")
					}
					c.Close()
					if allocs := testing.AllocsPerRun(10, check); allocs != 0 {
						t.Fatal("warm private local-cell call allocations", allocs)
					}
					in.Close()
					rv64ReferenceImportedStartRetired(t, in, c)
				})
			}
		}
	}
}
