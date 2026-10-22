//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"strings"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestReferenceFunctionResourceImportsSource(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tails := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-tails%t-wide%t", vector, tails, wide), func(t *testing.T) {
					m, err := wasm.DecodeModule(wasmtest.ClosedReferenceResourceImports(vector, tails, wide))
					if err != nil {
						t.Fatal(err)
					}
					if m.ImportedFuncCount() != 3 || m.ImportedGlobalCount() != 4 || m.ImportedMemCount() != 3 || m.GlobalCount() != 8 || m.MemCount() != 4 {
						t.Fatal("separate namespaces")
					}
					if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
						t.Fatal("numeric resource confinement", err)
					}
					opts := closedReferenceProductionOptions(tails, false)
					opts.ImportBindings = hostImportBindings(3)
					image, err := CompileModuleWith(m, opts)
					if err != nil || image == nil || !image.NativeReferenceFunctions || len(image.Entry) != len(m.Code) {
						t.Fatal("resource staging", err)
					}
				})
			}
		}
	}
}

func referenceResourceImportsDecode(t *testing.T, vector bool) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.ClosedReferenceResourceImports(vector, true, true))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReferenceFunctionResourceImportsRejectShapes(t *testing.T) {
	for _, vector := range []bool{false, true} {
		mutations := map[string]func(*wasm.Module){
			"global-reference": func(m *wasm.Module) {
				m.Imports[9].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.FuncRef})
			},
			"global-inactive-union": func(m *wasm.Module) {
				m.Imports[9].Type = wasm.NewMemExternType(wasm.MemType{Limits: wasm.Limits{Min: 1}})
				m.Imports[9].Type.Kind = wasm.ExternGlobal
			},
			"memory-inactive-union": func(m *wasm.Module) {
				m.Imports[8].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.I32})
				m.Imports[8].Type.Kind = wasm.ExternMem
			},
			"function-inactive-union": func(m *wasm.Module) {
				m.Imports[7].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.I32})
				m.Imports[7].Type.Kind = wasm.ExternFunc
			},
			"late-shared-memory": func(m *wasm.Module) {
				mt := m.Imports[8].Type.MemType()
				mt.Shared = true
				m.Imports[8].Type = wasm.NewMemExternType(mt)
			},
			"late-inactive-max": func(m *wasm.Module) {
				mt := m.Imports[8].Type.MemType()
				mt.Limits.HasMax = false
				m.Imports[8].Type = wasm.NewMemExternType(mt)
			},
			"late-bad-limits": func(m *wasm.Module) {
				mt := m.Imports[8].Type.MemType()
				mt.Limits.Max = 0
				m.Imports[8].Type = wasm.NewMemExternType(mt)
			},
			"table-import":            func(m *wasm.Module) { m.Imports[8].Type = wasm.NewTableExternType(wasm.TableType{}) },
			"tag-import":              func(m *wasm.Module) { m.Imports[9].Type = wasm.NewTagExternType(wasm.TagType{}) },
			"unknown-import":          func(m *wasm.Module) { m.Imports[9].Type.Kind = 99 },
			"total-import-bound":      func(m *wasm.Module) { m.Imports = make([]wasm.Import, MaxProductionImports+1) },
			"global-namespace":        func(m *wasm.Module) { m.Globals = make([]wasm.Global, 253) },
			"memory-namespace":        func(m *wasm.Module) { m.Memories = make([]wasm.MemType, 98) },
			"data-namespace":          func(m *wasm.Module) { m.Data[0].Mode.Mem = 4 },
			"global-export-namespace": func(m *wasm.Module) { m.Exports[0].Index = wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 8} },
			"memory-export-namespace": func(m *wasm.Module) { m.Exports[0].Index = wasm.ExternIdx{Kind: wasm.ExternMem, Index: 4} },
		}
		if !vector {
			mutations["unused-vector-global"] = func(m *wasm.Module) { m.Imports[9].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.V128}) }
		}
		for name, edit := range mutations {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				m := referenceResourceImportsDecode(t, vector)
				edit(m)
				opts := closedReferenceProductionOptions(true, false)
				opts.ImportBindings = hostImportBindings(3)
				switch name {
				case "memory-inactive-union", "function-inactive-union", "late-shared-memory", "late-inactive-max":
					checkPublicPrimitiveModule(t, m, opts)
					return
				}
				pressure := 0
				opts.MemoryPressure = func() { pressure++ }
				if image, err := CompileModuleWith(m, opts); image != nil || err == nil || pressure != 0 {
					t.Fatal("unsupported resource staged", err, pressure)
				}
			})
		}
		m := referenceResourceImportsDecode(t, vector)
		storage := strings.Repeat("n", MaxModuleBytes)
		lo, hi := 0, len(storage)
		for lo < hi {
			mid := (lo + hi + 1) / 2
			m.Imports[9].Name = storage[:mid]
			if admitClosedReferenceFunctionModule(m, vector) == nil {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		m.Imports[9].Name = storage[:lo]
		if lo == 0 || admitClosedReferenceFunctionModule(m, vector) != nil {
			t.Fatal("combined resource name budget")
		}
		m.Imports[9].Name = storage[:lo+1]
		if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
			t.Fatal("resource name plus one", err)
		}
		m.Imports[9].Name = storage[:lo]
		m.Imports[7].Name += "x"
		if err := admitClosedReferenceFunctionModule(m, vector); !errors.Is(err, ErrUnsupported) {
			t.Fatal("function/global names not aggregated", err)
		}
	}
}

func referenceResourceImportBounds(t *testing.T, functions, globals, memories int) *wasm.Module {
	t.Helper()
	var imports [][]byte
	for i := 0; i < max(functions, globals, memories); i++ {
		if i < globals {
			imports = append(imports, wasmtest.GlobalImportEntry("env.resources", "g", wasm.I32, false))
		}
		if i < functions {
			imports = append(imports, append(append(wasmtest.Name("env.calls"), wasmtest.Name("f")...), 0, 0))
		}
		if i < memories {
			imports = append(imports, append(append(wasmtest.Name("env.resources"), wasmtest.Name("m")...), 2, 1, 0, 0))
		}
	}
	body := append([]byte{0xd0, 0x70, 0x10}, wasmtest.ULEB(uint32(functions+1))...)
	body = append(body, 0x1a, 0x0b)
	exports := [][]byte{wasmtest.ExportEntry("run", 0, uint32(functions)), wasmtest.ExportEntry("last-global", 3, uint32(globals-1)), wasmtest.ExportEntry("last-memory", 2, uint32(memories-1))}
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x20, 0, 0x0b}))))
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReferenceFunctionResourceImportsNamespaceBounds(t *testing.T) {
	for _, extra := range []string{"exact", "functions", "globals", "memories"} {
		t.Run(extra, func(t *testing.T) {
			f, g, mem := MaxHostFunctionImports, 256, MaxProductionMemories
			switch extra {
			case "functions":
				f++
			case "globals":
				g++
			case "memories":
				mem++
			}
			m := referenceResourceImportBounds(t, f, g, mem)
			if extra == "exact" {
				// Each call uses a fresh module, so warm-up cannot hide an allocated
				// cached import directory. Uniform-width admission itself allocates zero.
				fresh := make([]wasm.Module, 11)
				for i := range fresh {
					fresh[i] = *m
				}
				index := 0
				if allocs := testing.AllocsPerRun(10, func() {
					if admitClosedReferenceFunctionModule(&fresh[index], false) != nil {
						panic("namespace preflight")
					}
					index++
				}); allocs != 0 {
					t.Fatal("import preflight allocation", allocs)
				}
			}
			opts := closedReferenceProductionOptions(false, false)
			opts.ImportBindings = hostImportBindings(f)
			pressure := 0
			opts.MemoryPressure = func() { pressure++ }
			image, err := CompileModuleWith(m, opts)
			if extra == "exact" || extra == "globals" {
				if err != nil || image == nil || pressure != 1 {
					t.Fatal("exact combined namespaces", err, pressure)
				}
			} else if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
				t.Fatal("namespace plus one staged", err, pressure)
			}
		})
	}
	// A cached import directory must not authorize a changed same-slice module.
	for _, kind := range []wasm.ExternKind{wasm.ExternFunc, wasm.ExternGlobal, wasm.ExternMem} {
		m := referenceResourceImportBounds(t, MaxHostFunctionImports, 256, MaxProductionMemories)
		if m.ImportedFuncCount() != 256 || m.ImportedGlobalCount() != 256 || m.ImportedMemCount() != 100 {
			t.Fatal("cached prefix")
		}
		index := 0
		for m.Imports[index].Type.Kind == kind {
			index++
		}
		switch kind {
		case wasm.ExternFunc:
			m.Imports[index].Type = wasm.NewFuncExternType(wasm.TypeIdx{})
		case wasm.ExternGlobal:
			m.Imports[index].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.I32})
		case wasm.ExternMem:
			m.Imports[index].Type = wasm.NewMemExternType(wasm.MemType{Limits: wasm.Limits{HasMax: true}})
		}
		if err := admitClosedReferenceFunctionModule(m, false); !errors.Is(err, ErrUnsupported) {
			t.Fatal("stale directory authorized namespace", kind, err)
		}
	}
}
