//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64DeclarativeModule(t *testing.T, vector, expressions, references bool, drop int) []byte {
	source := rv64BodyReferenceModule(vector, false)
	elements := make([][]byte, 256)
	for i := range elements {
		if expressions {
			value := []byte{0xd0, 0x70, 0x0b}
			if references {
				value = []byte{0xd2, 0, 0x0b}
			}
			elements[i] = append([]byte{7, 0x70}, wasmtest.Vec(value, value, value, value)...)
		} else {
			elements[i] = []byte{3, 0, 4, 0, 0, 0, 0}
		}
	}
	exports := wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("carrier", 0, 1), wasmtest.ExportEntry("drop", 0, 2)))
	source = rv64ReplaceTableStageSection(t, source, 7, append(exports, wasmtest.Section(9, wasmtest.Vec(elements...))...))
	carrier := []byte{0x41, 7, 0x0b}
	if references {
		carrier = []byte{0xd2, 0, 0xd1, 0x0b}
	}
	body := []byte{}
	if drop >= 0 {
		for repeat := 0; repeat < 2; repeat++ {
			body = append(body, 0xfc, 13)
			body = append(body, wasmtest.ULEB(uint32(drop))...)
		}
	}
	body = append(body, 0x41, 7, 0x0b)
	return rv64ReplaceTableStageSection(t, source, 10, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code(carrier), wasmtest.Code(body))))
}

func TestRV64PublicDeclarativeImportedTargets(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, expressions := range []bool{false, true} {
			for _, kind := range []string{"callback", "host", "native"} {
				for _, artifact := range []bool{false, true} {
					t.Run(fmt.Sprintf("v%t-expr%t-%s-artifact%t", vector, expressions, kind, artifact), func(t *testing.T) {
						types := [][]byte{wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})}
						if vector {
							types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))
						}
						imports := wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)))
						source := rv64DeclarativeModule(t, false, expressions, true, 255)
						source = rv64ReplaceTableStageSection(t, source, 1, append(wasmtest.Section(1, wasmtest.Vec(types...)), imports...))
						source = rv64ReplaceTableStageSection(t, source, 3, wasmtest.Section(3, []byte{2, 1, 2}))
						source = rv64ReplaceTableStageSection(t, source, 10, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd2, 0, 0x42, 7, 0x10, 0, 0x1a, 0xd1, 0x0b}), wasmtest.Code([]byte{0xfc, 13, 0xff, 1, 0x41, 7, 0x0b}))))
						c := rv64TableStageCompile(t, fmt.Sprintf("decl-import-v%t-expr%t", vector, expressions), source, artifact)
						rt := NewRuntime()
						defer rt.Close()
						calls := 0
						var binding any = slotHostFunc(func(_ HostModule, p, r []uint64) { calls++; copy(r, p) })
						var host *HostFuncRef
						var producer *Instance
						var pc *Compiled
						if kind == "host" {
							host = rv64HostTokenOwner(t, rt, ValI64, slotHostFunc(func(_ HostModule, p, r []uint64) { calls++; copy(r, p) }))
							defer host.Close()
							binding = host
						} else if kind == "native" {
							pc = rv64TableStageCompile(t, "decl-native-bare", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I64}), artifact)
							producer = rv64NativeTokenInstantiate(t, rt, pc, NewImports())
							defer producer.Close()
							var err error
							binding, err = producer.ExportedFunc("identity")
							if err != nil {
								t.Fatal(err)
							}
						}
						in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Function("env", "target", binding))
						defer in.Close()
						if host != nil {
							if err := host.Close(); err == nil {
								t.Fatal("host closed with attached importer")
							}
						}
						if producer != nil {
							producer.Close()
							pc.Close()
						}
						for repeat := 0; repeat < 3; repeat++ {
							importedGlobalInvoke(t, in, "carrier", []uint64{0}, 0)
							importedGlobalInvoke(t, in, "drop", []uint64{7})
						}
						if kind != "native" && calls != 3 {
							t.Fatal("host call count", calls)
						}
						if in.tableDescPtr != 0 || len(rt.refStore.byToken) != 0 {
							t.Fatal("declaration created table/token")
						}
						in.Close()
						if host != nil {
							if err := host.Close(); err != nil {
								t.Fatal(err)
							}
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicDeclarativeFeatureGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source feature admission")
	}
	for _, vector := range []bool{false, true} {
		source := rv64DeclarativeModule(t, vector, false, false, -1)
		c, err := Compile(NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false), source)
		if err != nil {
			t.Fatal("inert legacy declarations invented reference feature", err)
		}
		if c.needsFuncRefDescs() {
			t.Fatal("inert legacy declaration descriptors")
		}
		c.Close()
		c, err = Compile(NewRuntimeConfig().WithFeature(CoreFeatureBulkMemoryOperations, false), rv64DeclarativeModule(t, vector, false, false, 0))
		if c != nil {
			c.Close()
		}
		if c != nil || err == nil {
			t.Fatal("bulk feature disabled", err)
		}
	}
}

func TestRV64PublicDeclarativeElementsWithoutTable(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, expressions := range []bool{false, true} {
			for _, references := range []bool{false, true} {
				for _, drop := range []int{-1, 0, 255} {
					for _, artifact := range []bool{false, true} {
						name := fmt.Sprintf("decl-v%t-expr%t-ref%t-drop%d", vector, expressions, references, drop)
						t.Run(fmt.Sprintf("%s-artifact%t", name, artifact), func(t *testing.T) {
							c := rv64TableStageCompile(t, name, rv64DeclarativeModule(t, vector, expressions, references, drop), artifact)
							rt := NewRuntime()
							defer rt.Close()
							in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
							defer in.Close()
							if c.tableCount() != 0 || in.tableDescPtr != 0 || len(in.globalCells) != 0 {
								t.Fatal("declaration allocated table/global state")
							}
							if !references && len(in.funcRefDescs) != 0 {
								t.Fatal("inert declaration allocated descriptors")
							}
							var directory []byte
							if drop < 0 {
								if len(in.c.passiveElems) != 0 || in.jm.PassiveElemPtr() != 0 {
									t.Fatal("unused element directory retained")
								}
							} else {
								if len(in.c.passiveElems) != drop+1 || in.jm.PassiveElemPtr() == 0 {
									t.Fatal("sparse element directory extent", len(in.c.passiveElems), drop)
								}
								directory = unsafe.Slice((*byte)(offHeapPtr(in.jm.PassiveElemPtr())), (drop+1)*16)
								if !bytes.Equal(directory, make([]byte, len(directory))) {
									t.Fatal("declaration payload was materialized")
								}
							}
							want := uint64(7)
							if references {
								want = 0
							}
							before := bytes.Clone(directory)
							for repeat := 0; repeat < 3; repeat++ {
								importedGlobalInvoke(t, in, "carrier", []uint64{want}, 0xdeadbeef00000000)
								importedGlobalInvoke(t, in, "drop", []uint64{7})
							}
							if !bytes.Equal(directory, before) || len(rt.refStore.byToken) != 0 {
								t.Fatal("declarative drop changed state or issued tokens")
							}
							for _, el := range in.c.passiveElems {
								if len(el.Values) != 0 {
									t.Fatal("declaration retained payload")
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestRV64PublicDeclarativeElementMetadata(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source and metadata admission")
	}
	for _, vector := range []bool{false, true} {
		for _, expressions := range []bool{false, true} {
			c := rv64TableStageCompile(t, fmt.Sprintf("decl-v%t-expr%t-reffalse-drop255", vector, expressions), rv64DeclarativeModule(t, vector, expressions, false, 255), false)
			for name, mutate := range map[string]func(*Compiled){
				"count":         func(v *Compiled) { v.passiveElems = append(v.passiveElems, ElemInit{Mode: ElemModeDeclarative}) },
				"bulk":          func(v *Compiled) { v.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
				"refs":          func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
				"frame":         func(v *Compiled) { v.registerABIDisabled = false },
				"context":       func(v *Compiled) { v.nativeTableContextABI = false },
				"reference-abi": func(v *Compiled) { v.nativeTableReferenceABI = false },
				"active":        func(v *Compiled) { v.passiveElems[0].Mode = ElemModeActive },
				"unknown-mode":  func(v *Compiled) { v.passiveElems[0].Mode = ElemMode(255) },
				"payload":       func(v *Compiled) { v.passiveElems[0].Values = []RefInit{{FuncIndex: 0}} },
				"offset":        func(v *Compiled) { v.passiveElems[0].Offset.Base = 1 },
				"global":        func(v *Compiled) { v.passiveElems[0].Offset.HasGlobal = true },
				"table":         func(v *Compiled) { v.passiveElems[0].TableIndex = 1 },
				"type":          func(v *Compiled) { v.passiveElems[0].RefType = ValExternRef },
				"exact": func(v *Compiled) {
					v.passiveElems[0].HasValueType = true
					v.passiveElems[0].ValueTypeIndex = ^uint32(0)
				},
			} {
				v := *c.executionView()
				v.passiveElems = slices.Clone(v.passiveElems)
				mutate(&v)
				if err := railshotValidateCompiled(&v); err == nil {
					t.Fatal("declaration metadata admitted", name)
				}
				blob, err := marshalCompiled(&v)
				if err == nil {
					loaded, err := LoadTrustedArtifact(blob)
					if loaded != nil {
						// The codec reconstructs required features from structural
						// element metadata; verify normalization instead of rejection.
						normalized := (name == "bulk" && loaded.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) || name == "refs" && loaded.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes)) && len(loaded.passiveElems) == len(c.passiveElems)
						loaded.Close()
						if err == nil && normalized {
							continue
						}
					}
					if err == nil {
						t.Fatal("declaration artifact admitted", name)
					}
				}
			}
		}
	}
}
