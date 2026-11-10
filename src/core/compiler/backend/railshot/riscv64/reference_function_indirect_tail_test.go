//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestReferenceFunctionIndirectTailUntyped(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			m, err := wasm.DecodeModule(wasmtest.ClosedReferenceBareIndirectTails(vector))
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModuleWithWorkers(m, 1); err != nil {
				t.Fatal("frontend", err)
			}
			o := closedReferenceProductionOptions(true, false)
			image, err := CompileModuleWith(m, o)
			if err != nil || image == nil || !image.NativeReferenceFunctions || image.NativeReferenceCalls || image.NativeReferenceControl || image.NativeTail == nil || len(image.NativeTail.Plans) != 2 {
				t.Fatal("untyped private indirect admission", err)
			}
			for _, p := range image.NativeTail.Plans {
				if p.Kind != enc.NativeTailIndirect || p.ImportIndex != 0 {
					t.Fatal("canonical indirect plan", p)
				}
			}
			for _, body := range [][]byte{{0, 0x15, 0, 0x0b}, {0x20, 1, 0xd2, 0, 0x15, 0, 0x0b}, {0, 0xd0, 0, 0x1a, 0x20, 1, 0x20, 2, 0x13, 0, 0, 0x0b}} {
				m.Code[1].BodyBytes = body
				pressure := 0
				o.MemoryPressure = func() { pressure++ }
				if i, err := CompileModuleWith(m, o); i != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("typed permission manufactured", err, pressure)
				}
			}
		})
	}
}

func TestReferenceFunctionIndirectTailFloors(t *testing.T) {
	numeric := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	private := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}, Results: []wasm.ValType{wasm.I32}}
	table := wasm.TableType{Ref: wasm.FuncRef.Ref(), Limits: wasm.Limits{Min: 1, Max: 1, HasMax: true}}
	state := &moduleState{production: true, referenceFunctions: true, importReferences: true, ownedTable: true, foreignTableCalls: true, localTailCalls: true, importTailCalls: true, dynamicTailCalls: true, functions: []functionSpec{{signature: private}}, types: []functionSpec{{signature: numeric, params: []bool{false}, results: []bool{false}}, {signature: private}}}
	for _, body := range [][]byte{{0, 0x15, 0, 0x0b}, {0x41, 1, 0xd2, 0, 0x15, 0, 0x0b}, {0, 0xd0, 0, 0x1a, 0x13, 0, 0, 0x0b}} {
		fn := wasm.Func{BodyBytes: body}
		_, scalar := walkInteger(&fn, private, state, nil)
		_, swar := walkSWARPairsWithReferenceFunctionMode(&fn, private, memoryProfile{}, []*wasm.CompType{numeric, private}, []*wasm.CompType{private}, nil, 0, &table, []uint64{1, 2}, 0, 0, true, false, true, false, true, true, false, true, nil)
		if !errors.Is(scalar, ErrUnsupported) || !errors.Is(swar, ErrUnsupported) {
			t.Fatal("untyped low-level walker manufactured typed authority", scalar, swar)
		}
	}
	for _, tc := range []struct {
		name   string
		prefix []byte
		valid  bool
	}{
		{"live", []byte{0x41, 3, 0x41, 0}, true}, {"explicit-dead", []byte{0}, true}, {"unknown", []byte{0, 0x1b}, true},
		{"wide-index", []byte{0, 0x42, 0}, false}, {"reference-index", []byte{0, 0xd0, 0x70}, false}, {"wide-argument", []byte{0, 0x42, 0, 0x41, 0}, false}, {"reference-argument", []byte{0, 0xd0, 0x70, 0x41, 0}, false},
		{"inherited-floor", []byte{0, 2, 0x40}, false}, {"explicit-nested", []byte{0, 2, 0x40, 0}, true}, {"live-underflow", []byte{0x41, 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := append(slices.Clone(tc.prefix), 0x13, 0, 0, 0x0b)
			if tc.name == "inherited-floor" || tc.name == "explicit-nested" {
				body = append(body, 0x0b)
			}
			fn := wasm.Func{BodyBytes: body}
			m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: *numeric}}}, {SubTypes: []wasm.SubType{{Final: true, Comp: *private}}}}, FuncTypes: []wasm.TypeIdx{{Index: 1}}, Code: []wasm.Func{fn}, Tables: []wasm.Table{{Type: table}}}
			front := wasm.ValidateModuleWithWorkers(m, 1)
			if (front == nil) != tc.valid {
				t.Fatal("frontend oracle", front)
			}
			_, scalar := walkInteger(&fn, private, state, nil)
			_, swar := walkSWARPairsWithReferenceFunctionMode(&fn, private, memoryProfile{}, []*wasm.CompType{numeric, private}, []*wasm.CompType{private}, nil, 0, &table, []uint64{1, 2}, 0, 0, true, false, true, false, true, true, false, true, nil)
			if (scalar == nil) != tc.valid || (swar == nil) != tc.valid {
				t.Fatal("private indirect floor", scalar, swar)
			}
		})
	}
}

func TestReferenceFunctionIndirectTailKindWitnesses(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, kind := range []string{"mixed", "live-indirect-dead-reference", "live-reference-dead-indirect", "dead-only", "private", "missing", "results", "table"} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, kind), func(t *testing.T) {
				data := wasmtest.ClosedReferenceProperIndirectTails(vector, true, 2, 0)
				if kind == "live-reference-dead-indirect" {
					data = wasmtest.ClosedReferenceProperTails(vector, true, 2, 0)
				}
				m, err := wasm.DecodeModule(data)
				if err != nil {
					t.Fatal(err)
				}
				body := slices.Clone(m.Code[9].BodyBytes)
				switch kind {
				case "mixed":
					m.Code[8].BodyBytes = []byte{0x20, 0, 0xd2, 0, 0x15, 0, 0x0b}
				case "live-indirect-dead-reference":
					body = append(body[:len(body)-1], 0x15, 0, 0x0b)
				case "live-reference-dead-indirect":
					body = append(body[:len(body)-1], 0x13, 0, 0, 0x0b)
				case "dead-only":
					body = []byte{0, 0x13, 0, 0, 0x0b}
				case "private":
					body = []byte{0, 0x13, 6, 0, 0x0b}
				case "missing":
					body = []byte{0, 0x13, 127, 0, 0x0b}
				case "results":
					body = []byte{0, 0x13, 1, 0, 0x0b}
				case "table":
					body = []byte{0, 0x13, 0, 1, 0x0b}
				}
				m.Code[9].BodyBytes = body
				o := closedReferenceProductionOptions(true, true)
				o.ImportBindings = hostImportBindings(3)
				pressure := 0
				o.MemoryPressure = func() { pressure++ }
				image, err := CompileModuleWith(m, o)
				if kind == "mixed" {
					if image == nil || err != nil || len(image.NativeTail.Plans) != 3 {
						t.Fatal("three kinds", err)
					}
					var mask uint32
					for _, p := range image.NativeTail.Plans {
						mask |= 1 << p.Kind
					}
					if mask != 7 {
						t.Fatal("mixed kinds", mask)
					}
				} else if kind == "live-indirect-dead-reference" || kind == "live-reference-dead-indirect" || kind == "dead-only" || kind == "private" {
					if image == nil || err != nil || pressure != 1 || image.CodeImage != nil || image.NativeTail == nil {
						t.Fatal("valid public tail witness rejected", err, pressure)
					}
				} else if image != nil || err == nil || pressure != 0 {
					t.Fatal("unproven kind/type staged", err, pressure)
				}
			})
		}
	}
}

func TestReferenceFunctionIndirectTailFinalized(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c, m, o := referenceProperTailCompile(t, vector, true)
		if image, err := stageClosedReferenceProductionModule(c, o, m, vector); image == nil || err != nil {
			t.Fatal("exact private indirect stage", err)
		}
		for _, name := range []string{"source-table", "source-type", "reference-unfrozen", "home-overlap", "kind", "witness", "mode", "tableless", "inactive-import", "missing-finalization", "frame"} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				c, m, o := referenceProperTailCompile(t, vector, true)
				p := &(*o.tailPlans)[1]
				switch name {
				case "source-table":
					m.Code[9].BodyBytes[p.sourcePC-m.Code[9].LocalDeclBytes+2] = 1
				case "source-type":
					m.Code[9].BodyBytes[p.sourcePC-m.Code[9].LocalDeclBytes+1] = 1
				case "reference-unfrozen":
					candidate := p.referenceBase + 8
					if candidate > c.functions[9].FrameBytes-8 {
						candidate = p.referenceBase - 8
					}
					if candidate < p.sourceBase+8 {
						t.Fatal("no alternate bounded home")
					}
					p.referenceBase = candidate
				case "home-overlap":
					p.referenceBase = p.sourceBase
					c.finalizedReferenceImportTails = slices.Clone(*o.tailPlans)
				case "kind":
					p.kind = enc.NativeTailReference
					c.finalizedReferenceImportTails = slices.Clone(*o.tailPlans)
				case "witness":
					o.indirectTailInstructions = false
				case "mode":
					c.functions[9].mode.dynamicTailCalls = false
				case "tableless":
					m.Tables = nil
				case "inactive-import":
					p.importIndex = 1
					c.finalizedReferenceImportTails = slices.Clone(*o.tailPlans)
				case "missing-finalization":
					c.referenceImportTailsFinalized = false
				case "frame":
					c.functions[9].FrameBytes += 16
				}
				pressure := 0
				o.MemoryPressure = func() { pressure++ }
				if image, err := stageClosedReferenceProductionModule(c, o, m, vector); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("unproven exact indirect plan", err, pressure)
				}
			})
		}
	}
}
