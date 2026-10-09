//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func referenceLocalCellsDecode(t *testing.T, vector, tails, wide bool, start int) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.ClosedReferenceLocalCells(vector, tails, wide, start))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReferenceFunctionLocalCellsSource(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tails := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				for _, start := range []int{0, 1, 2} {
					t.Run(fmt.Sprintf("vector%t-tails%t-wide%t-start%d", vector, tails, wide, start), func(t *testing.T) {
						m := referenceLocalCellsDecode(t, vector, tails, wide, start)
						if has, err := admitLocalMutableFunctionGlobals(m); !has || err != nil {
							t.Fatal("bounded private cell proof", has, err)
						}
						if err := AdmitPublicInitializers(m); err != nil {
							t.Fatal(err)
						}
						if a := testing.AllocsPerRun(5, func() {
							remaining := MaxModuleBytes
							if err := admitClosedReferenceNumericState(m, vector, &remaining); err != nil {
								panic(err)
							}
						}); a != 0 {
							t.Fatal("literal cell preflight allocation", a)
						}
						opts := closedReferenceProductionOptions(tails, false)
						opts.ImportBindings = hostImportBindings(3)
						image, err := CompileModuleWith(m, opts)
						if err != nil || image == nil || !image.NativeReferenceFunctions || len(image.Entry) != 8 {
							t.Fatal("private cell staging", err)
						}
						if tails && (image.NativeTail == nil || len(image.NativeTail.Plans) != 1) {
							t.Fatal("private cell direct imported tail")
						}
						opts.NativeReferenceFunctions = false
						if image, err := CompileModuleWith(m, opts); image != nil || !errors.Is(err, ErrUnsupported) {
							t.Fatal("legacy staging accepted private cell ABI", err)
						}
					})
				}
			}
		}
	}
}

func TestReferenceFunctionLocalCellsAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for name, mutate := range map[string]func(*wasm.Module){
			"immutable":      func(m *wasm.Module) { m.Globals[0].Type.Mutable = false },
			"extern":         func(m *wasm.Module) { m.Globals[0].Type.Type = wasm.ExternRef },
			"copy":           func(m *wasm.Module) { m.Globals[1].Init.BodyBytes = []byte{0x23, 2, 0x0b} },
			"private-target": func(m *wasm.Module) { m.Globals[1].Init.BodyBytes = []byte{0xd2, 4, 0x0b} },
			"missing-target": func(m *wasm.Module) { m.Globals[1].Init.BodyBytes = []byte{0xd2, 11, 0x0b} },
			"private-export": func(m *wasm.Module) { m.Exports[0].Index.Index = 4 },
			"cell-export":    func(m *wasm.Module) { m.Exports[0].Index.Kind = wasm.ExternGlobal; m.Exports[0].Index.Index = 2 },
			"reference-import": func(m *wasm.Module) {
				m.Imports[2].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.FuncRef})
			},
			"structured":              func(m *wasm.Module) { m.Globals[0].Init.Instrs = []wasm.Instruction{{}} },
			"wrong-null":              func(m *wasm.Module) { m.Globals[0].Init.BodyBytes = []byte{0xd0, 0x6f, 0x0b} },
			"private-dead-descriptor": func(m *wasm.Module) { m.Code[2].BodyBytes = []byte{0x00, 0xd2, 9, 0x1a, 0x0b} },
			"try-table":               func(m *wasm.Module) { m.Code[2].BodyBytes = []byte{0x1f, 0x40, 0xff, 0xff, 0xff, 0xff, 0x0f} },
			"oversize-type-pool":      func(m *wasm.Module) { m.Types = make([]wasm.RecType, MaxFunctions+1) },
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				m := referenceLocalCellsDecode(t, vector, true, true, 2)
				mutate(m)
				if _, err := admitLocalMutableFunctionGlobals(m); !errors.Is(err, ErrUnsupported) {
					t.Fatal("early cell helper bypassed closed proof", err)
				}
				opts := closedReferenceProductionOptions(true, false)
				opts.ImportBindings = hostImportBindings(3)
				pressure := 0
				opts.MemoryPressure = func() { pressure++ }
				if image, err := CompileModuleWith(m, opts); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("bad cell reached publication", err, pressure)
				}
			})
		}
	}
}

func TestReferenceFunctionLocalCellsCarrierLimits(t *testing.T) {
	for _, vector := range []bool{false, true} {
		limit := 256
		if vector {
			limit = 128
		}
		for _, dimension := range []string{"params", "results", "locals"} {
			for _, count := range []int{limit, limit + 1} {
				t.Run(fmt.Sprintf("vector%t-%s%d", vector, dimension, count), func(t *testing.T) {
					m := referenceLocalCellsDecode(t, vector, true, true, 0)
					// This extra unexported function has a reference binding and no
					// call sites; isolate its exact declared carrier boundary.
					params, results := []wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}
					locals := wasm.Locals{}
					body := []byte{0x20, 0, 0x0b}
					if dimension == "params" {
						params = make([]wasm.ValType, count)
						for i := range params {
							params[i] = wasm.FuncRef
						}
					}
					if dimension == "results" {
						results = make([]wasm.ValType, count)
						body = nil
						for i := range results {
							results[i] = wasm.FuncRef
							body = append(body, 0x20, 0)
						}
						body = append(body, 0x0b)
					}
					if dimension == "locals" {
						locals.Runs = []wasm.LocalRun{{Count: uint32(count - 1), Type: wasm.FuncRef}}
					}
					typ := wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: results}
					m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: typ}}})
					m.FuncTypes = append(m.FuncTypes, wasm.TypeIdx{Index: uint32(len(m.Types) - 1)})
					m.Code = append(m.Code, wasm.Func{Locals: locals, BodyBytes: body})
					err := AdmitPublicInitializers(m)
					if count <= limit && err != nil || count > limit && dimension != "locals" && err == nil {
						t.Fatal("early initializer carrier bound", err)
					}
					// For SWAR local129, the bounded scalar proof may succeed early;
					// exact SWAR staging still has to reject before image allocation.
					opts := closedReferenceProductionOptions(true, false)
					opts.ImportBindings = hostImportBindings(3)
					image, err := CompileModuleWith(m, opts)
					if count <= limit {
						if err != nil || image == nil {
							t.Fatal("exact carrier limit rejected", err)
						}
					} else if image != nil || !errors.Is(err, ErrUnsupported) {
						t.Fatal("carrier limit+1 admitted", err)
					}
				})
			}
		}
	}
}

func TestReferenceFunctionLocalCellsAggregateBudget(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			m := referenceLocalCellsDecode(t, vector, true, true, 1)
			padding := make([]byte, MaxModuleBytes)
			m.Customs = []wasm.CustomSec{{Name: "opaque"}}
			lo, hi := 0, len(padding)
			for lo < hi {
				mid := (lo + hi + 1) / 2
				m.Customs[0].Data = padding[:mid]
				if admitClosedReferenceFunctionModule(m, vector) == nil {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			m.Customs[0].Data = padding[:lo]
			if lo == 0 || AdmitPublicInitializers(m) != nil || admitClosedReferenceFunctionModule(m, vector) != nil {
				t.Fatal("exact cell-bearing source budget")
			}
			m.Globals = append(m.Globals, m.Globals[0])
			if err := AdmitPublicInitializers(m); !errors.Is(err, ErrUnsupported) {
				t.Fatal("extra literal reference cell escaped aggregate budget", err)
			}
			m.Globals = m.Globals[:len(m.Globals)-1]
			if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
				t.Fatal("cell budget not restored", err)
			}
			m.Customs[0].Data = padding[:lo+1]
			if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
				t.Fatal("source cell budget+1 admitted", err)
			}
		})
	}
}
