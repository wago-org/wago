package railssa

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestStackInstrIsDense(t *testing.T) {
	if got, want := unsafe.Sizeof(StackInstr{}), uintptr(16); got != want {
		t.Fatalf("StackInstr size = %d bytes, want %d", got, want)
	}
}

func scalarModule(params, results []wasm.ValType, body []byte) *wasm.Module {
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results)))
	funcSec := wasmtest.Section(3, wasmtest.Vec([]byte{0}))
	codeSec := wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body)))
	m, err := wasm.DecodeModule(wasmtest.Module(typeSec, funcSec, codeSec))
	if err != nil {
		panic(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		panic(err)
	}
	return m
}

func TestBuildVerifyAndEval(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, []byte{0x20, 0x00, 0x20, 0x01, 0x7c, 0x42, 0x03, 0x85, 0x0b})
	fn, err := BuildFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(fn.Values), 5; got != want {
		t.Fatalf("values = %d, want %d", got, want)
	}
	got, err := Eval(fn, []uint64{10, 5})
	if err != nil {
		t.Fatal(err)
	}
	if got != 12 {
		t.Fatalf("eval = %d, want 12", got)
	}
}

func TestBuildRejectsUnsupportedControlFlow(t *testing.T) {
	m := scalarModule(nil, nil, []byte{0x02, 0x40, 0x0b, 0x0b})
	if _, err := BuildFunc(m, 0); err == nil {
		t.Fatal("block accepted by straight-line MVP")
	}
}

func TestBuildStackFuncRetainsV128TypeAndSIMDDescriptor(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0xfd, 0xe4, 0x00, // i8x16.bitmask
		0x0b,
	})
	f, _, flow, semantic := buildSemanticTest(t, m)
	if len(f.Locals) != 1 || f.Locals[0] != wasm.V128 || f.MaxStack != 1 {
		t.Fatalf("locals=%v max-stack=%d", f.Locals, f.MaxStack)
	}
	d, ok := f.SIMDImmediateAt(1)
	if !ok || d.Kind != wasm.InstrI8x16Bitmask || d.Class != wasm.SIMDEffectReduceI32 {
		t.Fatalf("SIMD descriptor=%#v ok=%v", d, ok)
	}
	result := semantic.Insts[semantic.InstructionMap[1]-1].Result
	if result == 0 || flow.Values[result].Type != wasm.I32 {
		t.Fatalf("SIMD result v%d type=%s", result, flow.Values[result].Type)
	}
}

func TestBuildStackFuncRetainsReferenceValuesWithoutWideningInstructions(t *testing.T) {
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("identity")...)
	importEntry = append(importEntry, 0)
	importEntry = append(importEntry, wasmtest.ULEB(0)...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	f, err := BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !f.HasReferences || len(f.Instrs) != 3 || len(f.MultiResults) != 1 || len(f.ResultTypes) != 1 {
		t.Fatalf("reference stack function: has-refs=%v instructions=%d sparse=%#v types=%v", f.HasReferences, len(f.Instrs), f.MultiResults, f.ResultTypes)
	}
	if got, ok := f.InstructionResultType(1, f.Instrs[1], 0); !ok || got != wasm.FuncRef {
		t.Fatalf("call result type = %s, ok=%v", got, ok)
	}
	cfg, err := BuildCFG(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	locals, err := BuildLocalSSA(f, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := BuildValueFlow(f, cfg, locals, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := flow.InstructionValues[1]
	if result == 0 || flow.Values[result].Type != wasm.FuncRef {
		t.Fatalf("reference call result = v%d %#v", result, flow.Values[result])
	}
	if got, want := unsafe.Sizeof(StackInstr{}), uintptr(16); got != want {
		t.Fatalf("StackInstr size = %d bytes after reference admission, want %d", got, want)
	}
}

func TestBuildStackFuncRetainsExactRefFuncType(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd2, 0x00, 0xd1, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	f, err := BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	if got, ok := f.InstructionResultType(0, f.Instrs[0], 0); !ok || !wasm.EqualValType(got, want) {
		t.Fatalf("ref.func result type = %s, ok=%v; want %s", got, ok, want)
	}
	cfg, err := BuildCFG(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	locals, err := BuildLocalSSA(f, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := BuildValueFlow(f, cfg, locals, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := flow.InstructionValues[0]
	if result == 0 || !wasm.EqualValType(flow.Values[result].Type, want) {
		t.Fatalf("ref.func flow result = v%d %s; want %s", result, flow.Values[result].Type, want)
	}
}
