//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func referenceFunctionModule(signatures []*wasm.CompType, bindings []uint32, bodies [][]byte) *wasm.Module {
	m := &wasm.Module{}
	for _, ft := range signatures {
		m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: *ft}}})
	}
	for i, index := range bindings {
		m.FuncTypes = append(m.FuncTypes, wasm.TypeIdx{Index: index})
		m.Code = append(m.Code, wasm.Func{BodyBytes: bodies[i], LocalDeclBytes: 1})
	}
	return m
}

func referenceFunctionWalk(m *wasm.Module, local int, vector, enabled bool) error {
	types, specs := referenceControlDirectory(m)
	functions := make([]*wasm.CompType, len(m.FuncTypes))
	fs := make([]functionSpec, len(functions))
	for i, index := range m.FuncTypes {
		functions[i] = types[index.Index]
		fs[i] = specs[index.Index]
	}
	ft := functions[local]
	if vector {
		_, err := walkSWARPairsWithReferenceFunctionMode(&m.Code[local], ft, memoryProfile{}, types, functions, nil, 0, nil, nil, 0, 0, true, false, false, false, false, false, true, enabled, nil)
		return err
	}
	state := moduleState{production: true, importReferences: true, referenceCalls: true, referenceFunctions: enabled, types: specs, functions: fs}
	_, err := walkInteger(&m.Code[local], ft, &state, nil)
	return err
}

func referenceFunctionImage(t *testing.T, m *wasm.Module, vector bool) ([]byte, []FunctionInfo) {
	t.Helper()
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
		t.Fatal("reference function frontend proof", err)
	}
	types, specs := referenceControlDirectory(m)
	functions := make([]*wasm.CompType, len(m.FuncTypes))
	fs := make([]functionSpec, len(functions))
	for i, index := range m.FuncTypes {
		functions[i] = types[index.Index]
		fs[i] = specs[index.Index]
	}
	state := moduleState{production: true, importReferences: true, referenceCalls: true, referenceFunctions: true, types: specs, functions: fs}
	mode := emissionMode{production: true, interruptible: true, foreignTableCalls: true, referenceCalls: true, referenceFunctions: true}
	var image []byte
	infos := make([]FunctionInfo, len(m.Code))
	var relocs []directCall
	for i := range m.Code {
		mode.functionIndex, mode.entryPC = uint32(i), m.Code[i].LocalDeclBytes
		var code []byte
		var info FunctionInfo
		var calls []directCall
		var err error
		if vector {
			code, info, calls, err = compileSWARPairsWithGlobals(&m.Code[i], functions[i], nil, types, functions, nil, mode)
		} else {
			summary, e := walkInteger(&m.Code[i], functions[i], &state, nil)
			if e != nil {
				t.Fatal(e)
			}
			code, info, calls, err = compileIntegerMode(&m.Code[i], functions[i], &state, summary, mode)
		}
		if err != nil {
			t.Fatal("private reference function compilation", i, err)
		}
		for len(image)&15 != 0 {
			image = append(image, 0x13, 0, 0, 0)
		}
		info.Entry = len(image)
		info.CodeBytes = len(code)
		infos[i] = info
		for _, call := range calls {
			call.site += len(image)
			relocs = append(relocs, call)
		}
		image = append(image, code...)
	}
	asm := enc.Asm{B: image}
	for _, call := range relocs {
		if err := asm.PatchLong(call.site, int64(infos[call.callee].Entry)); err != nil {
			t.Fatal(err)
		}
	}
	return image, infos
}

func TestReferenceFunctionPrivateClassesAndFloors(t *testing.T) {
	base := &wasm.CompType{Kind: wasm.CompFunc}
	unary := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}, Results: []wasm.ValType{wasm.FuncRef}}
	for _, tc := range []struct {
		name string
		body []byte
		ok   bool
	}{
		{"identity", []byte{0x20, 0, 0x10, 0, 0x0b}, true},
		{"return", []byte{0x20, 0, 0x10, 0, 0x0f, 0x0b}, true},
		{"root-branch", []byte{0x20, 0, 0x10, 0, 0x0c, 0, 0x0b}, true},
		{"wrong-call", []byte{0x42, 0, 0x10, 0, 0x0b}, false},
		{"dead-unknown", []byte{0, 0x1b, 0x10, 0, 0x0b}, true},
		{"dead-known", []byte{0, 0x42, 0, 0x10, 0, 0x0b}, false},
		{"inherited-dead", []byte{0, 2, 0x40, 0x10, 0, 0x1a, 0x0b, 0x0b}, false},
		{"cross-floor", []byte{0x20, 0, 2, 0x40, 0x10, 0, 0x1a, 0x0b, 0x0b}, false},
		{"wrong-return", []byte{0x42, 0, 0x0b}, false},
		{"wrong-result-consumer", []byte{0x20, 0, 0x10, 0, 0x50, 0x1a, 0x20, 0, 0x0b}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := referenceFunctionModule([]*wasm.CompType{base, unary}, []uint32{1, 1}, [][]byte{{0x20, 0, 0x0b}, tc.body})
			err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256})
			var invalid *wasm.ValidationError
			if tc.ok && err != nil || !tc.ok && (!errors.As(err, &invalid) || invalid.Code != wasm.ErrTypeMismatch) {
				t.Fatal("frontend oracle", err)
			}
			for _, vector := range []bool{false, true} {
				for _, enabled := range []bool{false, true} {
					err := referenceFunctionWalk(m, 1, vector, enabled)
					if (err == nil) != (tc.ok && enabled) {
						t.Fatal("private reference call gate/class/floor", vector, enabled, err)
					}
				}
			}
		})
	}
}

func TestReferenceFunctionPrivateNeverPublished(t *testing.T) {
	for _, vector := range []bool{false, true} {
		ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}, Results: []wasm.ValType{wasm.FuncRef}}
		m := referenceFunctionModule([]*wasm.CompType{ft}, []uint32{0}, [][]byte{{0x20, 0, 0x0b}})
		if vector {
			m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}}}}})
		}
		code, infos := referenceFunctionImage(t, m, vector)
		if !vector && !infos[0].Result64 {
			t.Fatal("single reference result narrowed")
		}
		for _, stateMarker := range []bool{false, true} {
			pressure := 0
			c := &Compiled{code: code, functions: infos}
			if stateMarker {
				c = &Compiled{code: code, state: moduleState{referenceFunctions: true}}
			}
			for _, deferMapping := range []bool{false, true} {
				image, err := stageProduction(c, deferMapping)
				if image != nil || !errors.Is(err, ErrUnsupported) {
					t.Fatal("private ABI reached lower-level staging", vector, stateMarker, deferMapping, err)
				}
			}
			image, err := stageProductionModule(c, CompileOptions{DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
			if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
				t.Fatal("private reference ABI staged", vector, stateMarker, err, pressure)
			}
		}
		pressure := 0
		image, err := CompileModuleWith(m, referenceControlProductionOptions(true, &pressure))
		if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
			t.Fatal("source reference callable gate", vector, err, pressure)
		}
	}
}

func TestReferenceFunctionPrivateOtherCallsClosed(t *testing.T) {
	for _, op := range []byte{0x11, 0x12, 0x13, 0x14, 0x15} {
		for _, dead := range []bool{false, true} {
			for _, vector := range []bool{false, true} {
				t.Run(fmt.Sprintf("op%x-dead%t-vector%t", op, dead, vector), func(t *testing.T) {
					ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}, Results: []wasm.ValType{wasm.FuncRef}}
					body := []byte{op, 0}
					if op == 0x11 || op == 0x13 {
						body = append(body, 0)
					}
					if dead {
						body = append([]byte{0}, body...)
					}
					body = append(body, 0x0b)
					m := referenceFunctionModule([]*wasm.CompType{ft}, []uint32{0}, [][]byte{body})
					if err := referenceFunctionWalk(m, 0, vector, true); !errors.Is(err, ErrUnsupported) {
						t.Fatal("non-direct reference ABI admitted", err)
					}
				})
			}
		}
	}
}

func TestReferenceFunctionPrivatePartialDeadOperands(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.FuncRef}}
	for _, tc := range []struct {
		name string
		body []byte
		ok   bool
	}{
		{"missing-prefix", []byte{0, 0xd0, 0x70, 0x10, 0, 0x0b}, true},
		{"unknown-prefix", []byte{0, 0x1b, 0xd0, 0x70, 0x10, 0, 0x0b}, true},
		{"bad-suffix", []byte{0, 0x1b, 0x42, 0, 0x10, 0, 0x0b}, false},
		{"bad-prefix", []byte{0, 0x42, 0, 0xd0, 0x70, 0x10, 0, 0x0b}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := referenceFunctionModule([]*wasm.CompType{ft, {Kind: wasm.CompFunc}}, []uint32{0, 1}, [][]byte{{0x0b}, tc.body})
			err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256})
			var invalid *wasm.ValidationError
			if tc.ok && err != nil || !tc.ok && (!errors.As(err, &invalid) || invalid.Code != wasm.ErrTypeMismatch) {
				t.Fatal("partial operand frontend oracle", err)
			}
			for _, vector := range []bool{false, true} {
				if err := referenceFunctionWalk(m, 1, vector, true); (err == nil) != tc.ok {
					t.Fatal("partial known reference call operands", vector, err)
				}
			}
		})
	}
}

func TestReferenceFunctionPrivateBoundsAndBudget(t *testing.T) {
	for _, vector := range []bool{false, true} {
		limit := 256
		if vector {
			limit = 128
		}
		for _, n := range []int{limit, limit + 1} {
			params := make([]wasm.ValType, n)
			var leaf []byte
			for i := range params {
				params[i] = wasm.FuncRef
				leaf = append(leaf, 0x20)
				leaf = append(leaf, wasmtest.ULEB(uint32(i))...)
			}
			leaf = append(leaf, 0x0b)
			ft := &wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: params}
			m := referenceFunctionModule([]*wasm.CompType{ft}, []uint32{0}, [][]byte{leaf})
			if err := referenceFunctionWalk(m, 0, vector, true); (err == nil) != (n == limit) {
				t.Fatal("reference ABI profile arity", vector, n, err)
			}
			if n != limit {
				continue
			}
			good := (integerBudget - 2) / (3*n + 1)
			for _, calls := range []int{good, good + 1} {
				body := []byte{0}
				for range calls {
					body = append(body, 0x10, 0)
					body = append(body, bytes.Repeat([]byte{0x1a}, n)...)
				}
				body = append(body, 0x0b)
				m := referenceFunctionModule([]*wasm.CompType{ft, {Kind: wasm.CompFunc}}, []uint32{0, 1}, [][]byte{leaf, body})
				if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
					t.Fatal("dead call budget oracle", err)
				}
				if err := referenceFunctionWalk(m, 1, vector, true); (err == nil) != (calls == good) {
					t.Fatal("dead reference call signature budget", vector, calls, err)
				}
			}
		}
	}
}

func TestReferenceFunctionPrivateDirectoryAndMode(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}, Results: []wasm.ValType{wasm.FuncRef}}
	fn := &wasm.Func{BodyBytes: []byte{0x20, 0, 0x0b}}
	for _, n := range []int{MaxFunctions, MaxFunctions + 1} {
		for _, missing := range []bool{false, true} {
			specs := make([]functionSpec, n)
			types := make([]*wasm.CompType, n)
			for i := range specs {
				specs[i].signature = ft
				types[i] = ft
			}
			if missing {
				specs[n-1].signature = nil
				types[n-1] = nil
			}
			state := moduleState{production: true, importReferences: true, referenceCalls: true, referenceFunctions: true, functions: specs}
			_, a := walkInteger(fn, ft, &state, nil)
			_, b := walkSWARPairsWithReferenceFunctionMode(fn, ft, memoryProfile{}, nil, types, nil, 0, nil, nil, 0, 0, true, false, false, false, false, false, true, true, nil)
			want := n == MaxFunctions && !missing
			if (a == nil) != want || (b == nil) != want {
				t.Fatal("actual reference function directory", n, missing, a, b)
			}
		}
	}
	state := moduleState{production: true, importReferences: true, referenceCalls: true, referenceFunctions: true, functions: []functionSpec{{signature: ft}}, importedFunctions: 1}
	if _, err := walkInteger(fn, ft, &state, nil); !errors.Is(err, ErrUnsupported) {
		t.Fatal("reference imported prefix", err)
	}
	if _, err := walkSWARPairsWithReferenceFunctionMode(fn, ft, memoryProfile{}, nil, []*wasm.CompType{ft, ft}, nil, 0, nil, nil, 0, 1, true, false, false, false, false, false, true, true, nil); !errors.Is(err, ErrUnsupported) {
		t.Fatal("SWAR reference imported prefix", err)
	}
	state.importedFunctions = 0
	summary, err := walkInteger(fn, ft, &state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := compileIntegerMode(fn, ft, &state, summary, emissionMode{production: true}); !errors.Is(err, ErrUnsupported) {
		t.Fatal("reference preflight/emission mismatch", err)
	}
}
