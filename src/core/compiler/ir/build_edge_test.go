package ir

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestBuildDeadStructuredBlocksStillVerify(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want []string
	}{
		{"dead_block_merge", bytes(0x02, wasm.MustEncodeValType(wasm.I32), 0x00, 0x0b, 0x41, 0x01, 0x1a, 0x0b), []string{"trap", "b2(%"}},
		{"dead_loop_after", bytes(0x03, wasm.MustEncodeValType(wasm.I32), 0x00, 0x0b, 0x41, 0x01, 0x1a, 0x0b), []string{"trap", "b2(%"}},
		{"dead_if_arms_and_merge", bytes(0x00, 0x04, wasm.MustEncodeValType(wasm.I32), 0x41, 0x01, 0x05, 0x41, 0x02, 0x0b, 0x1a, 0x0b), []string{"trap", "b3(%"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := rawModule(wasm.FuncType{}, tc.body)
			f, err := BuildFunc(m, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := VerifyFunc(f); err != nil {
				t.Fatal(err)
			}
			dump := FormatFunc(f)
			for _, want := range tc.want {
				if !strings.Contains(dump, want) {
					t.Fatalf("dump missing %q:\n%s", want, dump)
				}
			}
		})
	}
}

func TestBuildBranchToFunctionLabel(t *testing.T) {
	body := wasmtest.Code(bytes(0x41, 0x2a, 0x0f, 0x41, 0x00, 0x1a, 0x0b))
	m := decodeValidate(t, module([]wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{body}))
	f, dump := buildOne(t, m)
	if len(f.Blocks) != 1 || !strings.Contains(dump, "return %") || !strings.Contains(dump, "const i32 42") {
		t.Fatalf("unexpected dump:\n%s", dump)
	}
}

func TestBuildBrIfToFunctionLabelCreatesExplicitReturnEdge(t *testing.T) {
	body := wasmtest.Code(bytes(0x41, 0x2a, 0x20, 0x00, 0x0d, 0x00, 0x0b))
	m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{body}))
	f, dump := buildOne(t, m)
	if !strings.Contains(dump, "condbr") || !strings.Contains(dump, "return %") || !strings.Contains(dump, "else b") {
		t.Fatalf("unexpected dump:\n%s", dump)
	}
	if countSyntheticReturns(f) != 1 {
		t.Fatalf("synthetic return blocks = %d, want 1\n%s", countSyntheticReturns(f), dump)
	}
}

func TestBuildBrTableToFunctionLabelReusesSyntheticReturn(t *testing.T) {
	body := wasmtest.Code(bytes(0x41, 0x2a, 0x20, 0x00, 0x0e, 0x01, 0x00, 0x00, 0x0b))
	m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{body}))
	f, dump := buildOne(t, m)
	if !strings.Contains(dump, "switch %") || !strings.Contains(dump, "return %") {
		t.Fatalf("unexpected dump:\n%s", dump)
	}
	if countSyntheticReturns(f) != 1 {
		t.Fatalf("synthetic return blocks = %d, want 1\n%s", countSyntheticReturns(f), dump)
	}
}

func TestBuildCallMultipleParamsAndResults(t *testing.T) {
	types := []wasm.FuncType{{Params: []wasm.ValType{wasm.I32, wasm.I64}, Results: []wasm.ValType{wasm.I64, wasm.I32}}, {Results: []wasm.ValType{wasm.I64, wasm.I32}}}
	m := decodeValidate(t, module(types, []uint32{0, 1}, nil, nil, nil, [][]byte{
		wasmtest.Code(bytes(0x20, 0x01, 0x20, 0x00, 0x0b)),
		wasmtest.Code(bytes(0x41, 0x07, 0x42, 0x08, 0x10, 0x00, 0x0b)),
	}))
	im, err := BuildModule(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyModule(im); err != nil {
		t.Fatal(err)
	}
	dump := FormatFunc(&im.Funcs[1])
	if !strings.Contains(dump, "call $0") || !strings.Contains(dump, "return %") || im.Funcs[1].Insts[len(im.Funcs[1].Insts)-1].Results.Len != 2 {
		t.Fatalf("unexpected call multi-result dump:\n%s", dump)
	}
}

func TestBuildCallIndirectCanonicalTypeID(t *testing.T) {
	types := []wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}, {Results: []wasm.ValType{wasm.I32}}}
	m := decodeValidate(t, module(types, []uint32{1}, []wasm.TableType{{Ref: wasm.FuncRef.Ref, Limits: wasm.Limits{Min: 1}}}, nil, nil, [][]byte{wasmtest.Code(bytes(0x41, 0x00, 0x11, 0x01, 0x00, 0x0b))}))
	f, dump := buildOne(t, m)
	if !strings.Contains(dump, "call_indirect type=1 table=0 canon=0") {
		t.Fatalf("unexpected dump:\n%s", dump)
	}
	if got := f.Insts[len(f.Insts)-1].Aux2; got != 0 {
		t.Fatalf("canonical type id = %d, want 0", got)
	}
}

func TestBuildFuncTypeUsesFlattenedRecGroupSubtypeIndex(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.RecType{{SubTypes: []wasm.SubType{
			{Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct}},
			{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}},
		}}},
		FuncTypes: []wasm.TypeIdx{{Index: 1}},
		Code: []wasm.Func{{
			Body:      wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrLocalGet, Index: 0}}},
			BodyBytes: bytes(0x20, 0x00, 0x0b),
		}},
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("ValidateModule: %v", err)
	}
	im, err := BuildModule(m)
	if err != nil {
		t.Fatalf("BuildModule: %v", err)
	}
	if err := VerifyModule(im); err != nil {
		t.Fatalf("VerifyModule: %v", err)
	}
	if len(im.Types) != 2 || im.Funcs[0].TypeIndex != 1 || !sameTypes(im.Funcs[0].Sig.Params, []wasm.ValType{wasm.I32}) {
		t.Fatalf("flattened metadata mismatch: types=%d typeIndex=%d sig=%#v", len(im.Types), im.Funcs[0].TypeIndex, im.Funcs[0].Sig)
	}
}

func TestBuildRejectsDirectCallToNonFunctionTypeIndex(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.RecType{{SubTypes: []wasm.SubType{
			{Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct}},
			{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}},
		}}},
		FuncTypes: []wasm.TypeIdx{{Index: 0}, {Index: 1}},
		Code: []wasm.Func{
			{BodyBytes: bytes(0x0b)},
			{BodyBytes: bytes(0x10, 0x00, 0x0b)},
		},
	}
	_, err := BuildFunc(m, 1)
	if err == nil || !strings.Contains(err.Error(), "non-function type") {
		t.Fatalf("BuildFunc error = %v, want non-function type", err)
	}
}

func TestBuildFuncUsesFlattenedImportedMetadata(t *testing.T) {
	memMod := rawModule(wasm.FuncType{Results: []wasm.ValType{wasm.I32}}, bytes(0x3f, 0x00, 0x0b))
	memMod.Imports = []wasm.Import{{Type: wasm.ExternType{Kind: wasm.ExternMem, Mem: wasm.MemType{Limits: wasm.Limits{Min: 1}}}}}
	if f, err := BuildFunc(memMod, 0); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(FormatFunc(f), "memory.size mem=0") {
		t.Fatalf("unexpected memory import dump:\n%s", FormatFunc(f))
	}

	tableMod := rawModule(wasm.FuncType{}, bytes(0x41, 0x00, 0x11, 0x00, 0x00, 0x0b))
	tableMod.Imports = []wasm.Import{{Type: wasm.ExternType{Kind: wasm.ExternTable, Table: wasm.TableType{Ref: wasm.FuncRef.Ref, Limits: wasm.Limits{Min: 1}}}}}
	if f, err := BuildFunc(tableMod, 0); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(FormatFunc(f), "call_indirect type=0 table=0") {
		t.Fatalf("unexpected table import dump:\n%s", FormatFunc(f))
	}
}

func TestBuildImportedMutableGlobalSet(t *testing.T) {
	m := rawModule(wasm.FuncType{}, bytes(0x42, 0x01, 0x24, 0x00, 0x0b))
	m.Imports = []wasm.Import{{Type: wasm.ExternType{Kind: wasm.ExternGlobal, Global: wasm.GlobalType{Type: wasm.I64, Mutable: true}}}}
	f, err := BuildFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyFunc(f); err != nil {
		t.Fatal(err)
	}
	if dump := FormatFunc(f); !strings.Contains(dump, "global.set 0") {
		t.Fatalf("unexpected dump:\n%s", dump)
	}
}

func TestBuildBlockTypeForms(t *testing.T) {
	m := decodeValidate(t, module([]wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}, {Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{
		wasmtest.Code(bytes(0x41, 0x01, 0x02, 0x01, 0x0b, 0x0b)),
	}))
	_, dump := buildOne(t, m)
	if !strings.Contains(dump, "b1(%") || !strings.Contains(dump, "br b2") {
		t.Fatalf("unexpected dump:\n%s", dump)
	}
}

func countSyntheticReturns(f *Func) int {
	n := 0
	for i := range f.Blocks {
		if f.Blocks[i].Flags&BlockSyntheticReturn != 0 {
			n++
		}
	}
	return n
}

func TestBuildNoResultFunctionExplicitReturn(t *testing.T) {
	m := decodeValidate(t, module([]wasm.FuncType{{}}, []uint32{0}, nil, nil, nil, [][]byte{wasmtest.Code(bytes(0x0f, 0x0b))}))
	_, dump := buildOne(t, m)
	if !strings.Contains(dump, "return\n") {
		t.Fatalf("unexpected dump:\n%s", dump)
	}
}
