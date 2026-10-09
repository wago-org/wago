//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func closedReferenceProductionOptions(tails, typed bool) CompileOptions {
	return CompileOptions{NativeReferenceFunctions: true, NativeReferenceCalls: typed, NativeTailCalls: tails, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true}
}

func TestReferenceFunctionProductionStaging(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tails := range []bool{false, true} {
			for _, indexed := range []bool{false, true} {
				for _, nullable := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t-tails%t-indexed%t-nullable%t", vector, tails, indexed, nullable), func(t *testing.T) {
						m := closedReferenceDecode(t, vector, tails, indexed, nullable)
						if nullable && !indexed {
							if err := frontend.RejectUnsupportedWithFeatures(m, frontend.Features{ReferenceTypes: true, BulkMemory: true, SIMD: vector, TailCalls: tails}); err != nil {
								t.Fatal("baseline nullable fixture requires typed features", err)
							}
						}
						private, err := closedReferenceCompile(m, vector, tails)
						if err != nil {
							t.Fatal(err)
						}
						for _, heap := range []bool{false, true} {
							opts := closedReferenceProductionOptions(tails, indexed || !nullable)
							opts.DeferCodeMapping = heap
							pressure := 0
							opts.MemoryPressure = func() { pressure++ }
							image, err := CompileModuleWith(m, opts)
							if err != nil {
								t.Fatal("closed staging", heap, err)
							}
							if !image.NativeReferenceFunctions || image.NativeReferenceCalls || image.NativeReferenceControl != (indexed || !nullable) || (image.NativeTail != nil) != tails || image.NativeTail != nil && len(image.NativeTail.Plans) != 0 || pressure != 1 {
								t.Fatal("actual ABI/feature/plan witnesses", image.NativeReferenceFunctions, image.NativeReferenceControl, image.NativeTail, pressure)
							}
							if !bytes.Equal(image.Code, private.code) || len(image.Entry) != 5 || len(image.InternalEntry) != 5 {
								t.Fatal("closed code/physical function extent")
							}
							for i, f := range private.functions {
								if image.Entry[i] != f.Entry || image.InternalEntry[i] != f.Entry {
									t.Fatal("closed entry ordinal", i)
								}
							}
							if len(image.DirectPrepared) != 0 || len(image.DirectPreparedLight) != 0 || len(image.DirectPreparedBounded) != 0 || image.PreparedIsolatedTables || image.RequiredAMD64Features != 0 || image.RequiresBMI2 || image.RequiresAVX2 || image.RequiresAVX512 || image.RequiresBitCount != 0 {
								t.Fatal("unqualified entry/ISA metadata")
							}
							if heap {
								if image.CodeImage != nil {
									t.Fatal("deferred heap ownership")
								}
							} else {
								if image.CodeImage == nil {
									t.Fatal("missing RW ownership")
								}
								mapping, base, err := image.CodeImage.Take()
								if err != nil {
									t.Fatal(err)
								}
								if base == 0 || !bytes.Equal(mapping[:len(image.Code)], private.code) {
									t.Error("mapping transfer/code")
								}
								if err := coreruntime.Unmap(mapping); err != nil {
									t.Fatal(err)
								}
								if err := image.CodeImage.Close(); err != nil {
									t.Fatal(err)
								}
							}
						}
						if indexed || !nullable {
							if image, err := CompileModuleWith(m, closedReferenceProductionOptions(tails, false)); image != nil || !errors.Is(err, ErrUnsupported) {
								t.Fatal("typed signature silently became baseline", err)
							}
						}
					})
				}
			}
		}
	}
}

func TestReferenceFunctionProductionBareNullable(t *testing.T) {
	// Only the original one-byte funcref signature and ReferenceTypes opcodes;
	// no typed-reference, bulk-memory, tail-call or multi-value requirement.
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}))),
		wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(append(wasmtest.Name("run"), 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x10, 1, 0xd1, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}))))
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := frontend.RejectUnsupportedWithFeatures(m, frontend.Features{ReferenceTypes: true}); err != nil {
		t.Fatal("baseline ReferenceTypes source", err)
	}
	if n := testing.AllocsPerRun(10, func() {
		selected, err := hasDefinedReferenceFunctions(m)
		if err != nil || !selected || admitClosedReferenceFunctionModule(m, false) != nil {
			panic("closed baseline admission")
		}
	}); n != 0 {
		t.Fatal("selection/closed proof allocation", n)
	}
	image, err := CompileModuleWith(m, closedReferenceProductionOptions(false, false))
	if err != nil || !image.NativeReferenceFunctions || image.NativeReferenceControl || image.NativeReferenceCalls || image.NativeTail != nil {
		t.Fatal("baseline ABI witness", err)
	}
}

func TestReferenceFunctionProductionPermissionOnly(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, imported := range []bool{false, true} {
			for _, controls := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-imported%t-controls%t", vector, imported, controls), func(t *testing.T) {
					typ := byte(0x7f)
					if vector {
						typ = 0x7b
					}
					data := binaryModule(fixtureFunc{params: []byte{typ}, result: typ, body: []byte{0x20, 0, 0x0b}})
					if imported {
						data = hostImportModule(1, 0, []byte{typ}, false)
					}
					m, err := wasm.DecodeModule(data)
					if err != nil {
						t.Fatal(err)
					}
					if controls {
						m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}, Results: []wasm.ValType{wasm.FuncRef}}}}})
					}
					opts := closedReferenceProductionOptions(true, true)
					opts.NativeReferenceFunctions = false
					if imported {
						opts.ImportBindings = hostImportBindings(1)
					}
					before, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal("ordinary fixture", err)
					}
					opts.NativeReferenceFunctions = true
					after, err := CompileModuleWith(m, opts)
					if err != nil || !reflect.DeepEqual(before, after) || after.NativeReferenceFunctions {
						t.Fatal("permission changed numeric/control-only module", err)
					}
				})
			}
		}
	}
	// The isolated byte-source API does not acquire retained owner authority.
	if c, err := CompileWithOptions(nil, CompileOptions{NativeReferenceFunctions: true}); c != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("permission reached isolated decoding", err)
	}
}

func TestReferenceFunctionProductionAdmissionBeforeStaging(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, test := range []struct {
			name   string
			change func(*wasm.Module, *CompileOptions)
		}{
			{"permission", func(m *wasm.Module, o *CompileOptions) { o.NativeReferenceFunctions = false }},
			{"cooperative", func(m *wasm.Module, o *CompileOptions) { o.Interruptible = false }},
			{"owner", func(m *wasm.Module, o *CompileOptions) { o.ForeignTableCalls = false }},
			{"invocation", func(m *wasm.Module, o *CompileOptions) { o.InvocationContext = false }},
			{"profile", func(m *wasm.Module, o *CompileOptions) { o.Profile = true }},
			{"source-map", func(m *wasm.Module, o *CompileOptions) { o.SourceMaps = true }},
			{"unwind", func(m *wasm.Module, o *CompileOptions) { o.UnwindMaps = true }},
			{"reference-export", func(m *wasm.Module, o *CompileOptions) { m.Exports[0].Index.Index = 2 }},
			{"reference-descriptor", func(m *wasm.Module, o *CompileOptions) { m.Elements[0].Kind.Funcs[0] = 2 }},
			{"reference-escape", func(m *wasm.Module, o *CompileOptions) { m.Code[1].BodyBytes = []byte{0x00, 0xd2, 2, 0x1a, 0x0b} }},
			{"reference-call", func(m *wasm.Module, o *CompileOptions) { m.Code[1].BodyBytes = []byte{0x00, 0x14, 0, 0x0b} }},
			{"indirect-tail", func(m *wasm.Module, o *CompileOptions) { m.Code[1].BodyBytes = []byte{0x00, 0x13, 0, 0, 0x0b} }},
			{"reference-tail", func(m *wasm.Module, o *CompileOptions) { m.Code[1].BodyBytes = []byte{0x00, 0x15, 0, 0x0b} }},
			{"memory", func(m *wasm.Module, o *CompileOptions) { m.Memories = []wasm.MemType{{}} }},
			{"late-invalid-binding", func(m *wasm.Module, o *CompileOptions) { m.FuncTypes[4].Index = 999 }},
			{"late-reference-import", func(m *wasm.Module, o *CompileOptions) {
				m.Imports = []wasm.Import{{Type: wasm.NewFuncExternType(wasm.TypeIdx{Index: 2})}}
			}},
			{"inactive-fields", func(m *wasm.Module, o *CompileOptions) { m.Types[2].SubTypes[0].Comp.Fields = []wasm.FieldType{{}} }},
			{"local-metadata-bound", func(m *wasm.Module, o *CompileOptions) { m.Code[4].LocalDeclBytes = MaxModuleBytes + 1 }},
			{"depth-bound", func(m *wasm.Module, o *CompileOptions) {
				m.Code[1].BodyBytes = append(bytes.Repeat([]byte{2, 0x40}, maxControlNesting+1), bytes.Repeat([]byte{0x0b}, maxControlNesting+2)...)
			}},
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, test.name), func(t *testing.T) {
				m := closedReferenceDecode(t, vector, true, false, true)
				opts := closedReferenceProductionOptions(true, false)
				pressure := 0
				opts.MemoryPressure = func() { pressure++ }
				test.change(m, &opts)
				if image, err := CompileModuleWith(m, opts); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("closed admission reached staging", err, pressure)
				}
			})
		}
		m := closedReferenceDecode(t, vector, false, false, true)
		m.Code[3].BodyBytes = []byte{0x0b}
		opts := closedReferenceProductionOptions(false, false)
		pressure := 0
		opts.MemoryPressure = func() { pressure++ }
		var invalid *wasm.ValidationError
		if image, err := CompileModuleWith(m, opts); image != nil || !errors.As(err, &invalid) || invalid.Code != wasm.ErrTypeMismatch || pressure != 0 {
			t.Fatal("whole-module frontend skipped", vector, err, pressure)
		}
	}
}

func TestReferenceFunctionProductionStagingRechecksProofAndModes(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, test := range []struct {
			name   string
			change func(*Compiled, *wasm.Module, *CompileOptions)
		}{
			{"permission", func(c *Compiled, m *wasm.Module, o *CompileOptions) { o.NativeReferenceFunctions = false }},
			{"selection", func(c *Compiled, m *wasm.Module, o *CompileOptions) { o.referenceFunctions = false }},
			{"state", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.state.referenceFunctions = false }},
			{"count", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.functions = c.functions[:4] }},
			{"imports", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.state.importedFunctions = 1 }},
			{"empty-code", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.code = nil }},
			{"late-mode", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.functions[4].mode.referenceFunctions = false }},
			{"late-invocation", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.functions[4].mode.invocationContext = false }},
			{"late-frame", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.functions[4].FrameHeaderBytes = 0 }},
			{"late-dynamic", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.functions[4].mode.dynamicTailCalls = true }},
			{"tail-mode", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.functions[4].mode.localTailCalls = true }},
			{"typed-mode", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.functions[4].mode.referenceCalls = false }},
			{"spurious-control", func(c *Compiled, m *wasm.Module, o *CompileOptions) { o.referenceControlInstructions = true }},
			{"changed-export", func(c *Compiled, m *wasm.Module, o *CompileOptions) { m.Exports[0].Index.Index = 2 }},
			{"changed-body", func(c *Compiled, m *wasm.Module, o *CompileOptions) {
				m.Code[4].BodyBytes = []byte{0, 0xd2, 2, 0x1a, 0x0b}
			}},
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, test.name), func(t *testing.T) {
				m := closedReferenceDecode(t, vector, false, false, true)
				c, err := closedReferenceCompile(m, vector, false)
				if err != nil {
					t.Fatal(err)
				}
				opts := closedReferenceProductionOptions(false, true)
				opts.referenceFunctions = true
				pressure := 0
				opts.MemoryPressure = func() { pressure++ }
				test.change(c, m, &opts)
				if image, err := stageClosedReferenceProductionModule(c, opts, m, vector); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("staging accepted incoherent proof/modes", err, pressure)
				}
			})
		}
	}
}

func TestReferenceFunctionProductionSymbolicPlanConfinement(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, test := range []struct {
			name   string
			change func(*wasm.Module, *CompileOptions)
		}{
			{"nil-plans", func(m *wasm.Module, o *CompileOptions) { o.tailPlans = nil }},
			{"foreign-plan", func(m *wasm.Module, o *CompileOptions) {
				*o.tailPlans = []tailImportPlan{{kind: enc.NativeTailDirectImport}}
			}},
			{"nil-source", func(m *wasm.Module, o *CompileOptions) { o.tailModule = nil }},
			{"different-source", func(m *wasm.Module, o *CompileOptions) { other := *m; o.tailModule = &other }},
			{"reference-call", func(m *wasm.Module, o *CompileOptions) { o.referenceCallInstructions = true }},
			{"reference-tail", func(m *wasm.Module, o *CompileOptions) { o.referenceTailInstructions = true }},
			{"missing-control", func(m *wasm.Module, o *CompileOptions) { o.referenceControlInstructions = false }},
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, test.name), func(t *testing.T) {
				m := closedReferenceDecode(t, vector, true, true, true)
				c, err := closedReferenceCompile(m, vector, true)
				if err != nil {
					t.Fatal(err)
				}
				opts := closedReferenceProductionOptions(true, true)
				opts.referenceFunctions, opts.localTailCalls, opts.referenceControlInstructions = true, true, true
				opts.tailModule = m
				var plans []tailImportPlan
				opts.tailPlans = &plans
				if image, err := stageClosedReferenceProductionModule(c, opts, m, vector); image == nil || err != nil {
					t.Fatal("valid closed staging baseline", err)
				}
				pressure := 0
				opts.MemoryPressure = func() { pressure++ }
				test.change(m, &opts)
				if image, err := stageClosedReferenceProductionModule(c, opts, m, vector); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("symbolic plans escaped closed proof", err, pressure)
				}
			})
		}
	}
}
