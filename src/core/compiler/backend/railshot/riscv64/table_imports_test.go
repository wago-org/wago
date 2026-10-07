//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The wire import order and type order differ from the function namespace.
func combinedTableImportSource(count int, vector, importedTable bool, elementMode byte) []byte {
	value := wasm.I32
	if vector {
		value = wasm.V128
	}
	types := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}), wasmtest.FuncType([]wasm.ValType{value}, []wasm.ValType{value})))
	imports := [][]byte{append(append(wasmtest.Name("env"), wasmtest.Name("global")...), 3, 0x7f, 0)}
	if importedTable {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 1, 2))
	}
	for i := 0; i < count; i++ {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name(fmt.Sprintf("host%d", i))...), 0, 1))
	}
	imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 0, 1))
	sections := [][]byte{types, wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 1})}
	if !importedTable {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 1, 2}))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("target", 0, uint32(count)))))
	var element []byte
	if elementMode == 0 {
		element = []byte{0, 0x41, 0, 0x0b, 1}
	} else {
		element = []byte{elementMode, 0, 1}
	}
	element = append(element, wasmtest.ULEB(uint32(count))...)
	sections = append(sections, wasmtest.Section(9, wasmtest.Vec(element)))
	body := append([]byte{0x20, 0, 0x10}, wasmtest.ULEB(uint32(count-1))...)
	body = append(body, 0x0b)
	return wasmtest.Module(append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))...)
}

func TestProductionTableFunctionImportsNamespace(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, borrowed := range []bool{false, true} {
			for _, count := range []int{1, 256} {
				for _, mode := range []byte{0, 1, 3} {
					source := combinedTableImportSource(count, vector, borrowed, mode)
					m, err := wasm.DecodeModule(source)
					if err != nil {
						t.Fatal(err)
					}
					if err := AdmitProductionSource(source); err != nil {
						t.Fatal(err)
					}
					opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(count)}
					image, err := CompileModuleWith(m, opts)
					if err != nil || image == nil || len(image.Entry) != 1 || len(image.Code) == 0 {
						t.Fatal("combined namespace", vector, borrowed, count, mode, err)
					}
					if vector {
						state := swarStateProfile{publicStarts: true, exports: true, importedMemory: true, importedGlobals: true, importedTable: true, functionTableImports: true}
						image, err := compileSWARDecodedModuleProfile(m, emissionMode{production: true, interruptible: true, foreignTableCalls: true}, swarModuleTables, state)
						if err != nil {
							t.Fatal(err)
						}
						ft, _ := m.LocalFuncType(0)
						key := wasm.StructuralFuncTypeKey(ft)
						if len(image.functionKeys) != count+1 {
							t.Fatal("function key namespace", len(image.functionKeys))
						}
						for i, k := range image.functionKeys {
							if k != key {
								t.Fatal("wrong imported/defined key", i, k, key)
							}
						}
						state.functionTableImports = false
						if image, err := compileSWARDecodedModuleProfile(m, emissionMode{production: true, interruptible: true, foreignTableCalls: true}, swarModuleTables, state); image != nil || !errors.Is(err, ErrUnsupported) {
							t.Fatal("private profile widened", err)
						}
					}
					opts.ForeignTableCalls = false
					if image, err := CompileModuleWith(m, opts); image != nil || !errors.Is(err, ErrUnsupported) {
						t.Fatal("missing retained context admitted", err)
					}
				}
			}
		}
	}
}

func TestProductionTableFunctionImportsRejectProxyReferences(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, mode := range []byte{0, 1, 3} {
			m, err := wasm.DecodeModule(combinedTableImportSource(2, vector, false, mode))
			if err != nil {
				t.Fatal(err)
			}
			opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, ImportBindings: hostImportBindings(2)}
			pressure := 0
			opts.MemoryPressure = func() { pressure++ }
			for _, index := range []wasm.FuncIdx{0, 1, 3} {
				m.Elements[0].Kind.Funcs[0] = index
				image, err := CompileModuleWith(m, opts)
				if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("proxy/out-of-bounds element staged", vector, mode, index, err)
				}
			}
			// A function export declares imported ref.func to the Wasm validator;
			// the complete backend walk must reject it even after unreachable.
			m.Elements[0].Kind.Funcs[0] = 2
			ex := m.Exports[0]
			ex.Name, ex.Index.Index = "imported", 0
			m.Exports = append(m.Exports, ex)
			m.Code[0].BodyBytes = []byte{0, 0xd2, 0, 0x1a, 0x0b}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal("invalid negative fixture", err)
			}
			image, err := CompileModuleWith(m, opts)
			if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
				t.Fatal("dead imported ref.func staged", vector, err)
			}
		}
	}
}
