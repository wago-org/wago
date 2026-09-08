package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestGCSyncHostSlotCapacityRejectsU16Overflow(t *testing.T) {
	fields := make([]gc.FieldDesc, 1<<15)
	for i := range fields {
		fields[i].Kind = gc.StorageV128
	}
	_, err := gcSyncHostSlotCapacity([]gc.TypeDesc{{Kind: gc.KindStruct, Fields: fields}})
	if err == nil || !strings.Contains(err.Error(), "uint16") {
		t.Fatalf("u16 overflow error = %v", err)
	}
}

func TestModuleImportPrepassesKeepFunctionNamespace(t *testing.T) {
	wideParams := make([]wasm.ValType, coreruntime.MaxHostArity+1)
	for i := range wideParams {
		wideParams[i] = wasm.I32
	}
	m := &wasm.Module{
		Types: []wasm.RecType{
			{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc}}}},
			{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompFunc, Params: wideParams, Results: []wasm.ValType{wasm.AnyRef}}}}},
		},
		Imports: []wasm.Import{
			{Type: wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.I32})},
			{Type: wasm.NewFuncExternType(wasm.TypeIdx{Index: 0})},
			{Type: wasm.NewMemExternType(wasm.MemType{Limits: wasm.Limits{Min: 1}})},
			{Type: wasm.NewFuncExternType(wasm.TypeIdx{Index: 1})},
			{Type: wasm.NewTableExternType(wasm.TableType{Ref: wasm.FuncRef.Ref()})},
		},
	}
	if got, err := moduleSyncHostSlotCapacity(m); err != nil || got != coreruntime.MaxHostArity+1 {
		t.Fatalf("interleaved import slot capacity = %d, %v", got, err)
	}
	if !moduleHasCollectorReferenceCallBoundary(m) {
		t.Fatal("interleaved collector-reference function import was not found")
	}

	m.Imports[3].Type = wasm.NewFuncExternType(wasm.TypeIdx{Index: 99})
	if _, err := moduleSyncHostSlotCapacity(m); err == nil || !strings.Contains(err.Error(), "imported function 1") {
		t.Fatalf("interleaved invalid function error = %v", err)
	}
}

func TestCompileImportSignaturesKeepInterleavedOrder(t *testing.T) {
	globalImport := append(append(wasmtest.Name("env"), wasmtest.Name("g")...), byte(wasm.ExternGlobal), byte(wasm.NumI32), byte(wasm.Const))
	firstFunc := append(append(wasmtest.Name("env"), wasmtest.Name("first")...), byte(wasm.ExternFunc), 0)
	memoryImport := append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), byte(wasm.ExternMem), 0, 1)
	secondFunc := append(append(wasmtest.Name("env"), wasmtest.Name("second")...), byte(wasm.ExternFunc), 1)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.F64}),
		)),
		wasmtest.Section(2, wasmtest.Vec(globalImport, firstFunc, memoryImport, secondFunc)),
	)
	compiled, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if len(compiled.importFuncSigs) != 2 {
		t.Fatalf("import signatures = %d, want 2", len(compiled.importFuncSigs))
	}
	first, second := compiled.importFuncSigs[0], compiled.importFuncSigs[1]
	if len(first.Params) != 1 || first.Params[0] != ValI32 || len(first.Results) != 0 || !first.HasTypeIndex || first.TypeIndex != 0 {
		t.Fatalf("first import signature = %#v", first)
	}
	if len(second.Params) != 1 || second.Params[0] != ValI64 || len(second.Results) != 1 || second.Results[0] != ValF64 || !second.HasTypeIndex || second.TypeIndex != 1 {
		t.Fatalf("second import signature = %#v", second)
	}
}
