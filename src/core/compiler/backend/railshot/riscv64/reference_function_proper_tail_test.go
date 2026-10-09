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

func referenceProperTailDecode(t *testing.T, vector bool) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.ClosedReferenceProperTails(vector, true, 2, 0))
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestReferenceFunctionProperTailSource(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			m := referenceProperTailDecode(t, vector)
			o := closedReferenceProductionOptions(true, true)
			o.ImportBindings = hostImportBindings(3)
			image, err := CompileModuleWith(m, o)
			if image == nil || err != nil || image.NativeTail == nil || len(image.NativeTail.Plans) != 2 || image.NativeTail.Plans[0].Kind != enc.NativeTailDirectImport || image.NativeTail.Plans[1].Kind != enc.NativeTailReference || image.NativeTail.Plans[1].FunctionIndex != 12 {
				t.Fatal("mixed private proper tails", err)
			}
			for _, field := range []string{"tail", "typed", "foreign", "invocation", "interruptible", "private"} {
				broken := o
				switch field {
				case "tail":
					broken.NativeTailCalls = false
				case "typed":
					broken.NativeReferenceCalls = false
				case "foreign":
					broken.ForeignTableCalls = false
				case "invocation":
					broken.InvocationContext = false
				case "interruptible":
					broken.Interruptible = false
				case "private":
					broken.NativeReferenceFunctions = false
				}
				pressure := 0
				broken.MemoryPressure = func() { pressure++ }
				if image, err := CompileModuleWith(m, broken); image != nil || err == nil || pressure != 0 {
					t.Fatal("missing protocol staged", field, err, pressure)
				}
			}
		})
	}
}

func TestReferenceFunctionProperTailFloors(t *testing.T) {
	numeric := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	private := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}, Results: []wasm.ValType{wasm.I32}}
	state := &moduleState{production: true, referenceFunctions: true, referenceCalls: true, importReferences: true, foreignTableCalls: true, localTailCalls: true, importTailCalls: true, dynamicTailCalls: true, functions: []functionSpec{{signature: private}}, types: []functionSpec{{signature: numeric, params: []bool{false}, results: []bool{false}}}}
	for _, tc := range []struct {
		name   string
		prefix []byte
		valid  bool
	}{
		{"live", []byte{0x41, 1, 0xd0, 0}, true}, {"explicit-dead", []byte{0}, true}, {"unknown", []byte{0, 0x1b}, true},
		{"wrong-target", []byte{0, 0x41, 1}, false}, {"wide-argument", []byte{0, 0x42, 1, 0xd0, 0}, false}, {"reference-argument", []byte{0, 0xd0, 0x70, 0xd0, 0}, false},
		{"inherited-floor", []byte{0, 2, 0x40}, false}, {"explicit-nested", []byte{0, 2, 0x40, 0}, true}, {"live-underflow", []byte{0xd0, 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := append(slices.Clone(tc.prefix), 0x15, 0, 0x0b)
			if tc.name == "inherited-floor" || tc.name == "explicit-nested" {
				body = append(body, 0x0b)
			}
			fn := wasm.Func{BodyBytes: body}
			m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: *numeric}}}, {SubTypes: []wasm.SubType{{Final: true, Comp: *private}}}}, FuncTypes: []wasm.TypeIdx{{Index: 1}}, Code: []wasm.Func{fn}}
			front := wasm.ValidateModuleWithWorkers(m, 1)
			if (front == nil) != tc.valid {
				t.Fatal("frontend oracle", front)
			}
			_, scalar := walkInteger(&fn, private, state, nil)
			_, swar := walkSWARPairsWithReferenceFunctionMode(&fn, private, memoryProfile{}, []*wasm.CompType{numeric, private}, []*wasm.CompType{private}, nil, 0, nil, nil, 0, 0, true, false, true, false, true, true, true, true, nil)
			if (scalar == nil) != tc.valid || (swar == nil) != tc.valid {
				t.Fatal("proper tail floor", scalar, swar, front)
			}
		})
	}
}

func TestReferenceFunctionProperTailDeadWitness(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, kind := range []string{"live-and-dead", "dead-only", "private-live", "private-dead", "unbound-live", "unbound-dead", "result-live", "result-dead"} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, kind), func(t *testing.T) {
				m := referenceProperTailDecode(t, vector)
				body := slices.Clone(m.Code[9].BodyBytes)
				if kind == "live-and-dead" {
					body = append(body[:len(body)-1], 0x15, 0, 0x0b)
				} else {
					target := byte(0)
					switch kind {
					case "private-live", "private-dead":
						target = 6
					case "unbound-live", "unbound-dead":
						target = 127
					case "result-live", "result-dead":
						target = 1
					}
					body = []byte{0, 0x15, target, 0x0b}
					if kind == "private-live" || kind == "unbound-live" || kind == "result-live" {
						body = []byte{0x41, 1, 0xd2, 0, 0x15, target, 0x0b}
					}
				}
				m.Code[9].BodyBytes = body
				o := closedReferenceProductionOptions(true, true)
				o.ImportBindings = hostImportBindings(3)
				pressure := 0
				o.MemoryPressure = func() { pressure++ }
				image, err := CompileModuleWith(m, o)
				if kind == "live-and-dead" {
					if image == nil || err != nil || len(image.NativeTail.Plans) != 2 {
						t.Fatal("live/dead witness", err)
					}
				} else if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("dead/type confinement reached allocation", err, pressure)
				}
			})
		}
	}
}

func referenceProperTailCompile(t *testing.T, vector bool, indirect ...bool) (*Compiled, *wasm.Module, CompileOptions) {
	t.Helper()
	m := referenceProperTailDecode(t, vector)
	if len(indirect) != 0 && indirect[0] {
		var err error
		m, err = wasm.DecodeModule(wasmtest.ClosedReferenceProperIndirectTails(vector, true, 2, 0))
		if err != nil {
			t.Fatal(err)
		}
	}
	o := closedReferenceProductionOptions(true, true)
	o.production, o.publicStarts, o.referenceFunctions = true, true, true
	o.localTailCalls, o.importTailCalls, o.dynamicTailCalls = true, true, true
	o.referenceTailInstructions, o.referenceCallInstructions, o.referenceControlInstructions = true, true, true
	if len(indirect) != 0 && indirect[0] {
		o.referenceTailInstructions, o.indirectTailInstructions = false, true
	}
	o.tailModule = m
	var plans []tailImportPlan
	o.tailPlans = &plans
	o.ImportBindings = hostImportBindings(3)
	if err := admitProductionOptions(m, &o, vector); err != nil {
		t.Fatal(err)
	}
	if !vector {
		c, err := compileDecodedModule(m, scalarRegisters[:], o)
		if err != nil {
			t.Fatal(err)
		}
		return c, m, o
	}
	mode := closedReferenceMode(true)
	mode.referenceCalls, mode.importTailCalls, mode.dynamicTailCalls = true, true, true
	mode.tailPlans = o.tailPlans
	i, err := compileSWARDecodedModuleProfile(m, mode, swarModuleTables, swarStateProfile{publicStarts: true, exports: true, publicInitializers: true, importedMemory: true, indexedMemories: true, importedGlobals: true, importedTable: true, functionTableImports: true})
	if err != nil {
		t.Fatal(err)
	}
	return &Compiled{code: i.code, functions: i.functions, state: moduleState{importedFunctions: m.ImportedFuncCount(), referenceFunctions: true, localTailCalls: true, importTailCalls: true, dynamicTailCalls: true}, finalizedReferenceImportTails: i.finalizedReferenceImportTails, referenceImportTailsFinalized: i.referenceImportTailsFinalized}, m, o
}
func TestReferenceFunctionProperTailFinalizedPlans(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c, m, o := referenceProperTailCompile(t, vector)
		if len(*o.tailPlans) != 2 || !slices.Equal(*o.tailPlans, c.finalizedReferenceImportTails) {
			t.Fatal("mixed finalized directory")
		}
		if i, err := stageClosedReferenceProductionModule(c, o, m, vector); i == nil || err != nil {
			t.Fatal("mixed finalized stage", err)
		}
		for name, mutate := range map[string]func(*Compiled, *wasm.Module, *CompileOptions){
			"missing-finalization": func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.referenceImportTailsFinalized = false },
			"nil":                  func(c *Compiled, m *wasm.Module, o *CompileOptions) { o.tailPlans = nil }, "drop-ref": func(c *Compiled, m *wasm.Module, o *CompileOptions) { *o.tailPlans = (*o.tailPlans)[:1] },
			"kind-indirect": func(c *Compiled, m *wasm.Module, o *CompileOptions) { (*o.tailPlans)[1].kind = enc.NativeTailIndirect },
			"reference-overlap": func(c *Compiled, m *wasm.Module, o *CompileOptions) {
				(*o.tailPlans)[1].referenceBase = (*o.tailPlans)[1].sourceBase
				c.finalizedReferenceImportTails = slices.Clone(*o.tailPlans)
			},
			"reference-unfrozen": func(c *Compiled, m *wasm.Module, o *CompileOptions) {
				p := &(*o.tailPlans)[1]
				candidate := p.referenceBase + 8
				if candidate > c.functions[9].FrameBytes-8 {
					candidate = p.referenceBase - 8
				}
				if candidate < p.sourceBase+8 {
					t.Fatal("fixture lacks alternative in-bounds home")
				}
				p.referenceBase = candidate
			},
			"reference-outside": func(c *Compiled, m *wasm.Module, o *CompileOptions) {
				(*o.tailPlans)[1].referenceBase = c.functions[9].FrameBytes
				c.finalizedReferenceImportTails = slices.Clone(*o.tailPlans)
			},
			"type-private": func(c *Compiled, m *wasm.Module, o *CompileOptions) {
				(*o.tailPlans)[1].typeIndex = 6
				c.finalizedReferenceImportTails = slices.Clone(*o.tailPlans)
			},
			"inactive-import": func(c *Compiled, m *wasm.Module, o *CompileOptions) {
				(*o.tailPlans)[1].importIndex = 1
				c.finalizedReferenceImportTails = slices.Clone(*o.tailPlans)
			},
			"pc": func(c *Compiled, m *wasm.Module, o *CompileOptions) {
				(*o.tailPlans)[1].sourcePC++
				c.finalizedReferenceImportTails = slices.Clone(*o.tailPlans)
			},
			"opcode": func(c *Compiled, m *wasm.Module, o *CompileOptions) {
				p := (*o.tailPlans)[1]
				m.Code[9].BodyBytes[p.sourcePC-m.Code[9].LocalDeclBytes] = 0x14
			},
			"frame":      func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.functions[9].FrameBytes += 16 },
			"state-mode": func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.state.dynamicTailCalls = false }, "function-mode": func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.functions[9].mode.dynamicTailCalls = false },
			"options-mode": func(c *Compiled, m *wasm.Module, o *CompileOptions) { o.dynamicTailCalls = false }, "tail-witness": func(c *Compiled, m *wasm.Module, o *CompileOptions) { o.referenceTailInstructions = false },
			"scalar-no-typed": func(c *Compiled, m *wasm.Module, o *CompileOptions) { o.NativeReferenceCalls = false },
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				c, m, o := referenceProperTailCompile(t, vector)
				pressure := 0
				o.MemoryPressure = func() { pressure++ }
				mutate(c, m, &o)
				if i, err := stageClosedReferenceProductionModule(c, o, m, vector); i != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("bad mixed plan staged", err, pressure)
				}
			})
		}
	}
}
