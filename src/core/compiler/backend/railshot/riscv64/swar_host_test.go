//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func swarHostSource(count, target, slots int, nested bool) ([]byte, []fixtureFunc, []byte) {
	hostTypes := make([]byte, slots)
	for i := range hostTypes {
		hostTypes[i] = []byte{0x7f, 0x7e, 0x7d, 0x7c}[i%4]
	}
	params := append([]byte{0x7b, 0x7c}, hostTypes...)
	callee := fixtureFunc{params: params, results: params, locals: []byte{1, byte(128 - len(params)), 0x7b}, body: []byte{0x20, 0, 0x20, 1}, unexported: true}
	for i := range hostTypes {
		callee.body = uleb(append(callee.body, 0x20), uint32(i+2))
	}
	callee.body = append(uleb(append(callee.body, 0x10), uint32(target)), 0x0b)
	root := callee
	root.unexported = false
	fixtures := []fixtureFunc{root}
	if nested {
		root.results = append([]byte{0x7b, 0x7c}, params...)
		root.body = []byte{0x20, 0, 0x20, 1}
		for i := range params {
			root.body = uleb(append(root.body, 0x20), uint32(i))
		}
		root.body = append(uleb(append(root.body, 0x10), uint32(count+1)), 0x0b)
		fixtures = []fixtureFunc{root, callee}
	}
	source := binaryModule(fixtures...)
	types := []byte{byte(1 + len(fixtures))}
	for _, f := range append([]fixtureFunc{{params: hostTypes, results: hostTypes}}, fixtures...) {
		types = append(types, 0x60)
		types = append(uleb(types, uint32(len(f.params))), f.params...)
		types = append(uleb(types, uint32(len(f.results))), f.results...)
	}
	source = modifySection(source, 1, 1, types, true)
	functionTypes := []byte{byte(len(fixtures))}
	for i := range fixtures {
		functionTypes = append(functionTypes, byte(i+1))
	}
	source = modifySection(source, 3, 3, functionTypes, true)
	imports := uleb(nil, uint32(count))
	for range count {
		imports = append(imports, 1, 'm', 1, 'h', 0, 0)
	}
	source = modifySection(source, 3, 2, imports, false)
	source = modifySection(source, 7, 7, uleb([]byte{1, 1, 'a', 0}, uint32(count)), true)
	return source, fixtures, hostTypes
}

func swarValidatedHostSource(t *testing.T, source []byte) (*wasm.Module, []*wasm.CompType, []*wasm.CompType) {
	t.Helper()
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 128}); err != nil {
		t.Fatal(err)
	}
	types := make([]*wasm.CompType, len(m.Types))
	for i := range types {
		types[i] = &m.Types[i].SubTypes[0].Comp
	}
	functions := make([]*wasm.CompType, m.FuncCount())
	for i := range functions {
		var ok bool
		if i < m.ImportedFuncCount() {
			functions[i], ok = m.ImportFuncType(i)
		} else {
			functions[i], ok = m.LocalFuncType(i - m.ImportedFuncCount())
		}
		if !ok {
			t.Fatal("validated host function directory", i)
		}
	}
	return m, types, functions
}

func TestSWARScalarHostExplicitPrefixAndDefinedRelocations(t *testing.T) {
	for _, count := range []int{1, 65, 256} {
		for _, slots := range []int{0, 4, 65, 126} {
			for _, nested := range []bool{false, true} {
				if slots == 126 && nested {
					continue // The additional live caller prefix would exceed128 results.
				}
				source, _, _ := swarHostSource(count, count-1, slots, nested)
				m, types, functions := swarValidatedHostSource(t, source)
				for i := range m.Code {
					mode := emissionMode{production: true, interruptible: true, functionIndex: uint32(count + i), entryPC: m.Code[i].LocalDeclBytes}
					code, info, calls, err := compileSWARPairsWithImports(&m.Code[i], functions[count+i], nil, types, functions, nil, 0, nil, nil, 0, count, mode)
					if err != nil || len(code) == 0 || info.FrameHeaderBytes != callFrameHeader || info.FrameBytes <= 2048 || info.mode.functionIndex != uint32(count+i) {
						t.Fatal("host prefix code/frame/source", count, slots, nested, i, err)
					}
					if nested && i == 0 {
						if len(calls) != 1 || calls[0].callee != 1 {
							t.Fatal("defined relocation excludes imported prefix", calls)
						}
					} else if len(calls) != 0 || info.DirectCalls != 0 {
						t.Fatal("dynamic host call fabricated direct relocation", calls)
					}
					for _, mode := range []emissionMode{{}, {production: true}} {
						if code, _, _, err := compileSWARPairsWithImports(&m.Code[i], functions[count+i], nil, types, functions, nil, 0, nil, nil, 0, count, mode); code != nil || err == nil {
							t.Fatal("host missing production/interruption bridge", err)
						}
					}
				}
				for _, compile := range []func([]byte, emissionMode) (*swarModuleImage, error){compileSWARModule, compileSWARTableModule} {
					if image, err := compile(source, emissionMode{production: true, interruptible: true}); image != nil || err == nil {
						t.Fatal("private module import policy widened", err)
					}
				}
			}
		}
	}
}

func TestSWARScalarHostUnusedSignatureBoundsAndDeadTyping(t *testing.T) {
	source, _, _ := swarHostSource(256, 255, 4, false)
	m, types, functions := swarValidatedHostSource(t, source)
	fn, ft := &m.Code[0], functions[256]
	mode := emissionMode{production: true, interruptible: true}
	for _, count := range []int{-1, 257, len(functions)} {
		if code, _, _, err := compileSWARPairsWithImports(fn, ft, nil, types, functions, nil, 0, nil, nil, 0, count, mode); code != nil || err == nil {
			t.Fatal("host prefix bound", count, err)
		}
	}
	table := &wasm.TableType{Ref: wasm.AbsRef(wasm.HeapFunc), Limits: wasm.Limits{Min: 0, Max: 1, HasMax: true}}
	if code, _, _, err := compileSWARPairsWithImports(fn, ft, nil, types, functions, nil, 0, table, nil, 0, 256, mode); code != nil || err == nil {
		t.Fatal("imports with private references admitted", err)
	}
	for _, sig := range []*wasm.CompType{{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}}, {Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.V128}}, {Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}}, {Kind: wasm.CompFunc, Params: make([]wasm.ValType, 129)}} {
		bad := append([]*wasm.CompType(nil), functions...)
		bad[0] = sig // Uncalled import0 must be checked, not only target255.
		if code, _, _, err := compileSWARPairsWithImports(fn, ft, nil, types, bad, nil, 0, nil, nil, 0, 256, mode); code != nil || err == nil {
			t.Fatal("unused unsupported host signature admitted", err)
		}
	}
	// Dead calls retain full signatures/index/transfer bounds but emit no guards,
	// code, cold islands or result transports when the machine path is dead.
	fn.BodyBytes = []byte{0x00, 0x0b}
	baseline, info, _, err := compileSWARPairsWithImports(fn, ft, nil, types, functions, nil, 0, nil, nil, 0, 256, mode)
	if err != nil {
		t.Fatal(err)
	}
	// Discard the scalar results before the function's synthetic polymorphic end.
	fn.BodyBytes = []byte{0x00, 0x10, 0xff, 1, 0x1a, 0x1a, 0x1a, 0x1a, 0x0b}
	code, actual, calls, err := compileSWARPairsWithImports(fn, ft, nil, types, functions, nil, 0, nil, nil, 0, 256, mode)
	if err != nil || !bytes.Equal(code, baseline) || actual.FrameBytes != info.FrameBytes || actual.SpillSlots != info.SpillSlots || len(calls) != 0 {
		t.Fatal("dead host ghost code/transport/frame", err)
	}
	for _, body := range [][]byte{{0x00, 0x10, 0x82, 2, 0x0b}, {0x00, 0x10, 0x80}, {0x00, 0x20, 0, 0x10, 0xff, 1, 0x0b}, {0x00, 2, 0x40, 0x10, 0xff, 1, 0x0b, 0x0b}} {
		fn.BodyBytes = body
		if code, _, _, err := compileSWARPairsWithImports(fn, ft, nil, types, functions, nil, 0, nil, nil, 0, 256, mode); code != nil || err == nil {
			t.Fatal("dead host index/immediate/class/floor escaped", body, err)
		}
	}
}
