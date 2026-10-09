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

func closedReferenceImportTailCompile(t *testing.T, vector, sites bool, base uint32) (*Compiled, *wasm.Module, CompileOptions) {
	t.Helper()
	data := wasmtest.ClosedReferenceImportTails(vector, true)
	if !sites {
		data = closedReferenceSource(vector, true, false, true)
	}
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	opts := closedReferenceProductionOptions(true, false)
	opts.production, opts.publicStarts, opts.referenceFunctions = true, true, true
	opts.localTailCalls, opts.importTailCalls, opts.tailSiteBase, opts.tailModule = true, true, base, m
	var plans []tailImportPlan
	opts.tailPlans = &plans
	if sites {
		opts.ImportBindings = hostImportBindings(3)
	}
	if err := admitProductionOptions(m, &opts, vector); err != nil {
		t.Fatal(err)
	}
	if !vector {
		c, err := compileDecodedModule(m, scalarRegisters[:], opts)
		if err != nil {
			t.Fatal(err)
		}
		return c, m, opts
	}
	mode := closedReferenceMode(true)
	mode.referenceCalls = false
	mode.importTailCalls, mode.tailPlans, mode.tailSiteBase = true, opts.tailPlans, base
	image, err := compileSWARDecodedModuleProfile(m, mode, swarModuleTables, swarStateProfile{publicStarts: true, exports: true, publicInitializers: true, importedMemory: true, indexedMemories: true, importedGlobals: true, importedTable: true, functionTableImports: true})
	if err != nil {
		t.Fatal(err)
	}
	return &Compiled{code: image.code, functions: image.functions, state: moduleState{importedFunctions: m.ImportedFuncCount(), referenceFunctions: true, localTailCalls: true, importTailCalls: true}, finalizedReferenceImportTails: image.finalizedReferenceImportTails, referenceImportTailsFinalized: image.referenceImportTailsFinalized}, m, opts
}

func TestReferenceFunctionImportedTailSource(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, large := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-large%t", vector, large), func(t *testing.T) {
				m, err := wasm.DecodeModule(wasmtest.ClosedReferenceImportTails(vector, large))
				if err != nil {
					t.Fatal(err)
				}
				if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
					t.Fatal("private direct-tail confinement", err)
				}
				opts := closedReferenceProductionOptions(true, false)
				opts.ImportBindings = hostImportBindings(3)
				image, err := CompileModuleWith(m, opts)
				if err != nil || image == nil || !image.NativeReferenceFunctions || image.NativeTail == nil || len(image.NativeTail.Plans) != 3 {
					t.Fatal("private direct-tail staging", err)
				}
				for _, p := range image.NativeTail.Plans {
					if p.Kind != enc.NativeTailDirectImport || p.TypeIndex != 0 || p.ReferenceBase != 0 || p.FunctionIndex != 5 || p.ImportIndex >= 3 || p.FrameBytes < 48 {
						t.Fatal("private direct-tail symbolic proof", p)
					}
				}
			})
		}
	}
}

func TestReferenceFunctionImportedTailStaging(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, sites := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-sites%t", vector, sites), func(t *testing.T) {
				c, m, opts := closedReferenceImportTailCompile(t, vector, sites, 0)
				if !c.referenceImportTailsFinalized || !slices.Equal(c.finalizedReferenceImportTails, *opts.tailPlans) || len(*opts.tailPlans) != map[bool]int{false: 0, true: 3}[sites] {
					t.Fatal("missing finalized symbolic directory")
				}
				if image, err := stageClosedReferenceProductionModule(c, opts, m, vector); image == nil || err != nil {
					t.Fatal("finalized source staging", err)
				}
				c.referenceImportTailsFinalized = false
				pressure := 0
				opts.MemoryPressure = func() { pressure++ }
				if image, err := stageClosedReferenceProductionModule(c, opts, m, vector); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("missing finalization reached allocation", err, pressure)
				}
			})
		}
		for _, tc := range []struct {
			name   string
			change func(*Compiled, *wasm.Module, *CompileOptions)
		}{
			{"nil-plans", func(c *Compiled, m *wasm.Module, o *CompileOptions) { o.tailPlans = nil }},
			{"drop-all", func(c *Compiled, m *wasm.Module, o *CompileOptions) { *o.tailPlans = nil }},
			{"drop-site", func(c *Compiled, m *wasm.Module, o *CompileOptions) { *o.tailPlans = (*o.tailPlans)[:2] }},
			{"duplicate", func(c *Compiled, m *wasm.Module, o *CompileOptions) {
				*o.tailPlans = append(*o.tailPlans, (*o.tailPlans)[0])
			}},
			{"reorder", func(c *Compiled, m *wasm.Module, o *CompileOptions) { p := *o.tailPlans; p[0], p[1] = p[1], p[0] }},
			{"in-bounds-home", func(c *Compiled, m *wasm.Module, o *CompileOptions) { (*o.tailPlans)[0].sourceBase -= 8 }},
			{"kind", func(c *Compiled, m *wasm.Module, o *CompileOptions) { (*o.tailPlans)[0].kind = enc.NativeTailReference }},
			{"inactive-type", func(c *Compiled, m *wasm.Module, o *CompileOptions) { (*o.tailPlans)[0].typeIndex = 1 }},
			{"inactive-reference", func(c *Compiled, m *wasm.Module, o *CompileOptions) { (*o.tailPlans)[0].referenceBase = 48 }},
			{"defined-target", func(c *Compiled, m *wasm.Module, o *CompileOptions) { (*o.tailPlans)[0].importIndex = 3 }},
			{"wrong-import", func(c *Compiled, m *wasm.Module, o *CompileOptions) { (*o.tailPlans)[0].importIndex = 1 }},
			{"source-ordinal", func(c *Compiled, m *wasm.Module, o *CompileOptions) { (*o.tailPlans)[0].functionIndex = 4 }},
			{"source-pc", func(c *Compiled, m *wasm.Module, o *CompileOptions) { (*o.tailPlans)[0].sourcePC++ }},
			{"site-ordinal", func(c *Compiled, m *wasm.Module, o *CompileOptions) { (*o.tailPlans)[0].site++ }},
			{"stride", func(c *Compiled, m *wasm.Module, o *CompileOptions) { (*o.tailPlans)[0].sourceStride = 32 }},
			{"directory-bound", func(c *Compiled, m *wasm.Module, o *CompileOptions) {
				*o.tailPlans = make([]tailImportPlan, tailRAMaxSites+1)
			}},
			{"nil-source", func(c *Compiled, m *wasm.Module, o *CompileOptions) { o.tailModule = nil }},
			{"other-source", func(c *Compiled, m *wasm.Module, o *CompileOptions) { copy := *m; o.tailModule = &copy }},
			{"disabled-imports", func(c *Compiled, m *wasm.Module, o *CompileOptions) { o.importTailCalls = false }},
			{"disabled-finalized-mode", func(c *Compiled, m *wasm.Module, o *CompileOptions) {
				o.NativeTailCalls, o.localTailCalls, o.importTailCalls = false, false, false
				o.tailPlans, o.tailModule = nil, nil
				c.state.localTailCalls, c.state.importTailCalls = false, false
				for i := range c.functions {
					c.functions[i].mode.localTailCalls, c.functions[i].mode.importTailCalls = false, false
				}
			}},
			{"dynamic-mode", func(c *Compiled, m *wasm.Module, o *CompileOptions) { o.dynamicTailCalls = true }},
			{"state-imports", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.state.importTailCalls = false }},
			{"state-local", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.state.localTailCalls = false }},
			{"function-imports", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.functions[2].mode.importTailCalls = false }},
			{"source-header", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.functions[2].FrameHeaderBytes = 16 }},
			{"source-frame", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.functions[2].FrameBytes += 16 }},
			{"source-entry", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.functions[2].Entry++ }},
			{"source-function", func(c *Compiled, m *wasm.Module, o *CompileOptions) { c.functions[2].mode.functionIndex-- }},
			{"source-opcode", func(c *Compiled, m *wasm.Module, o *CompileOptions) {
				p := (*o.tailPlans)[0]
				m.Code[2].BodyBytes[int(p.sourcePC)-int(m.Code[2].LocalDeclBytes)] = 0x10
			}},
			{"source-result", func(c *Compiled, m *wasm.Module, o *CompileOptions) {
				m.Types[3].SubTypes[0].Comp.Results[0] = wasm.FuncRef
			}},
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, tc.name), func(t *testing.T) {
				c, m, opts := closedReferenceImportTailCompile(t, vector, true, 0)
				pressure := 0
				opts.MemoryPressure = func() { pressure++ }
				tc.change(c, m, &opts)
				if image, err := stageClosedReferenceProductionModule(c, opts, m, vector); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("incoherent finalized tail reached allocation", err, pressure)
				}
			})
		}
		t.Run(fmt.Sprintf("vector%t-nonzero-production-base", vector), func(t *testing.T) {
			c, m, opts := closedReferenceImportTailCompile(t, vector, true, 1)
			pressure := 0
			opts.MemoryPressure = func() { pressure++ }
			if image, err := stageClosedReferenceProductionModule(c, opts, m, vector); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
				t.Fatal("nonzero production base reached allocation", err, pressure)
			}
		})
	}
}

func TestReferenceFunctionImportedTailDeadAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, dead := range []bool{false, true} {
			for _, op := range []byte{0x11, 0x13, 0x14, 0x15} {
				m, err := wasm.DecodeModule(wasmtest.ClosedReferenceImportTails(vector, false))
				if err != nil {
					t.Fatal(err)
				}
				body := []byte{op, 0}
				if op == 0x14 {
					body[1] = 3
				} // Numeric ordinary references are now admitted; private targets stay closed.
				if op == 0x11 || op == 0x13 {
					body = append(body, 0)
				}
				if dead {
					body = append([]byte{0}, body...)
				}
				m.Code[2].BodyBytes = append(body, 0x0b)
				if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
					t.Fatal("dead/live unconfined descriptor target admitted", vector, dead, op, err)
				}
			}
			m, err := wasm.DecodeModule(wasmtest.ClosedReferenceImportTails(vector, false))
			if err != nil {
				t.Fatal(err)
			}
			m.Types[3].SubTypes[0].Comp.Results[0] = wasm.FuncRef
			body := []byte{0x12, 0, 0x0b}
			if dead {
				body = append([]byte{0}, body...)
			}
			m.Code[2].BodyBytes = body
			opts := closedReferenceProductionOptions(true, false)
			opts.ImportBindings = hostImportBindings(3)
			pressure := 0
			opts.MemoryPressure = func() { pressure++ }
			if image, err := CompileModuleWith(m, opts); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
				t.Fatal("dead/live imported-tail result mismatch reached allocation", vector, dead, err, pressure)
			}
			m, err = wasm.DecodeModule(wasmtest.ClosedReferenceImportTails(vector, false))
			if err != nil {
				t.Fatal(err)
			}
			opts.NativeTailCalls = false
			if image, err := CompileModuleWith(m, opts); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
				t.Fatal("direct imported tails acquired permission implicitly", vector, err, pressure)
			}
			if dead {
				m.Code[2].BodyBytes = []byte{0, 0x12, 0, 0x0b}
				opts.NativeTailCalls = true
				image, err := CompileModuleWith(m, opts)
				if image == nil || err != nil || image.NativeTail == nil || len(image.NativeTail.Plans) != 0 {
					t.Fatal("valid dead-only imported tail finalization", vector, err)
				}
			}
		}
	}
}
