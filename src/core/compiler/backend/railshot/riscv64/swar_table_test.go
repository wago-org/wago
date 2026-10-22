//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func swarTableFixture() fixtureFunc {
	return fixtureFunc{params: []byte{0x7b, 0x7f, 0x7f}, results: []byte{0x7b, 0x7f, 0x7f, 0x7f}, locals: []byte{2, 1, 0x70, 124, 0x7b}, body: []byte{
		0x20, 0, 0x20, 1, 0x25, 0, 0xd1,
		0x20, 1, 0xd2, 1, 0xd0, 0x70, 0x20, 2, 0x1c, 1, 0x70, 0x22, 3, 0x26, 0,
		0x20, 1, 0x25, 0, 0xd1, 0x20, 3, 0xd1, 0x0b,
	}}
}

func swarTableSource(f fixtureFunc) []byte {
	leaf := fixtureFunc{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x0b}, unexported: true}
	data := binaryModule(f, leaf)
	table := uleb([]byte{1, 0x70, 1}, 1024)
	table = uleb(table, 1024)
	data = modifySection(data, 7, 4, table, false)
	// Legacy declarative elements declare both local ref.func targets without
	// creating table initialization or an execution-time element descriptor.
	return modifySection(data, 10, 9, []byte{1, 3, 0, 2, 0, 1}, false)
}

func swarValidatedTableFunction(t *testing.T, f fixtureFunc) (*wasm.Func, *wasm.CompType, []*wasm.CompType, *wasm.TableType) {
	t.Helper()
	m, err := wasm.DecodeModule(swarTableSource(f))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 128}); err != nil {
		t.Fatal(err)
	}
	functions := make([]*wasm.CompType, len(m.Code))
	for i := range functions {
		functions[i], _ = m.LocalFuncType(i)
	}
	return &m.Code[0], functions[0], functions, &m.Tables[0].Type
}

func TestSWAROwnedTableReferenceFunctionAdmission(t *testing.T) {
	f := swarTableFixture()
	fn, ft, functions, table := swarValidatedTableFunction(t, f)
	code, info, calls, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, table, emissionMode{production: true})
	if err != nil || len(code) == 0 || info.FrameBytes <= 2048 || calls != nil || info.FrameHeaderBytes != callFrameHeader {
		t.Fatal("mixed reference/table lowering", info, err)
	}
	if code, _, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, table, emissionMode{}); code != nil || err == nil {
		t.Fatal("isolated table owner", err)
	}
	if code, _, _, err := compileSWARPairsWithCalls(fn, ft, nil, nil, functions, emissionMode{production: true}); code != nil || err == nil {
		t.Fatal("reference local without owner", err)
	}
	if image, err := compileSWARModule(swarTableSource(f), emissionMode{production: true}); image != nil || err == nil {
		t.Fatal("whole-module table/element gate widened", err)
	}
	for _, bad := range []*wasm.TableType{
		{Ref: wasm.ExternRef.Ref()}, {Ref: table.Ref, Limits: wasm.Limits{Addr64: true}},
		{Ref: table.Ref, Limits: wasm.Limits{Min: 1025}},
		{Ref: table.Ref, Limits: wasm.Limits{Min: 2, HasMax: true, Max: 1}},
		{Ref: table.Ref, Limits: wasm.Limits{HasMax: true, Max: 1025}},
	} {
		if code, _, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, bad, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("invalid unused table metadata", bad, err)
		}
	}
	// References cannot enter through numeric/raw public call signatures or
	// global cells, nor escape through an integer or reference root return.
	for _, sig := range []*wasm.CompType{
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}},
		{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.FuncRef}},
	} {
		if code, _, _, err := compileSWARPairsWithTable(&wasm.Func{BodyBytes: []byte{0, 0x0b}}, sig, nil, nil, nil, nil, 0, table, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("reference signature admitted", err)
		}
	}
}

func TestSWAROwnedReferenceControlAndDeadTyping(t *testing.T) {
	f := fixtureFunc{params: []byte{0x7b, 0x7f}, results: []byte{0x7b, 0x7f}, body: []byte{0x20, 0, 0x20, 1, 4, 0x70, 0xd2, 1, 5, 0xd0, 0x70, 0x0b, 0xd1, 0x0b}}
	fn, ft, functions, table := swarValidatedTableFunction(t, f)
	if code, _, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, table, emissionMode{production: true}); err != nil || len(code) == 0 {
		t.Fatal("inline reference if join", err)
	}
	baselineBody := []byte{0x20, 0, 0x41, 0, 0x0f, 0x0b}
	fn.BodyBytes = baselineBody
	baseline, info, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, table, emissionMode{production: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, tail := range [][]byte{{0xd0, 0x70, 0x1a}, {0xd1, 0x1a}, {0xd2, 1, 0x1a}, {0x25, 0, 0x1a}, {0x26, 0}, {2, 0x70, 0xd0, 0x70, 0x0b, 0x1a}, {0xfc, 16, 0, 0x1a}} {
		fn.BodyBytes = append(append([]byte(nil), baselineBody[:len(baselineBody)-1]...), append(tail, 0x0b)...)
		code, actual, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, table, emissionMode{production: true})
		if err != nil || !bytes.Equal(code, baseline) || actual.FrameBytes != info.FrameBytes || actual.SpillSlots != info.SpillSlots {
			t.Fatal("dead reference ghost code/frame", tail, err)
		}
	}
	for _, tail := range [][]byte{
		{0xd0, 0x6f, 0x1a}, {0xd2, 2, 0x1a}, {0x25, 1, 0x1a}, {0x26, 1},
		{0x42, 0, 0xd1, 0x1a}, {0xd0, 0x70, 0x50, 0x1a},
		{2, 0x40, 0xd1, 0x1a, 0x0b},
		{0xd0, 0x70, 0xd0, 0x70, 0x41, 0, 0x1b, 0x1a}, // reference select must be typed
		{0x11, 0, 0, 0x1a},                            // indirect identity directory remains required
	} {
		fn.BodyBytes = append(append([]byte(nil), baselineBody[:len(baselineBody)-1]...), append(tail, 0x0b)...)
		if code, _, _, err := compileSWARPairsWithTable(fn, ft, nil, nil, functions, nil, 0, table, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("invalid dead reference class/index/shape", tail, err)
		}
	}
}
