//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func referenceControlProductionOptions(typed bool, pressure *int) CompileOptions {
	return CompileOptions{NativeReferenceCalls: typed, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, MemoryPressure: func() { *pressure++ }}
}

func TestReferenceControlProductionDeclarations(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, indexed := range []bool{false, true} {
			for _, nullable := range []bool{false, true} {
				for _, defined := range []bool{false, true} {
					for _, imported := range []bool{false, true} {
						t.Run(fmt.Sprintf("vector%t-indexed%t-nullable%t-defined%t-imported%t", vector, indexed, nullable, defined, imported), func(t *testing.T) {
							heap := wasm.AbsHeap(wasm.HeapFunc)
							if indexed {
								heap = wasm.IndexedHeap(wasm.TypeIdx{})
							}
							ref := wasm.RefVal(wasm.Ref(nullable, heap, false))
							types := [][]byte{wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef})}
							if vector {
								types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))
							}
							sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
							if imported {
								im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
								sections = append(sections, wasmtest.Section(2, wasmtest.Vec(im)))
							}
							if defined {
								sections = append(sections, wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))))
							}
							m, err := wasm.DecodeModule(wasmtest.Module(sections...))
							if err != nil {
								t.Fatal(err)
							}
							m.Types[1].SubTypes[0].Comp.Params[0], m.Types[1].SubTypes[0].Comp.Results[0] = ref, ref
							if err = wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
								t.Fatal(err)
							}
							witness := indexed || !nullable
							for _, typed := range []bool{false, true} {
								pressure := 0
								opts := referenceControlProductionOptions(typed, &pressure)
								if imported {
									opts.ImportBindings = hostImportBindings(1)
								}
								image, err := CompileModuleWith(m, opts)
								allowed := !witness || typed && defined
								if !allowed {
									if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
										t.Fatal("closed typed declaration", typed, err, pressure)
									}
									continue
								}
								if image == nil || err != nil || image.NativeReferenceControl != witness || image.NativeReferenceCalls || image.CodeImage != nil || pressure != 1 {
									t.Fatal("unused declaration witness", typed, err, pressure)
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestReferenceControlProductionNumericCallableBoundary(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, binding := range []string{"defined", "import", "heap-null", "heap-local"} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, binding), func(t *testing.T) {
				m, _, _ := referenceControlModule(t, []byte{0x0b})
				if vector {
					m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}}}}})
				}
				switch binding {
				case "defined":
					m.FuncTypes[0] = wasm.TypeIdx{Index: 1}
				case "import":
					m.Imports = []wasm.Import{{Module: "env", Name: "bad", Type: wasm.NewFuncExternType(wasm.TypeIdx{Index: 1})}}
				case "heap-null":
					m.Code[0].BodyBytes = []byte{0xd0, 1, 0x1a, 0x0b}
				case "heap-local":
					m.Code[0].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false))}}
				}
				pressure := 0
				image, err := CompileModuleWith(m, referenceControlProductionOptions(true, &pressure))
				if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("reference control reached callable/heap ABI", err, pressure)
				}
			})
		}
	}
}

func TestReferenceControlProductionAggregateTypeBudget(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, over := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-over%t", vector, over), func(t *testing.T) {
				m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b})))))
				if err != nil {
					t.Fatal(err)
				}
				remaining := MaxModuleBytes - len(m.Code[0].BodyBytes)
				limit := 256
				if vector {
					limit = 128
					remaining -= int(m.Code[0].LocalDeclBytes)
					m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}}}}})
					remaining--
				}
				if over {
					remaining++
				}
				for remaining > 0 {
					values := make([]wasm.ValType, 0, 2*limit)
					for len(values) < 2*limit && remaining > 0 {
						typ, cost := wasm.FuncRef, 6
						if remaining < 6 {
							typ, cost = wasm.I32, 1
						}
						values = append(values, typ)
						remaining -= cost
					}
					n := min(limit, len(values))
					m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: values[:n], Results: values[n:]}}}})
				}
				pressure := 0
				image, err := CompileModuleWith(m, referenceControlProductionOptions(false, &pressure))
				if over {
					if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
						t.Fatal("over-budget declarations staged", err, pressure)
					}
				} else if image == nil || err != nil || pressure != 1 {
					t.Fatal("exact aggregate type budget", err, pressure)
				}
			})
		}
	}
}
