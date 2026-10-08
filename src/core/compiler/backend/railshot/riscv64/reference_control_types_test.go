//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func referenceControlModule(t *testing.T, body []byte) (*wasm.Module, []*wasm.CompType, []functionSpec) {
	t.Helper()
	types := [][]byte{
		wasmtest.FuncType(nil, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.FuncRef}, []wasm.ValType{wasm.I32, wasm.FuncRef}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.FuncRef}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I64}),
	}
	m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body)))))
	if err != nil {
		t.Fatal(err)
	}
	directory, specs := referenceControlDirectory(m)
	return m, directory, specs
}

func referenceControlDirectory(m *wasm.Module) ([]*wasm.CompType, []functionSpec) {
	directory := make([]*wasm.CompType, len(m.Types))
	specs := make([]functionSpec, len(directory))
	for i := range directory {
		ft := &m.Types[i].SubTypes[0].Comp
		directory[i], specs[i].signature = ft, ft
		specs[i].params, specs[i].results = make([]bool, len(ft.Params)), make([]bool, len(ft.Results))
		for j, typ := range ft.Params {
			specs[i].params[j] = scalarWide(typ)
		}
		for j, typ := range ft.Results {
			specs[i].results[j] = scalarWide(typ)
		}
	}
	return directory, specs
}

func TestReferenceIndexedControlClassesAndFloors(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		ok   bool
	}{
		{"identity", []byte{0xd2, 0, 2, 1, 0x0b, 0x1a, 0x0b}, true},
		{"if-else", []byte{0xd2, 0, 0x41, 0, 4, 1, 5, 0x0b, 0x1a, 0x0b}, true},
		{"if-no-else", []byte{0xd2, 0, 0x41, 0, 4, 1, 0x0b, 0x1a, 0x0b}, true},
		{"branch", []byte{2, 2, 0x42, 9, 0xd2, 0, 0x0c, 0, 0x0b, 0x1a, 0x0b}, true},
		{"loop", []byte{0xd2, 0, 3, 1, 0x41, 0, 0x0d, 0, 0x0b, 0x1a, 0x0b}, true},
		{"br-table", []byte{2, 2, 2, 0x70, 0xd2, 0, 0x41, 0, 0x0e, 1, 0, 1, 0x0b, 0x0b, 0x1a, 0x0b}, true},
		{"mixed", []byte{0x41, 7, 0xd2, 0, 2, 3, 0x0b, 0x1a, 0x1a, 0x0b}, true},
		{"null-loop", []byte{0xd2, 0, 3, 1, 0xd2, 0, 0xd5, 0, 0x1a, 0x0b, 0x1a, 0x0b}, true},
		{"nonnull-loop", []byte{0x41, 7, 0xd0, 0x70, 3, 3, 0xd6, 0, 0xd0, 0x70, 0x0b, 0x1a, 0x1a, 0x0b}, true},
		{"unknown-param", []byte{0, 0x1b, 2, 1, 0x0b, 0x1a, 0x0b}, true},
		{"unknown-result", []byte{2, 2, 0, 0x1b, 0x0b, 0x1a, 0x0b}, true},
		{"unknown-mixed-null-prefix", []byte{2, 4, 0, 0x1b, 0xd0, 0x70, 0xd5, 0, 0x1a, 0x0b, 0x1a, 0x1a, 0x0b}, true},
		{"unknown-mixed-nonnull-prefix", []byte{2, 4, 0, 0x1b, 0xd0, 0x70, 0xd6, 0, 0xd0, 0x70, 0x0b, 0x1a, 0x1a, 0x0b}, true},
		{"wrong-param", []byte{0x42, 0, 2, 1, 0x0b, 0x1a, 0x0b}, false},
		{"wrong-result", []byte{2, 2, 0x42, 0, 0x0b, 0x1a, 0x0b}, false},
		{"dead-wrong-param", []byte{0, 0x42, 0, 2, 1, 0x0b, 0x1a, 0x0b}, false},
		{"dead-wrong-result", []byte{2, 2, 0, 0x42, 0, 0x0b, 0x1a, 0x0b}, false},
		{"inherited-dead-param", []byte{0, 2, 0x40, 2, 1, 0x0b, 0x1a, 0x0b, 0x0b}, false},
		{"inherited-dead-result", []byte{0, 2, 2, 0x0b, 0x1a, 0x0b}, false},
		{"cross-floor-param", []byte{0xd2, 0, 2, 0x40, 2, 1, 0x0b, 0x1a, 0x0b, 0x1a, 0x0b}, false},
		{"cross-floor-branch", []byte{2, 2, 0xd2, 0, 2, 0x40, 0x0c, 1, 0x0b, 0x0b, 0x1a, 0x0b}, false},
		{"extra-result", []byte{2, 2, 0xd2, 0, 0xd2, 0, 0x0b, 0x1a, 0x0b}, false},
		{"extra-dead-result", []byte{2, 2, 0, 0xd2, 0, 0xd2, 0, 0x0b, 0x1a, 0x0b}, false},
		{"wrong-then", []byte{0xd2, 0, 0x41, 0, 4, 1, 0x1a, 0x42, 0, 5, 0x0b, 0x1a, 0x0b}, false},
		{"wrong-else", []byte{0xd2, 0, 0x41, 0, 4, 1, 5, 0x1a, 0x42, 0, 0x0b, 0x1a, 0x0b}, false},
		{"missing-else", []byte{0x41, 0, 4, 2, 0xd2, 0, 0x0b, 0x1a, 0x0b}, false},
		{"wrong-condition", []byte{0xd2, 0, 0x42, 0, 4, 1, 0x0b, 0x1a, 0x0b}, false},
		{"wrong-loop-edge", []byte{0xd2, 0, 3, 1, 0x1a, 0x42, 0, 0x0c, 0, 0x0b, 0x1a, 0x0b}, false},
		{"br-table-unknown-heterogeneous", []byte{2, 2, 2, 5, 0, 0x0e, 1, 0, 1, 0x0b, 0x1a, 0, 0x0b, 0x1a, 0x0b}, true},
		{"br-table-reversed", []byte{2, 2, 2, 5, 0, 0x0e, 1, 1, 0, 0x0b, 0x1a, 0, 0x0b, 0x1a, 0x0b}, true},
		{"br-table-duplicate", []byte{2, 2, 2, 5, 0, 0x0e, 2, 0, 0, 1, 0x0b, 0x1a, 0, 0x0b, 0x1a, 0x0b}, true},
		{"br-table-present-unknown", []byte{2, 2, 2, 5, 0, 0x1b, 0x41, 0, 0x0e, 1, 0, 1, 0x0b, 0x1a, 0, 0x0b, 0x1a, 0x0b}, true},
		{"br-table-known-suffix", []byte{2, 4, 2, 6, 0, 0x42, 0, 0x41, 0, 0x0e, 1, 0, 1, 0x0b, 0x1a, 0x1a, 0, 0x0b, 0x1a, 0x1a, 0x0b}, false},
		{"br-table-arity", []byte{2, 2, 2, 4, 0, 0x0e, 1, 0, 1, 0x0b, 0x1a, 0x1a, 0, 0x0b, 0x1a, 0x0b}, false},
		{"br-table-known-heterogeneous", []byte{2, 2, 2, 5, 0, 0x42, 0, 0x41, 0, 0x0e, 1, 0, 1, 0x0b, 0x1a, 0, 0x0b, 0x1a, 0x0b}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, types, specs := referenceControlModule(t, tc.body)
			err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256})
			var invalid *wasm.ValidationError
			if tc.ok && err != nil || !tc.ok && (!errors.As(err, &invalid) || invalid.Code != wasm.ErrTypeMismatch) {
				t.Fatal("frontend oracle", err)
			}
			for _, enabled := range []bool{false, true} {
				state := moduleState{production: true, importReferences: true, referenceCalls: enabled, types: specs, functions: specs[:1]}
				_, a := walkInteger(&m.Code[0], types[0], &state, nil)
				_, b := walkSWARPairsWithTypedCallMode(&m.Code[0], types[0], memoryProfile{}, types, types[:1], nil, 0, nil, nil, 1, 0, true, false, false, false, false, false, enabled, nil)
				if (a == nil) != (tc.ok && enabled) || (b == nil) != (tc.ok && enabled) {
					t.Fatal("control-only permission/class", enabled, a, b)
				}
			}
			if tc.ok {
				for _, vector := range []bool{false, true} {
					if vector {
						m.Types = append(m.Types, wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}}}}})
					}
					pressure := 0
					image, err := CompileModuleWith(m, CompileOptions{NativeReferenceCalls: true, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
					if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
						t.Fatal("production reference control gate", vector, image, err, pressure)
					}
				}
			}
		})
	}
}

func TestReferenceIndexedControlTypesAndBudget(t *testing.T) {
	for _, ref := range []wasm.ValType{wasm.FuncRef, wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapFunc), false)), wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false)), wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))} {
		ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, ref}, Results: []wasm.ValType{ref, wasm.I64}}
		spec := functionSpec{signature: ft}
		dir := internalReferenceTypes{scalar: []functionSpec{{signature: &wasm.CompType{Kind: wasm.CompFunc}}, spec}}
		for _, limit := range []int{integerBudget - 4, integerBudget - 3} {
			budget := limit
			p, r, pr, rr, err := integerControlSignature(spec, dir, true, true, &budget)
			if limit == integerBudget-4 {
				if err != nil || budget != integerBudget || len(p) != 2 || p[0] || !p[1] || !r[0] || !r[1] || pr[0] || !pr[1] || !rr[0] || rr[1] {
					t.Fatal("reference control flags/budget", err, p, r, pr, rr, budget)
				}
			} else if err == nil || len(pr) != 0 || len(rr) != 0 {
				t.Fatal("control budget before flags", err)
			}
		}
	}
	for _, vector := range []bool{false, true} {
		limit := 256
		if vector {
			limit = maxSWARLocals
		}
		for _, count := range []int{limit, limit + 1} {
			ft := &wasm.CompType{Kind: wasm.CompFunc, Params: make([]wasm.ValType, count), Results: make([]wasm.ValType, count)}
			for i := range ft.Params {
				ft.Params[i], ft.Results[i] = wasm.FuncRef, wasm.FuncRef
			}
			budget := 0
			if vector {
				_, _, err := swarControlSignature(ft, internalReferenceTypes{swar: []*wasm.CompType{ft}}, true, &budget)
				if (err == nil) != (count == limit) {
					t.Fatal("SWAR arity", count, err)
				}
			} else {
				_, _, _, _, err := integerControlSignature(functionSpec{signature: ft}, internalReferenceTypes{scalar: []functionSpec{{signature: ft}}}, true, true, &budget)
				if (err == nil) != (count == limit) {
					t.Fatal("scalar arity", count, err)
				}
			}
		}
	}
	// Every dead occurrence consumes the signature budget before allocation.
	for _, count := range []int{100, integerBudget/4 + 1} {
		body := append([]byte{0}, bytes.Repeat([]byte{2, 1, 0x0b, 0x1a}, count)...)
		body = append(body, 0x0b)
		m, types, specs := referenceControlModule(t, body)
		if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
			t.Fatal(err)
		}
		state := moduleState{production: true, importReferences: true, referenceCalls: true, types: specs}
		_, a := walkInteger(&m.Code[0], types[0], &state, nil)
		_, b := walkSWARPairsWithTypedCallMode(&m.Code[0], types[0], memoryProfile{}, types, nil, nil, 0, nil, nil, 0, 0, true, false, false, false, false, false, true, nil)
		if (a == nil) != (count == 100) || (b == nil) != (count == 100) {
			t.Fatal("dead signature budget", count, a, b)
		}
	}
}

func TestReferenceIndexedControlNeverCallable(t *testing.T) {
	for _, result := range []bool{false, true} {
		root := &wasm.CompType{Kind: wasm.CompFunc}
		if result {
			root.Results = []wasm.ValType{wasm.FuncRef}
		} else {
			root.Params = []wasm.ValType{wasm.FuncRef}
		}
		fn := &wasm.Func{BodyBytes: []byte{0, 0x0b}}
		state := moduleState{production: true, importReferences: true, referenceCalls: true}
		_, a := walkInteger(fn, root, &state, nil)
		_, b := walkSWARPairsWithTypedCallMode(fn, root, memoryProfile{}, nil, nil, nil, 0, nil, nil, 0, 0, true, false, false, false, false, false, true, nil)
		if !errors.Is(a, ErrUnsupported) || !strings.Contains(a.Error(), "numeric callable carrier") || !errors.Is(b, ErrUnsupported) || !strings.Contains(b.Error(), "pilot carrier type") {
			t.Fatal("reference-bearing root ABI", result, a, b)
		}
	}

	for _, op := range []byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15} {
		for _, dead := range []bool{false, true} {
			t.Run(fmt.Sprintf("op%x-dead%t", op, dead), func(t *testing.T) {
				body := []byte{op, 1}
				if op == 0x11 || op == 0x13 {
					body = append(body, 0)
				}
				if dead {
					body = append([]byte{0}, body...)
				}
				body = append(body, 0x0b)
				m, types, specs := referenceControlModule(t, body)
				state := moduleState{production: true, ownedTable: true, importReferences: true, referenceCalls: true, foreignTableCalls: true, localTailCalls: true, importTailCalls: true, dynamicTailCalls: true, types: specs, functions: specs[:2]}
				_, err := walkInteger(&m.Code[0], types[0], &state, nil)
				if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "numeric callable carrier") {
					t.Fatal("scalar control signature reached callable lowering", err)
				}
				table := &wasm.TableType{Ref: wasm.FuncRef.Ref(), Limits: wasm.Limits{Min: 1, Max: 1, HasMax: true}}
				functions := types[:1]
				if op == 0x10 || op == 0x12 {
					functions = types[:2]
				}
				_, err = walkSWARPairsWithTypedCallMode(&m.Code[0], types[0], memoryProfile{}, types, functions, nil, 0, table, make([]uint64, len(types)), 0, 0, true, false, true, false, true, true, true, nil)
				if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "call signature carrier") {
					t.Fatal("SWAR control signature reached callable lowering", err)
				}
			})
		}
	}
}
