//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceNumericCallsFixture(tableStage int, importedInitializer, vector, tails, wide bool, start, target int) []byte {
	if tableStage == 6 {
		if importedInitializer {
			return wasmtest.ClosedReferenceImmutableCellsImportedInitializer(vector, wide, start)
		}
		return wasmtest.ClosedReferenceImmutableCells(vector, wide, start, target)
	}
	if tableStage == 5 {
		if importedInitializer {
			return wasmtest.ClosedReferenceExpressionElementsImportedInitializer(vector, wide, start)
		}
		return wasmtest.ClosedReferenceExpressionElements(vector, wide, start, target)
	}
	if tableStage == 4 {
		if importedInitializer {
			return wasmtest.ClosedReferenceProperIndirectTailsImportedInitializer(vector, wide, start)
		}
		return wasmtest.ClosedReferenceProperIndirectTails(vector, wide, start, target)
	}
	if tableStage == 3 {
		if importedInitializer {
			return wasmtest.ClosedReferenceProperTailsImportedInitializer(vector, wide, start)
		}
		return wasmtest.ClosedReferenceProperTails(vector, wide, start, target)
	}
	if tableStage == 2 {
		if importedInitializer {
			return wasmtest.ClosedReferenceTableBulkImportedInitializer(vector, tails, wide, start)
		}
		return wasmtest.ClosedReferenceTableBulk(vector, tails, wide, start, target)
	}
	if tableStage == 1 {
		if importedInitializer {
			return wasmtest.ClosedReferenceOwnedTableImportedInitializer(vector, tails, wide, start)
		}
		return wasmtest.ClosedReferenceOwnedTable(vector, tails, wide, start, target)
	}
	if importedInitializer {
		return wasmtest.ClosedReferenceNumericCallsImportedInitializer(vector, tails, wide, start)
	}
	return wasmtest.ClosedReferenceNumericCalls(vector, tails, wide, start, target)
}

func rv64ReferenceNumericCallsRows(in *Instance) bool {
	if len(in.funcRefDescs) != 14*coreruntime.FuncRefDescBytes {
		return false
	}
	for _, index := range []int{4, 8, 9, 12} {
		if !rv64ReferenceTargetRowValid(in, index) {
			return false
		}
	}
	return true
}

func rv64ReferenceNumericCallsCheck(t *testing.T, in *Instance, vector bool, wantIndex int, targets ...int) {
	t.Helper()
	target := 0
	if len(targets) != 0 {
		target = targets[0]
	}
	indexWant, resultWant := 0, uint64(0xfffffffd)
	if target == 1 {
		indexWant, resultWant = 11, 0xffffffff
	} else if target == 2 {
		indexWant, resultWant = 2, 0xfffffffe
	}
	value := readGlobalObject(in.globalCells[14], ValFuncRef)
	rv64ReferenceImmutableCellCheck(t, in, value)
	if index, ok := in.funcrefDescriptorIndex(value); !ok || index != indexWant || value>>32 == 0 || readGlobalObject(in.globalCells[11], ValI64) != resultWant {
		t.Fatal("indexed numeric call_ref target identity/result", value, index, ok, readGlobalObject(in.globalCells[11], ValI64), resultWant)
	}
	if !rv64ReferenceNumericCallsRows(in) {
		t.Fatal("private local-cell descriptor holes")
	}
	for _, index := range []int{2, 3} {
		if readGlobalObject(in.globalCells[index], ValFuncRef) != 0 {
			t.Fatal("callback did not clear current reference cell", index)
		}
	}
	if in.c.tableCount() != 0 {
		table := privateTableBytes(in)
		size, cleared := uint32(3), 2
		if len(in.c.Globals) >= 16 {
			size, cleared = 4, 4
			wantGrow := uint64(3)
			if readGlobalObject(in.globalCells[6], ValI32) > 1 {
				wantGrow = 0xffffffff
			}
			if got := readGlobalObject(in.globalCells[15], ValI64); got != wantGrow {
				t.Fatal("unsigned grow publication/failure", got, wantGrow)
			}
		}
		if binary.LittleEndian.Uint32(table) != size || !bytes.Equal(table[8:8+cleared*coreruntime.TableEntryBytes], make([]byte, cleared*coreruntime.TableEntryBytes)) {
			t.Fatal("private table clear/size while saved carriers survived")
		}
		for _, name := range []string{"", "0", "table", "escape"} {
			if _, err := in.ExportedTable(name); err == nil {
				t.Fatal("private table exposed to host", name)
			}
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

func TestRV64PublicReferenceFunctionNumericCallsSourceArtifact(t *testing.T) {
	rv64ReferenceFunctionNumericCallsSourceArtifact(t, 0)
}
func TestRV64PublicReferenceFunctionOwnedTableSourceArtifact(t *testing.T) {
	rv64ReferenceFunctionNumericCallsSourceArtifact(t, 1)
}
func TestRV64PublicReferenceFunctionTableBulkSourceArtifact(t *testing.T) {
	rv64ReferenceFunctionNumericCallsSourceArtifact(t, 2)
}
func TestRV64PublicReferenceFunctionProperReferenceTailSourceArtifact(t *testing.T) {
	rv64ReferenceFunctionNumericCallsSourceArtifact(t, 3)
}
func TestRV64PublicReferenceFunctionProperIndirectTailSourceArtifact(t *testing.T) {
	rv64ReferenceFunctionNumericCallsSourceArtifact(t, 4)
}
func rv64ReferenceFunctionNumericCallsSourceArtifact(t *testing.T, tableStage int) {
	ownedTable := tableStage > 0
	t.Helper()
	product := "numeric-calls"
	if ownedTable {
		product = "owned-table"
	}
	if tableStage == 2 {
		product = "table-bulk"
	}
	if tableStage == 3 {
		product = "proper-reference-tail"
	}
	if tableStage == 4 {
		product = "proper-indirect-tail"
	}
	if tableStage == 5 {
		product = "expression-elements"
	}
	if tableStage == 6 {
		product = "immutable-cells"
	}
	for _, target := range []int{0, 1, 2} {
		for _, artifact := range []bool{false, true} {
			for _, vector := range []bool{false, true} {
				for _, tails := range []bool{false, true} {
					if tableStage >= 3 && !tails {
						continue
					}
					for _, wide := range []bool{false, true} {
						for _, start := range []int{0, 1, 2} {
							for _, owned := range []bool{false, true} {
								t.Run(fmt.Sprintf("artifact%t-vector%t-tails%t-wide%t-start%d-owned%t-target%d", artifact, vector, tails, wide, start, owned, target), func(t *testing.T) {
									rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
									defer rv64HostCertificateCloseRuntime(t, rt)
									provider, grower, _, im, _ := rv64ReferenceStartProvider(t, rt, artifact, vector, wide, false)
									pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-%s-v%t-t%t-w%t-s%d-target%d", product, vector, tails, wide, start, target), rv64ReferenceNumericCallsFixture(tableStage, false, vector, tails, wide, start, target), artifact)
									if !pc.nativeReferenceCallABI || !pc.nativeReferenceFunctionABI {
										t.Fatal("missing private numeric call witnesses")
									}
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
										if call.ParamCount() != 0 || call.ResultCount() != 0 || !rv64ReferenceNumericCallsRows(in) || caller.Memory()[48] != 7 {
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
									finish := rv64DynamicHostBinding(t, rt, owned, func(v int32) int32 { return v + 1 }, pc.importFuncSigs[2])
									im.Function("env.calls", "unused.fn", nf).Function("env.calls", "start.fn", binding).Function("env.calls", "finish.fn", finish)
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
									rv64ReferenceNumericCallsCheck(t, in, vector, 3, target)
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
										rv64ReferenceNumericCallsCheck(t, in, vector, wantIndex, target)
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
										if err := finish.(*HostFuncRef).Close(); err != nil {
											t.Fatal(err)
										}
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
}
