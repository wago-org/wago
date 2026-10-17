//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func referenceImportsDecode(t *testing.T, vector, tails, wide bool) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.ClosedReferenceImports(vector, tails, wide))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReferenceFunctionImportsSource(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tails := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-tails%t-wide%t", vector, tails, wide), func(t *testing.T) {
					m := referenceImportsDecode(t, vector, tails, wide)
					if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
						t.Fatal("confinement", err)
					}
					if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{ExtendedConstGlobals: true, MultiMemory: true}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
						t.Fatal("frontend", err)
					}
					opts := closedReferenceProductionOptions(tails, false)
					opts.ImportBindings = hostImportBindings(3)
					image, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal("numeric import staging", err)
					}
					if !image.NativeReferenceFunctions || image.NativeReferenceCalls || len(image.Entry) != len(m.Code) || image.NativeTail != nil && len(image.NativeTail.Plans) != 0 {
						t.Fatal("imported ABI/ordinal witnesses")
					}
				})
			}
		}
	}
}

func TestReferenceFunctionImportsRejectEscapes(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tc := range []struct {
			name string
			edit func(*wasm.Module, *CompileOptions)
		}{
			{"profile", func(_ *wasm.Module, o *CompileOptions) { o.Profile = true }},
			{"source-maps", func(_ *wasm.Module, o *CompileOptions) { o.SourceMaps = true }},
			{"unwind-maps", func(_ *wasm.Module, o *CompileOptions) { o.UnwindMaps = true }},
			{"reference-resource-import", func(m *wasm.Module, _ *CompileOptions) {
				m.Imports[2].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.FuncRef})
			}},
			{"recursive-import-index", func(m *wasm.Module, _ *CompileOptions) {
				m.Imports[2].Type = wasm.NewFuncExternType(wasm.TypeIdx{Rec: true})
			}},
			{"too-many-imports", func(m *wasm.Module, _ *CompileOptions) { m.Imports = make([]wasm.Import, MaxHostFunctionImports+1) }},
			{"reference-import", func(m *wasm.Module, _ *CompileOptions) { m.Imports[0].Type = wasm.NewFuncExternType(m.FuncTypes[2]) }},
			{"bad-export", func(m *wasm.Module, _ *CompileOptions) { m.Exports[0].Index.Index = 99 }},
			{"reference-declaration", func(m *wasm.Module, _ *CompileOptions) { m.Elements[0].Kind.Funcs[0] = 6 }},
			{"reference-descriptor", func(m *wasm.Module, _ *CompileOptions) { m.Code[1].BodyBytes = []byte{0, 0xd2, 5, 0x1a, 0x0b} }},
			{"imported-tail", func(m *wasm.Module, _ *CompileOptions) { m.Code[1].BodyBytes = []byte{0, 0x12, 0, 0x0b} }},
			{"last-imported-tail", func(m *wasm.Module, _ *CompileOptions) { m.Code[1].BodyBytes = []byte{0, 0x12, 2, 0x0b} }},
			{"late-invalid-binding", func(m *wasm.Module, _ *CompileOptions) {
				m.Imports[2].Type = wasm.NewFuncExternType(wasm.TypeIdx{Index: 999})
			}},
			{"import-name-bound", func(m *wasm.Module, _ *CompileOptions) { m.Imports[2].Name = strings.Repeat("x", MaxModuleBytes) }},
			{"missing-binding", func(_ *wasm.Module, o *CompileOptions) { o.ImportBindings = o.ImportBindings[:2] }},
			{"wrong-binding-index", func(_ *wasm.Module, o *CompileOptions) { o.ImportBindings[2].ImportIndex = 1 }},
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, tc.name), func(t *testing.T) {
				m := referenceImportsDecode(t, vector, true, false)
				opts := closedReferenceProductionOptions(true, false)
				opts.ImportBindings = hostImportBindings(3)
				pressure := 0
				opts.MemoryPressure = func() { pressure++ }
				tc.edit(m, &opts)
				if tc.name == "import-name-bound" {
					checkPublicPrimitiveModule(t, m, opts)
					return
				}
				if image, err := CompileModuleWith(m, opts); image != nil || err == nil || pressure != 0 {
					t.Fatal("unsupported shape staged", err, pressure)
				}
			})
		}
	}
}

func TestReferenceFunctionImportsNameBudget(t *testing.T) {
	for _, vector := range []bool{false, true} {
		m := referenceImportsDecode(t, vector, false, true)
		name := strings.Repeat("n", MaxModuleBytes)
		lo, hi := 0, len(name)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			m.Imports[2].Name = name[:mid]
			if admitClosedReferenceFunctionModule(m, vector) == nil {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		m.Imports[2].Name = name[:lo]
		if lo == 0 || admitClosedReferenceFunctionModule(m, vector) != nil {
			t.Fatal("exact aggregate import name bound")
		}
		m.Imports[2].Name = name[:lo+1]
		if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
			t.Fatal("import name plus one", err)
		}
		m.Imports[2].Name = name[:lo]
		m.Imports[0].Module += "x"
		if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
			t.Fatal("import names not charged together", err)
		}
	}
}

func TestReferenceFunctionImportsCountBound(t *testing.T) {
	for _, count := range []int{MaxHostFunctionImports, MaxHostFunctionImports + 1} {
		var imports [][]byte
		for range count {
			imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("f")...), 0, 0))
		}
		body := []byte{0xd0, 0x70, 0x10}
		body = append(body, wasmtest.ULEB(uint32(count+1))...)
		body = append(body, 0x1a, 0x0b)
		source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, uint32(count)))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x20, 0, 0x0b}))))
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		opts := closedReferenceProductionOptions(false, false)
		opts.ImportBindings = hostImportBindings(count)
		pressure := 0
		opts.MemoryPressure = func() { pressure++ }
		image, err := CompileModuleWith(m, opts)
		if count == MaxHostFunctionImports {
			if err != nil || image == nil || pressure != 1 || len(image.Entry) != 2 || !image.NativeReferenceFunctions {
				t.Fatal("exact imported prefix bound", err, pressure)
			}
		} else if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
			t.Fatal("imported prefix plus one staged", err, pressure)
		}
	}
}

func TestReferenceFunctionImportsStagingRechecks(t *testing.T) {
	for _, count := range []int{0, 2, 4} {
		t.Run(fmt.Sprintf("imports%d", count), func(t *testing.T) {
			m := referenceImportsDecode(t, false, false, true)
			opts := closedReferenceOptions(false)
			opts.hostImports = true
			opts.ImportBindings = hostImportBindings(3)
			c, err := compileDecodedModule(m, scalarRegisters[:], opts)
			if err != nil {
				t.Fatal("real imported private compilation", err)
			}
			_, _, _, control, err := admitProductionDynamicTails(m, true, false, false, true)
			if err != nil {
				t.Fatal(err)
			}
			stage := closedReferenceProductionOptions(false, true)
			stage.referenceFunctions = true
			stage.referenceControlInstructions = control
			if image, err := stageClosedReferenceProductionModule(c, stage, m, false); image == nil || err != nil {
				t.Fatal("valid imported staging baseline", err)
			}
			c.state.importedFunctions = count
			pressure := 0
			stage.MemoryPressure = func() { pressure++ }
			if image, err := stageClosedReferenceProductionModule(c, stage, m, false); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
				t.Fatal("incorrect imported count staged", err, pressure)
			}
		})
	}
}
