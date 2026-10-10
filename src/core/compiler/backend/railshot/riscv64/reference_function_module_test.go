//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func closedReferenceOptions(tails bool) CompileOptions {
	return CompileOptions{production: true, publicStarts: true, referenceFunctions: true, NativeReferenceCalls: true, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, localTailCalls: tails, DeferCodeMapping: true}
}
func closedReferenceMode(tails bool) emissionMode {
	return emissionMode{production: true, interruptible: true, foreignTableCalls: true, invocationContext: true, referenceCalls: true, referenceFunctions: true, localTailCalls: tails}
}
func closedReferenceCompile(m *wasm.Module, vector, tails bool) (*Compiled, error) {
	if !vector {
		return compileDecodedModule(m, scalarRegisters[:], closedReferenceOptions(tails))
	}
	image, err := compileSWARDecodedModuleProfile(m, closedReferenceMode(tails), swarModuleTables, swarStateProfile{publicStarts: true, exports: true, publicInitializers: true, importedTable: true})
	if err != nil {
		return nil, err
	}
	return &Compiled{code: image.code, functions: image.functions, state: moduleState{referenceFunctions: true, localTailCalls: tails}}, nil
}
func closedReferenceSource(vector, tails, indexed, nullable bool) []byte {
	typ := wasm.F64
	if vector {
		typ = wasm.V128
	}
	heap := wasm.AbsHeap(wasm.HeapFunc)
	heapByte := byte(0x70)
	if indexed {
		heap = wasm.IndexedHeap(wasm.TypeIdx{})
		heapByte = 0
	}
	ref := wasm.RefVal(wasm.Ref(nullable, heap, false))
	signature := func(params, results []wasm.ValType) []byte {
		out := []byte{0x60}
		for _, values := range [][]wasm.ValType{params, results} {
			out = append(out, wasmtest.ULEB(uint32(len(values)))...)
			for _, v := range values {
				if v.Kind() != wasm.ValRef {
					out = append(out, wasm.MustEncodeValType(v))
					continue
				}
				lead := byte(0x64)
				if v.Ref().Nullable() {
					lead = 0x63
				}
				out = append(out, lead, heapByte)
			}
		}
		return out
	}
	types := [][]byte{wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32, typ}, []wasm.ValType{typ, wasm.I32, wasm.I32}), signature([]wasm.ValType{ref, wasm.I32, typ}, []wasm.ValType{typ, wasm.I32, ref}), signature([]wasm.ValType{ref}, []wasm.ValType{ref})}
	lead := byte(0x64)
	if nullable {
		lead = 0x63
	}
	// Numeric descriptor targets 1 and 4 surround the private reference
	// functions 2 and 3. Directory rows must retain those function-index holes.
	root := []byte{0x20, 0, 0x41, 1, 0x71, 4, lead, heapByte, 0xd2, 1, 5, 0xd2, 4, 0x0b, 0x20, 0, 0x20, 1, 0x10, 2, 2, 3, 0x0c, 0, 0x0b, 0xd1, 0x0b}
	call := byte(0x10)
	if tails {
		call = 0x12
	}
	forward := []byte{0x20, 0, 0x20, 1, 0x20, 2, call, 3, 0x0b}
	leaf := []byte{0x20, 2, 0x20, 1, 0x41, 5, 0x6a, 0x20, 0, 0x0b}
	export := append(wasmtest.Name("run"), 0, 0)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, []byte{5, 1, 0, 2, 2, 0}), wasmtest.Section(7, wasmtest.Vec(export)), wasmtest.Section(9, []byte{1, 3, 0, 2, 1, 4}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(root), wasmtest.Code([]byte{0x0b}), wasmtest.Code(forward), wasmtest.Code(leaf), wasmtest.Code([]byte{0x0b}))))
}
func closedReferenceDecode(t *testing.T, vector, tails, indexed, nullable bool) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(closedReferenceSource(vector, tails, indexed, nullable))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func publicReferenceCallBoundaryDecodeResult(t *testing.T, referenceResult bool) *wasm.Module {
	t.Helper()
	resultType := byte(0x7f)
	callerBody := []byte{0x20, 0, 0x14, 0, 0x0b}
	if referenceResult {
		resultType = 0x70
		callerBody = []byte{0x20, 0, 0x14, 0, 0x1a, 0x20, 0, 0x0b}
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
			[]byte{0x60, 1, 0x63, 0, 1, resultType},
		)),
		wasmtest.Section(3, []byte{3, 0, 1, 2}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", 0, 0),
			wasmtest.ExportEntry("get", 0, 1),
			wasmtest.ExportEntry("call", 0, 2),
		)),
		wasmtest.Section(9, []byte{1, 3, 0, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 42, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x0b}),
			wasmtest.Code(callerBody),
		)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func publicReferenceCallBoundaryDecode(t *testing.T) *wasm.Module {
	return publicReferenceCallBoundaryDecodeResult(t, false)
}

func TestPublicReferenceCallBoundaryExactAdmission(t *testing.T) {
	for _, referenceResult := range []bool{false, true} {
		base := publicReferenceCallBoundaryDecodeResult(t, referenceResult)
		if !admitPublicReferenceCallBoundary(base) {
			t.Fatal("exact public call_ref boundary was not recognized", referenceResult)
		}
		if err := admitClosedReferenceFunctionModule(base, false); err != nil {
			t.Fatal("exact public call_ref boundary was not admitted", referenceResult, err)
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*wasm.Module)
	}{
		{"custom-section", func(m *wasm.Module) { m.Customs = []wasm.CustomSec{{Name: "x"}} }},
		{"memory", func(m *wasm.Module) { m.Memories = []wasm.MemType{{}} }},
		{"passive-element", func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemPassive }},
		{"reference-target", func(m *wasm.Module) { m.Elements[0].Kind.Funcs[0] = 1 }},
		{"inactive-element-ref", func(m *wasm.Module) { m.Elements[0].Kind.Ref = wasm.FuncRef.Ref() }},
		{"producer-body", func(m *wasm.Module) { m.Code[1].BodyBytes = []byte{0xd2, 0, 0x1a, 0xd2, 0, 0x0b} }},
		{"caller-body", func(m *wasm.Module) { m.Code[2].BodyBytes = []byte{0x20, 0, 0x01, 0x14, 0, 0x0b} }},
		{"caller-binding", func(m *wasm.Module) { m.FuncTypes[2].Index = 1 }},
		{"reference-result-with-numeric-body", func(m *wasm.Module) { m.Types[2].SubTypes[0].Comp.Results[0] = wasm.FuncRef }},
		{"export-name", func(m *wasm.Module) { m.Exports[0].Name = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := publicReferenceCallBoundaryDecode(t)
			tc.mutate(m)
			if admitPublicReferenceCallBoundary(m) {
				t.Fatal("nearby public call_ref shape inherited the boundary")
			}
			if err := admitClosedReferenceFunctionModule(m, false); !errors.Is(err, ErrUnsupported) {
				t.Fatal("nearby public call_ref shape was admitted", err)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*wasm.Module)
	}{
		{"numeric-body", func(m *wasm.Module) { m.Code[2].BodyBytes = []byte{0x20, 0, 0x14, 0, 0x0b} }},
		{"extern-result", func(m *wasm.Module) { m.Types[2].SubTypes[0].Comp.Results[0] = wasm.ExternRef }},
		{"different-return", func(m *wasm.Module) { m.Code[2].BodyBytes = []byte{0x20, 0, 0x14, 0, 0x1a, 0xd0, 0x70, 0x0b} }},
	} {
		t.Run("reference-result-"+tc.name, func(t *testing.T) {
			m := publicReferenceCallBoundaryDecodeResult(t, true)
			tc.mutate(m)
			if admitPublicReferenceCallBoundary(m) {
				t.Fatal("nearby public call_ref reference-result shape inherited the boundary")
			}
			if err := admitClosedReferenceFunctionModule(m, false); !errors.Is(err, ErrUnsupported) {
				t.Fatal("nearby public call_ref reference-result shape was admitted", err)
			}
		})
	}
}

func TestReferenceFunctionModulePrivatePipeline(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tails := range []bool{false, true} {
			for _, indexed := range []bool{false, true} {
				for _, nullable := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t-tails%t-indexed%t-nullable%t", vector, tails, indexed, nullable), func(t *testing.T) {
						m := closedReferenceDecode(t, vector, tails, indexed, nullable)
						if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
							t.Fatal("closed proof", err)
						}
						if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
							t.Fatal("closed fixture frontend", err)
						}
						c, err := closedReferenceCompile(m, vector, tails)
						if err != nil {
							t.Fatal("actual whole-module lowering", err)
						}
						if len(c.code) == 0 || len(c.functions) != 5 {
							t.Fatal("actual module image")
						}
						for _, f := range c.functions {
							if !f.mode.referenceFunctions || f.mode.localTailCalls != tails || !f.mode.invocationContext {
								t.Fatal("module admission/emission contract", f)
							}
						}
						for _, stage := range []func(*Compiled) error{
							func(c *Compiled) error {
								image, e := stageProduction(c, true)
								if image != nil {
									t.Fatal("private image staged")
								}
								return e
							},
							func(c *Compiled) error {
								image, e := stageProductionModule(c, closedReferenceOptions(tails))
								if image != nil {
									t.Fatal("private module staged")
								}
								return e
							},
							func(c *Compiled) error {
								i, e := c.Instantiate(0)
								if i != nil {
									t.Fatal("private instance allocated")
								}
								return e
							},
							func(c *Compiled) error {
								i, e := c.InstantiateWithOptions(0, InstantiateOptions{})
								if i != nil {
									t.Fatal("private instance options allocated")
								}
								return e
							},
						} {
							if err := stage(c); !errors.Is(err, ErrUnsupported) {
								t.Fatal("private publication veto", err)
							}
							saved := c.state.referenceFunctions
							c.state.referenceFunctions = false
							if err := stage(c); !errors.Is(err, ErrUnsupported) {
								t.Fatal("per-function publication veto", err)
							}
							c.state.referenceFunctions = saved
							for i := range c.functions {
								c.functions[i].mode.referenceFunctions = false
							}
							if err := stage(c); !errors.Is(err, ErrUnsupported) {
								t.Fatal("state-only publication veto", err)
							}
							for i := range c.functions {
								c.functions[i].mode.referenceFunctions = true
							}
						}
						opts := closedReferenceOptions(tails)
						if image, err := CompileWithOptions(nil, opts); image != nil || !errors.Is(err, ErrUnsupported) {
							t.Fatal("private scalar gate after decoding", err)
						}
						if image, err := compileSWARModule(nil, closedReferenceMode(tails)); image != nil || !errors.Is(err, ErrUnsupported) {
							t.Fatal("private SWAR gate after decoding", err)
						}
						if image, err := compileSWARModule(closedReferenceSource(vector, tails, indexed, nullable), closedReferenceMode(tails)); image != nil || !errors.Is(err, ErrUnsupported) {
							t.Fatal("private SWAR byte-source compiler", err)
						}
						if image, err := CompileModuleWith(m, opts); image != nil || !errors.Is(err, ErrUnsupported) {
							t.Fatal("private public compiler option", err)
						}
						if image, err := CompileWithOptions(closedReferenceSource(vector, tails, indexed, nullable), opts); image != nil || !errors.Is(err, ErrUnsupported) {
							t.Fatal("private byte-source compiler", err)
						}
						opts.referenceFunctions = false
						if image, err := CompileModuleWith(m, opts); image != nil || !errors.Is(err, ErrUnsupported) {
							t.Fatal("ordinary source callable gate", err)
						}
					})
				}
			}
		}
	}
}

func TestReferenceFunctionModuleClosedEscapeAndShape(t *testing.T) {
	cases := []struct {
		name   string
		change func(*wasm.Module)
	}{
		{"bad-export", func(m *wasm.Module) { m.Exports[0].Index.Index = 5 }},
		{"wrong-export-kind", func(m *wasm.Module) { m.Exports[0].Index.Kind = wasm.ExternGlobal }},
		{"reference-declaration", func(m *wasm.Module) { m.Elements[0].Kind.Funcs[0] = 2 }},
		{"bad-declaration", func(m *wasm.Module) { m.Elements[0].Kind.Funcs[0] = 5 }},
		{"active-element", func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemActive }},
		{"element-offset", func(m *wasm.Module) { m.Elements[0].Mode.Offset.BodyBytes = []byte{0x41, 0, 0x0b} }},
		{"element-expressions", func(m *wasm.Module) { m.Elements[0].Kind.Exprs = []wasm.Expr{{BodyBytes: []byte{0xd2, 1, 0x0b}}} }},
		{"element-ref", func(m *wasm.Module) { m.Elements[0].Kind.Ref = wasm.FuncRef.Ref() }},
		{"element-table", func(m *wasm.Module) { m.Elements[0].Mode.Table = 1 }},
		{"import", func(m *wasm.Module) { m.Imports = []wasm.Import{{Type: wasm.NewFuncExternType(wasm.TypeIdx{})}} }},
		{"compact-import", func(m *wasm.Module) { m.UsesCompactImports = true }},
		{"table", func(m *wasm.Module) { m.Tables = []wasm.Table{{}} }},
		{"shared-memory", func(m *wasm.Module) { m.Memories = []wasm.MemType{{Shared: true}} }},
		{"global", func(m *wasm.Module) { m.Globals = []wasm.Global{{}} }},
		{"data", func(m *wasm.Module) { m.Data = []wasm.Data{{}} }},
		{"bad-data-count", func(m *wasm.Module) { count := uint32(1); m.DataCount = &count }},
		{"start", func(m *wasm.Module) { m.Start = new(wasm.FuncIdx) }},
		{"tag", func(m *wasm.Module) { m.Tags = []wasm.TagType{{}} }},
		{"string", func(m *wasm.Module) { m.StringRefs = [][]byte{{}} }},
		{"empty-hints", func(m *wasm.Module) { m.BranchHints = []wasm.FuncBranchHints{} }},
		{"hint-custom", func(m *wasm.Module) { m.Customs = []wasm.CustomSec{{Name: "metadata.code.branch_hint"}} }},
		{"bad-binding", func(m *wasm.Module) { m.FuncTypes[2].Index = 99 }},
		{"recursive-binding", func(m *wasm.Module) { m.FuncTypes[2].Rec = true }},
		{"dual-body", func(m *wasm.Module) { m.Code[0].Body.BodyBytes = []byte{0x0b} }},
		{"structured-body", func(m *wasm.Module) { m.Code[0].Body.Instrs = []wasm.Instruction{{Kind: wasm.InstrNop}} }},
		{"inactive-fields", func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Fields = []wasm.FieldType{{}} }},
		{"inactive-array", func(m *wasm.Module) {
			m.Types[0].SubTypes[0].Comp.Array = wasm.NewFieldType(wasm.StorageVal(wasm.I64), wasm.Var)
		}},
	}
	for _, tc := range cases {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-vector%t", tc.name, vector), func(t *testing.T) {
				m := closedReferenceDecode(t, vector, false, false, true)
				tc.change(m)
				if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
					t.Fatal("closed shape/escape admitted", err)
				}
				if c, err := closedReferenceCompile(m, vector, false); c != nil || !errors.Is(err, ErrUnsupported) {
					t.Fatal("closed adapter staged shape/escape", err)
				}
			})
		}
	}
	for _, op := range []byte{0x11, 0x13, 0x14, 0x15, 0xd2} {
		for _, dead := range []bool{false, true} {
			for _, vector := range []bool{false, true} {
				t.Run(fmt.Sprintf("opcode%x-dead%t-vector%t", op, dead, vector), func(t *testing.T) {
					m := closedReferenceDecode(t, vector, false, false, true)
					body := []byte{op, 2}
					if op == 0x11 || op == 0x13 {
						body = append(body, 0)
					}
					if dead {
						body = append([]byte{0}, body...)
					}
					m.Code[1].BodyBytes = append(body, 0x0b)
					if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
						t.Fatal("closed dead/live escape", err)
					}
				})
			}
		}
	}
}

func TestReferenceFunctionTableFreePassiveDeclaration(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			m := closedReferenceDecode(t, vector, false, false, true)
			m.Elements[0].Mode.Kind = wasm.ElemPassive
			if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
				t.Fatal("table-free legacy passive declaration", err)
			}
			if c, err := closedReferenceCompile(m, vector, false); err != nil || c == nil {
				t.Fatal("table-free legacy passive production", err)
			}
			for name, mutate := range map[string]func(*wasm.Module){
				"empty": func(m *wasm.Module) { m.Elements[0].Kind.Funcs = nil },
				"private-target": func(m *wasm.Module) {
					m.Elements[0].Kind.Funcs[0] = 2
				},
				"typed-expression": func(m *wasm.Module) {
					m.Elements[0].Kind = wasm.ElemKind{Kind: wasm.ElemTypedExprs, Ref: wasm.FuncRef.Ref(), Exprs: []wasm.Expr{{BodyBytes: []byte{0xd2, 1, 0x0b}}}}
				},
			} {
				t.Run(name, func(t *testing.T) {
					bad := closedReferenceDecode(t, vector, false, false, true)
					bad.Elements[0].Mode.Kind = wasm.ElemPassive
					mutate(bad)
					if err := admitClosedReferenceFunctionModule(bad, vector); !errors.Is(err, ErrUnsupported) {
						t.Fatal("unqualified table-free passive declaration", err)
					}
				})
			}
		})
	}
	for _, remaining := range []int{20, 19} {
		t.Run(fmt.Sprintf("bytes%d", remaining), func(t *testing.T) {
			m := &wasm.Module{Elements: []wasm.Elem{{
				Mode: wasm.ElemMode{Kind: wasm.ElemPassive},
				Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}},
			}}}
			left := remaining
			err := admitClosedReferenceElements(m, &left, func(index uint32) bool { return index == 0 })
			if (err == nil) != (remaining == 20) || err == nil && left != 0 {
				t.Fatal("table-free passive descriptor/payload byte boundary", err, left)
			}
		})
	}
}

func TestReferenceFunctionModuleImmediateAndAggregateBounds(t *testing.T) {
	for _, vector := range []bool{false, true} {
		m := closedReferenceDecode(t, vector, false, false, true)
		// These bytes include ref.func/call-like values solely inside a raw f64
		// immediate. The classifier must not interpret them as instructions.
		m.Code[1].BodyBytes = []byte{0x44, 0xd2, 2, 0x14, 0x15, 0x11, 0x13, 0, 0, 0x1a, 0x0b}
		if c, err := closedReferenceCompile(m, vector, false); c == nil || err != nil {
			t.Fatal("immediate bytes classified as opcodes", vector, err)
		}
		for _, body := range [][]byte{{0, 0x1c, 2, 0x7f, 0x7f, 0x0b}, append([]byte{0, 0x0e}, wasmtest.ULEB(maxTableEntries+1)...), append(bytes.Repeat([]byte{1}, integerBudget), 0x0b)} {
			m.Code[1].BodyBytes = body
			if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
				t.Fatal("immediate/instruction bound", vector, err)
			}
		}
		m = closedReferenceDecode(t, vector, false, false, true)
		used := 0
		for _, g := range m.Types {
			ft := g.SubTypes[0].Comp
			for _, list := range [][]wasm.ValType{ft.Params, ft.Results} {
				for _, v := range list {
					used++
					if v.Kind() == wasm.ValRef {
						used += 5
					}
				}
			}
		}
		for _, fn := range m.Code {
			used += len(fn.BodyBytes) + int(fn.LocalDeclBytes)
		}
		for _, e := range m.Elements {
			used += 4 * len(e.Kind.Funcs)
		}
		for _, ex := range m.Exports {
			used += len(ex.Name)
		}
		m.Customs = []wasm.CustomSec{{Name: "opaque", Data: make([]byte, MaxModuleBytes-used-len("opaque"))}}
		if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
			t.Fatal("exact aggregate bound", vector, err)
		}
		m.Customs[0].Data = append(m.Customs[0].Data, 0)
		if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
			t.Fatal("over aggregate bound", vector, err)
		}
	}
}

func TestReferenceFunctionModuleDirectoryAndLocalBounds(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tc := range []struct {
			name   string
			limit  int
			resize func(*wasm.Module, int)
		}{
			{"functions", MaxFunctions, func(m *wasm.Module, n int) {
				for len(m.Code) < n {
					m.FuncTypes = append(m.FuncTypes, wasm.TypeIdx{})
					m.Code = append(m.Code, wasm.Func{BodyBytes: []byte{0x0b}, LocalDeclBytes: 1})
				}
			}},
			{"types", MaxFunctions, func(m *wasm.Module, n int) {
				for len(m.Types) < n {
					m.Types = append(m.Types, m.Types[0])
				}
			}},
			{"exports", MaxFunctions, func(m *wasm.Module, n int) {
				m.Exports = nil
				for i := 0; i < n; i++ {
					m.Exports = append(m.Exports, wasm.Export{Name: fmt.Sprint(i), Index: wasm.ExternIdx{Kind: wasm.ExternFunc, Index: 0}})
				}
			}},
			{"customs", MaxFunctions, func(m *wasm.Module, n int) { m.Customs = make([]wasm.CustomSec, n) }},
			{"segments", MaxOwnedElementSegments, func(m *wasm.Module, n int) {
				for len(m.Elements) < n {
					m.Elements = append(m.Elements, wasm.Elem{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}})
				}
			}},
			{"elements", MaxOwnedTableEntries, func(m *wasm.Module, n int) {
				m.Elements[0].Kind.Funcs = make([]wasm.FuncIdx, n)
				for i := range m.Elements[0].Kind.Funcs {
					m.Elements[0].Kind.Funcs[i] = 1
				}
				m.Elements[0].Kind.Funcs[n-1] = 4
			}},
		} {
			for _, n := range []int{tc.limit, tc.limit + 1} {
				t.Run(fmt.Sprintf("%s-n%d-vector%t", tc.name, n, vector), func(t *testing.T) {
					m := closedReferenceDecode(t, vector, false, false, true)
					tc.resize(m, n)
					if err := admitClosedReferenceFunctionModule(m, vector); (err == nil) != (n == tc.limit) {
						t.Fatal("closed directory bound", err)
					}
				})
			}
		}
		limit := 256
		if vector {
			limit = 128
		}
		for _, n := range []int{limit, limit + 1} {
			m := closedReferenceDecode(t, vector, false, false, true)
			m.Code[3].Locals.Runs = []wasm.LocalRun{{Count: uint32(n - 3), Type: wasm.I64}}
			if err := admitClosedReferenceFunctionModule(m, vector); (err == nil) != (n == limit) {
				t.Fatal("closed local bound", vector, n, err)
			}
		}
		m := closedReferenceDecode(t, vector, false, false, true)
		m.Code[3].Locals.Runs = make([]wasm.LocalRun, limit+1)
		if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
			t.Fatal("zero-run count bound", err)
		}
		body := bytes.Repeat([]byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x1a}, 6553)
		body = append(body, 1, 1, 1, 1, 1, 0x0b)
		m = closedReferenceDecode(t, vector, false, false, true)
		m.Code[1].BodyBytes = body
		if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
			t.Fatal("exact body bytes", err)
		}
		m.Code[1].BodyBytes = append([]byte{1}, body...)
		if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
			t.Fatal("body byte bound", err)
		}
		for _, depth := range []int{maxControlNesting, maxControlNesting + 1} {
			m = closedReferenceDecode(t, vector, false, false, true)
			m.Code[1].BodyBytes = append(bytes.Repeat([]byte{2, 0x40}, depth), bytes.Repeat([]byte{0x0b}, depth+1)...)
			if err := admitClosedReferenceFunctionModule(m, vector); (err == nil) != (depth == maxControlNesting) {
				t.Fatal("closed control depth", vector, depth, err)
			}
		}
	}
}

func TestReferenceFunctionModuleModeAndValidationGates(t *testing.T) {
	m := closedReferenceDecode(t, false, false, false, true)
	for _, change := range []func(*CompileOptions){
		func(o *CompileOptions) { o.production = false }, func(o *CompileOptions) { o.publicStarts = false }, func(o *CompileOptions) { o.Interruptible = false }, func(o *CompileOptions) { o.ForeignTableCalls = false }, func(o *CompileOptions) { o.InvocationContext = false }, func(o *CompileOptions) { o.importTailCalls = true }, func(o *CompileOptions) { o.dynamicTailCalls = true }, func(o *CompileOptions) { o.Profile = true }, func(o *CompileOptions) { o.SourceMaps = true }, func(o *CompileOptions) { o.UnwindMaps = true },
	} {
		opts := closedReferenceOptions(false)
		change(&opts)
		if c, err := compileDecodedModule(m, scalarRegisters[:], opts); c != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("closed scalar mode", err)
		}
	}
	vm := closedReferenceDecode(t, true, false, false, true)
	for _, change := range []func(*emissionMode){
		func(o *emissionMode) { o.production = false }, func(o *emissionMode) { o.interruptible = false }, func(o *emissionMode) { o.foreignTableCalls = false }, func(o *emissionMode) { o.invocationContext = false }, func(o *emissionMode) { o.importTailCalls = true }, func(o *emissionMode) { o.dynamicTailCalls = true },
	} {
		mode := closedReferenceMode(false)
		change(&mode)
		if c, err := compileSWARDecodedModuleProfile(vm, mode, swarModuleTables, swarStateProfile{publicStarts: true, exports: true, publicInitializers: true, importedTable: true}); c != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("closed SWAR mode", err)
		}
	}
	if profileEnabled {
		mode := closedReferenceMode(false)
		mode.profileMode = newProfileModule(true, true, len(vm.Code)).baseProfileMode()
		if c, err := compileSWARDecodedModuleProfile(vm, mode, swarModuleTables, swarStateProfile{publicStarts: true, exports: true, publicInitializers: true, importedTable: true}); c != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("closed SWAR profile mode", err)
		}
	}
	for _, vector := range []bool{false, true} {
		m := closedReferenceDecode(t, vector, false, false, true)
		m.Code[3].BodyBytes = []byte{0x0b}
		if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
			t.Fatal("shape should precede full validation", err)
		}
		var invalid *wasm.ValidationError
		if c, err := closedReferenceCompile(m, vector, false); c != nil || !errors.As(err, &invalid) || invalid.Code != wasm.ErrTypeMismatch {
			t.Fatal("whole-module frontend validation skipped", err)
		}
	}
}

func TestReferenceFunctionModuleUnderstatedLocalBytes(t *testing.T) {
	for _, vector := range []bool{false, true} {
		m := closedReferenceDecode(t, vector, false, false, true)
		limit := 256
		if vector {
			limit = 128
		}
		runs := make([]wasm.LocalRun, limit)
		// Zero-count runs still occupy declaration metadata. Include reference
		// encodings as well as scalar encodings; both must be charged independently
		// of the deliberately understated source-PC offset.
		for i := range runs {
			runs[i].Type = wasm.I64
			if i%2 == 0 {
				runs[i].Type = wasm.FuncRef
			}
		}
		for range 64 {
			m.FuncTypes = append(m.FuncTypes, wasm.TypeIdx{})
			m.Code = append(m.Code, wasm.Func{BodyBytes: []byte{0x0b}, LocalDeclBytes: 1, Locals: wasm.Locals{Runs: runs}})
		}
		if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
			t.Fatal("hand-built local oracle", err)
		}
		used := 0
		for _, g := range m.Types {
			ft := g.SubTypes[0].Comp
			for _, list := range [][]wasm.ValType{ft.Params, ft.Results} {
				for _, v := range list {
					used++
					if v.Kind() == wasm.ValRef {
						used += 5
					}
				}
			}
		}
		for _, fn := range m.Code {
			localBytes := len(wasmtest.ULEB(uint32(len(fn.Locals.Runs))))
			for _, run := range fn.Locals.Runs {
				localBytes += len(wasmtest.ULEB(run.Count)) + 1
				if run.Type.Kind() == wasm.ValRef {
					localBytes += 5
				}
			}
			used += len(fn.BodyBytes) + max(localBytes, int(fn.LocalDeclBytes))
		}
		for _, e := range m.Elements {
			used += 4 * len(e.Kind.Funcs)
		}
		for _, ex := range m.Exports {
			used += len(ex.Name)
		}
		m.Customs = []wasm.CustomSec{{Name: "opaque", Data: make([]byte, MaxModuleBytes-used-len("opaque"))}}
		if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
			t.Fatal("derived local aggregate exact bound", vector, err)
		}
		m.Customs[0].Data = append(m.Customs[0].Data, 0)
		if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
			t.Fatal("understated local aggregate over bound", vector, err)
		}
	}
}

func TestReferenceFunctionModuleExceptionPreflight(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, depth := range []int{1, maxControlNesting + 1} {
			m := closedReferenceDecode(t, vector, false, false, true)
			// Tag-free try_table with no handlers is valid Wasm. Its control frames
			// must be excluded before the frontend can expand this unsupported family.
			m.Code[1].BodyBytes = append(bytes.Repeat([]byte{0x1f, 0x40, 0}, depth), bytes.Repeat([]byte{0x0b}, depth+1)...)
			if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
				t.Fatal("tag-free exception oracle", err)
			}
			if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
				t.Fatal("exception nesting reached frontend", vector, depth, err)
			}
			if c, err := closedReferenceCompile(m, vector, false); c != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("exception module lowered", err)
			}
		}
		for _, dead := range []bool{false, true} {
			m := closedReferenceDecode(t, vector, false, false, true)
			body := []byte{0x1f, 0x40}
			body = append(body, wasmtest.ULEB(maxTableEntries+1)...)
			body = append(body, bytes.Repeat([]byte{2, 0}, maxTableEntries+1)...)
			body = append(body, 0x0b, 0x0b)
			if dead {
				body = append([]byte{0}, body...)
			}
			m.Code[1].BodyBytes = body
			if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
				t.Fatal("exception handler vector reached classifier", vector, dead, err)
			}
		}
	}
}
