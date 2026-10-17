//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func referenceExpressionDecode(t *testing.T, vector bool) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.ClosedReferenceExpressionElements(vector, true, 2, 0))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReferenceFunctionExpressionElementsSource(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			for _, start := range []int{0, 1, 2} {
				for _, target := range []int{0, 1, 2} {
					t.Run(fmt.Sprintf("v%t-w%t-s%d-target%d", vector, wide, start, target), func(t *testing.T) {
						m, err := wasm.DecodeModule(wasmtest.ClosedReferenceExpressionElements(vector, wide, start, target))
						if err != nil {
							t.Fatal(err)
						}
						if err := wasm.ValidateModuleWithFeaturesAndWorkers(m, wasm.ValidationFeatures{MultiMemory: true}, 1); err != nil {
							t.Fatal("frontend", err)
						}
						o := closedReferenceProductionOptions(true, true)
						o.ImportBindings = hostImportBindings(3)
						image, err := CompileModuleWith(m, o)
						if err != nil || image == nil || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || image.NativeTail == nil {
							t.Fatal("expression profile", err)
						}
					})
				}
			}
		}
		for _, drop := range []bool{false, true} {
			for _, payload := range []int{0, 1, 2} {
				t.Run(fmt.Sprintf("v%t-drop%t-p%d", vector, drop, payload), func(t *testing.T) {
					m, err := wasm.DecodeModule(wasmtest.ClosedReferenceDeclarativeExpressions(vector, drop, payload))
					if err != nil {
						t.Fatal(err)
					}
					if err := wasm.ValidateModuleWithFeaturesAndWorkers(m, wasm.ValidationFeatures{MultiMemory: true}, 1); err != nil {
						t.Fatal("frontend", err)
					}
					image, err := CompileModuleWith(m, closedReferenceProductionOptions(false, false))
					if err != nil || image == nil || !image.NativeReferenceFunctions || image.NativeReferenceCalls || image.NativeReferenceControl || image.NativeTail != nil {
						t.Fatal("tableless untyped declaration", err)
					}
				})
			}
		}
	}
}

func TestReferenceFunctionExpressionElementsConfinement(t *testing.T) {
	mutations := map[string]func(*wasm.Module){
		"private-active":         func(m *wasm.Module) { m.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0xd2, 12, 0x0b} },
		"private-declared":       func(m *wasm.Module) { m.Elements[1].Kind.Exprs[0].BodyBytes = []byte{0xd2, 12, 0x0b} },
		"private-unused-passive": func(m *wasm.Module) { m.Elements[2].Kind.Exprs[0].BodyBytes = []byte{0xd2, 12, 0x0b} },
		"missing":                func(m *wasm.Module) { m.Elements[1].Kind.Exprs[0].BodyBytes = []byte{0xd2, 127, 0x0b} },
		"global":                 func(m *wasm.Module) { m.Elements[2].Kind.Exprs[0].BodyBytes = []byte{0x23, 3, 0x0b} },
		"structured": func(m *wasm.Module) {
			m.Elements[2].Kind.Exprs[0] = wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrRefNull}}}
		},
		"indexed-null": func(m *wasm.Module) { m.Elements[2].Kind.Exprs[0].BodyBytes = []byte{0xd0, 0, 0x0b} },
		"nofunc-null":  func(m *wasm.Module) { m.Elements[2].Kind.Exprs[0].BodyBytes = []byte{0xd0, 0x73, 0x0b} },
		"trailing":     func(m *wasm.Module) { m.Elements[2].Kind.Exprs[0].BodyBytes = []byte{0xd0, 0x70, 0x0b, 0x0b} },
		"extended": func(m *wasm.Module) {
			m.Elements[2].Kind.Exprs[0].BodyBytes = []byte{0xd0, 0x70, 0x1a, 0xd0, 0x70, 0x0b}
		},
		"implicit-ref": func(m *wasm.Module) { m.Elements[0].Kind.Ref = wasm.FuncRef.Ref() },
		"empty-indexed-type": func(m *wasm.Module) {
			m.Elements[1].Kind.Exprs = nil
			m.Elements[1].Kind.Ref = wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false)
		},
		"empty-exact-type": func(m *wasm.Module) {
			m.Elements[1].Kind.Exprs = nil
			m.Elements[1].Kind.Ref = wasm.Ref(true, wasm.FuncRef.Ref().Heap(), true)
		},
		"typed-extern":      func(m *wasm.Module) { m.Elements[1].Kind.Ref = wasm.ExternRef.Ref() },
		"typed-nonnull":     func(m *wasm.Module) { m.Elements[1].Kind.Ref = wasm.Ref(false, wasm.FuncRef.Ref().Heap(), false) },
		"inactive-funcs":    func(m *wasm.Module) { m.Elements[2].Kind.Funcs = []wasm.FuncIdx{0} },
		"inactive-offset":   func(m *wasm.Module) { m.Elements[1].Mode.Offset.BodyBytes = []byte{0x41, 0, 0x0b} },
		"tableless-passive": func(m *wasm.Module) { m.Tables = nil; m.Elements = m.Elements[1:] },
		"tableless-active":  func(m *wasm.Module) { m.Tables = nil },
	}
	for _, vector := range []bool{false, true} {
		for name, mutate := range mutations {
			t.Run(fmt.Sprintf("v%t-%s", vector, name), func(t *testing.T) {
				m := referenceExpressionDecode(t, vector)
				mutate(m)
				o := closedReferenceProductionOptions(true, true)
				o.ImportBindings = hostImportBindings(3)
				switch name {
				case "empty-indexed-type", "nofunc-null", "implicit-ref", "typed-nonnull", "private-active", "private-unused-passive", "private-declared":
					checkPublicPrimitiveModule(t, m, o)
					return
				}
				pressure := 0
				o.MemoryPressure = func() { pressure++ }
				if image, err := CompileModuleWith(m, o); image != nil || err == nil || pressure != 0 {
					t.Fatal("unproven literal published", err, pressure)
				}
			})
		}
		for name, body := range map[string][]byte{"dead-OOB-drop": {0, 0xfc, 13, 0x80, 2, 0x0b}, "live-OOB-drop": {0xfc, 13, 0x80, 2, 0x41, 42, 0x0b}, "dead-table-init": {0, 0xfc, 12, 0, 0, 0x0b}, "live-table-init": {0x41, 0, 0x41, 0, 0x41, 0, 0xfc, 12, 0, 0, 0x41, 42, 0x0b}} {
			t.Run(fmt.Sprintf("v%t-%s", vector, name), func(t *testing.T) {
				m, err := wasm.DecodeModule(wasmtest.ClosedReferenceDeclarativeExpressions(vector, true, 1))
				if err != nil {
					t.Fatal(err)
				}
				m.Code[1].BodyBytes = body
				if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
					t.Fatal("tableless/dead bound", err)
				}
			})
		}
	}
}

func TestReferenceFunctionExpressionElementsBounds(t *testing.T) {
	for _, n := range []int{MaxOwnedTableEntries, MaxOwnedTableEntries + 1} {
		for _, mode := range []wasm.ElemModeKind{wasm.ElemActive, wasm.ElemPassive, wasm.ElemDeclarative} {
			t.Run(fmt.Sprintf("values%d-mode%d", n, mode), func(t *testing.T) {
				m := referenceExpressionDecode(t, false)
				m.Elements = []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: make([]wasm.FuncIdx, 512)}}, {Mode: wasm.ElemMode{Kind: mode}, Kind: wasm.ElemKind{Kind: wasm.ElemTypedExprs, Ref: wasm.FuncRef.Ref(), Exprs: make([]wasm.Expr, n-512)}}}
				m.Elements = append(m.Elements, wasm.Elem{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}, Kind: wasm.ElemKind{Kind: wasm.ElemFuncs}})
				if mode == wasm.ElemActive {
					m.Elements[1].Mode.Offset.BodyBytes = []byte{0x41, 0, 0x0b}
				}
				for i := range m.Elements[1].Kind.Exprs {
					m.Elements[1].Kind.Exprs[i].BodyBytes = []byte{0xd0, 0x70, 0x0b}
				}
				if err := admitClosedReferenceFunctionModule(m, false); (err == nil) != (n == MaxOwnedTableEntries) {
					t.Fatal("combined legacy/expression bound", err)
				}
			})
		}
	}
	// Canonical header16 + explicit type1 + two literal bodies6. The source
	// admission separately caps raw wire bytes, including alternate encodings.
	for _, hasTable := range []bool{false, true} {
		for _, extra := range []int{0, 1} {
			t.Run(fmt.Sprintf("table%t-bytes+%d", hasTable, extra), func(t *testing.T) {
				m := &wasm.Module{Elements: []wasm.Elem{{Mode: wasm.ElemMode{Kind: wasm.ElemDeclarative}, Kind: wasm.ElemKind{Kind: wasm.ElemTypedExprs, Ref: wasm.FuncRef.Ref(), Exprs: []wasm.Expr{{BodyBytes: []byte{0xd0, 0x70, 0x0b}}, {BodyBytes: []byte{0xd2, 0, 0x0b}}}}}}}
				remaining := 23 - extra
				if hasTable {
					m.Tables = []wasm.Table{{Type: wasm.TableType{Ref: wasm.FuncRef.Ref()}}}
					remaining += 21
					if err := admitClosedReferenceOwnedTableState(m, &remaining); err != nil {
						t.Fatal(err)
					}
				}
				if err := admitClosedReferenceElements(m, &remaining, func(index uint32) bool { return index == 0 }); (err == nil) != (extra == 0) || extra == 0 && remaining != 0 {
					t.Fatal("header/literal aggregate", err, remaining)
				}
			})
		}
	}
}

func TestReferenceFunctionExpressionElementsAggregate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, extra := range []int{0, 1} {
			t.Run(fmt.Sprintf("v%t-extra%d", vector, extra), func(t *testing.T) {
				m, err := wasm.DecodeModule(wasmtest.ClosedReferenceDeclarativeExpressions(vector, false, 1))
				if err != nil {
					t.Fatal(err)
				}
				// Carriers8 + bodies11 + locals3 + expression header/type/body20
				// + export3 + passive data header4 =49. SWAR adds6.
				fixed := 49
				if vector {
					fixed += 6
				}
				m.Data = []wasm.Data{{Mode: wasm.DataMode{Kind: wasm.DataPassive}, Init: make([]byte, MaxModuleBytes-fixed+extra)}}
				if err := admitClosedReferenceFunctionModule(m, vector); (err == nil) != (extra == 0) {
					t.Fatal("complete expression aggregate", err)
				}
			})
		}
	}
}
