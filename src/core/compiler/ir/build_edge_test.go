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
		{"dead_block_merge", bytes(0x02, byte(wasm.I32), 0x00, 0x0b, 0x41, 0x01, 0x1a, 0x0b), []string{"trap", "b2(%"}},
		{"dead_loop_after", bytes(0x03, byte(wasm.I32), 0x00, 0x0b, 0x41, 0x01, 0x1a, 0x0b), []string{"trap", "b2(%"}},
		{"dead_if_arms_and_merge", bytes(0x00, 0x04, byte(wasm.I32), 0x41, 0x01, 0x05, 0x41, 0x02, 0x0b, 0x1a, 0x0b), []string{"trap", "b3(%"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := decodeValidate(t, module([]wasm.FuncType{}, nil, nil, nil, nil, nil))
			m.Types = []wasm.FuncType{{}}
			m.Functions = []uint32{0}
			m.Code = []wasm.Code{{Body: tc.body}}
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
	_, dump := buildOne(t, m)
	if !strings.Contains(dump, "condbr") || !strings.Contains(dump, "return %") || !strings.Contains(dump, "else b") {
		t.Fatalf("unexpected dump:\n%s", dump)
	}
}

func TestBuildBrTableToFunctionLabel(t *testing.T) {
	body := wasmtest.Code(bytes(0x41, 0x2a, 0x20, 0x00, 0x0e, 0x01, 0x00, 0x00, 0x0b))
	m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{body}))
	_, dump := buildOne(t, m)
	if !strings.Contains(dump, "switch %") || strings.Count(dump, "return %") < 2 {
		t.Fatalf("unexpected dump:\n%s", dump)
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
	m := decodeValidate(t, module(types, []uint32{1}, []wasm.TableType{{Elem: wasm.FuncRef, Limits: wasm.Limits{Min: 1}}}, nil, nil, [][]byte{wasmtest.Code(bytes(0x41, 0x00, 0x11, 0x01, 0x00, 0x0b))}))
	f, dump := buildOne(t, m)
	if !strings.Contains(dump, "call_indirect type=1 table=0 canon=0") {
		t.Fatalf("unexpected dump:\n%s", dump)
	}
	if got := f.Insts[len(f.Insts)-1].Aux2; got != 0 {
		t.Fatalf("canonical type id = %d, want 0", got)
	}
}

func TestBuildImportedMutableGlobalSet(t *testing.T) {
	m := &wasm.Module{Types: []wasm.FuncType{{}}, Imports: []wasm.Import{{Kind: wasm.ExternGlobal, Global: wasm.GlobalType{Val: wasm.I64, Mutable: true}}}, Functions: []uint32{0}, Code: []wasm.Code{{Body: bytes(0x42, 0x01, 0x24, 0x00, 0x0b)}}}
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

func TestBuildNoResultFunctionExplicitReturn(t *testing.T) {
	m := decodeValidate(t, module([]wasm.FuncType{{}}, []uint32{0}, nil, nil, nil, [][]byte{wasmtest.Code(bytes(0x0f, 0x0b))}))
	_, dump := buildOne(t, m)
	if !strings.Contains(dump, "return\n") {
		t.Fatalf("unexpected dump:\n%s", dump)
	}
}
