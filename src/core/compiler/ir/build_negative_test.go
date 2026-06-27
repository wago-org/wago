package ir

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBuildMalformedBodiesReturnErrors(t *testing.T) {
	tests := []struct {
		name string
		m    *wasm.Module
		want string
	}{
		{"missing_end", rawModule(wasm.FuncType{}, bytes()), "missing end"},
		{"trailing_bytes_after_end", rawModule(wasm.FuncType{}, bytes(0x0b, 0x01)), "trailing bytes"},
		{"unknown_local", rawModule(wasm.FuncType{}, bytes(0x20, 0x00, 0x0b)), "unknown local"},
		{"unknown_global", rawModule(wasm.FuncType{}, bytes(0x23, 0x00, 0x0b)), "unknown global"},
		{"unknown_func_call", rawModule(wasm.FuncType{}, bytes(0x10, 0x01, 0x0b)), "unknown function"},
		{"unknown_call_indirect_table", rawModule(wasm.FuncType{}, bytes(0x41, 0x00, 0x11, 0x00, 0x00, 0x0b)), "unknown table"},
		{"unknown_call_indirect_type", &wasm.Module{Types: []wasm.FuncType{{}}, Functions: []uint32{0}, Tables: []wasm.TableType{{Elem: wasm.FuncRef}}, Code: []wasm.Code{{Body: bytes(0x41, 0x00, 0x11, 0x09, 0x00, 0x0b)}}}, "unknown type"},
		{"call_indirect_non_funcref_table", &wasm.Module{Types: []wasm.FuncType{{}}, Functions: []uint32{0}, Tables: []wasm.TableType{{Elem: wasm.ExternRef}}, Code: []wasm.Code{{Body: bytes(0x41, 0x00, 0x11, 0x00, 0x00, 0x0b)}}}, "element type"},
		{"load_without_memory", rawModule(wasm.FuncType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}, bytes(0x20, 0x00, 0x28, 0x00, 0x00, 0x0b)), "unknown memory"},
		{"memory_size_without_memory", rawModule(wasm.FuncType{Results: []wasm.ValType{wasm.I32}}, bytes(0x3f, 0x00, 0x0b)), "unknown memory"},
		{"memory_size_nonzero_memory", &wasm.Module{Types: []wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}}, Functions: []uint32{0}, Memories: []wasm.MemType{{}}, Code: []wasm.Code{{Body: bytes(0x3f, 0x01, 0x0b)}}}, "multi-memory unsupported"},
		{"memory_copy_without_memory", rawModule(wasm.FuncType{Params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}}, bytes(0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x00, 0x00, 0x0b)), "unknown memory"},
		{"immutable_global_set", &wasm.Module{Types: []wasm.FuncType{{}}, Functions: []uint32{0}, Globals: []wasm.Global{{Type: wasm.GlobalType{Val: wasm.I32, Mutable: false}}}, Code: []wasm.Code{{Body: bytes(0x41, 0x00, 0x24, 0x00, 0x0b)}}}, "immutable global"},
		{"invalid_block_type", rawModule(wasm.FuncType{}, bytes(0x02, 0x02, 0x0b, 0x0b)), "invalid block type"},
		{"block_ended_by_else", rawModule(wasm.FuncType{}, bytes(0x02, 0x40, 0x05, 0x0b)), "block ended by else"},
		{"loop_ended_by_else", rawModule(wasm.FuncType{}, bytes(0x03, 0x40, 0x05, 0x0b)), "loop ended by else"},
		{"unreachable_if_without_else_type_mismatch", rawModule(wasm.FuncType{}, bytes(0x00, 0x04, byte(wasm.I32), 0x0b, 0x0b)), "if without else type mismatch"},
		{"bad_select_arity", rawModule(wasm.FuncType{Params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}, bytes(0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x1c, 0x02, byte(wasm.I32), byte(wasm.I32), 0x0b)), "select result arity"},
		{"bad_fc_subopcode", rawModule(wasm.FuncType{}, bytes(0xfc, 0x09, 0x00, 0x0b)), "unsupported 0xfc opcode"},
		{"stack_underflow", rawModule(wasm.FuncType{Results: []wasm.ValType{wasm.I32}}, bytes(0x0b)), "stack underflow"},
		{"leftover_stack", rawModule(wasm.FuncType{}, bytes(0x41, 0x00, 0x0b)), "leftover stack"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildFunc(tc.m, 0)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("BuildFunc error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestBuildRejectsMultiMemoryModule(t *testing.T) {
	m := &wasm.Module{Types: []wasm.FuncType{{}}, Functions: []uint32{0}, Memories: []wasm.MemType{{}, {}}, Code: []wasm.Code{{Body: bytes(0x0b)}}}
	_, err := BuildModule(m)
	if err == nil || !strings.Contains(err.Error(), "multi-memory unsupported") {
		t.Fatalf("BuildModule error = %v, want multi-memory unsupported", err)
	}
}

func TestBuildMalformedBranchErrors(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want string
	}{
		{"unknown_br_label", bytes(0x0c, 0x01, 0x0b), "unknown label"},
		{"unknown_br_if_label", bytes(0x41, 0x00, 0x0d, 0x01, 0x0b), "unknown label"},
		{"unknown_br_table_label", bytes(0x41, 0x00, 0x0e, 0x01, 0x01, 0x00, 0x0b), "unknown label"},
		{"unreachable_unknown_br_table_label", bytes(0x00, 0x41, 0x00, 0x0e, 0x01, 0x01, 0x00, 0x0b), "unknown label"},
		{"br_table_type_mismatch", bytes(0x02, byte(wasm.I32), 0x02, byte(wasm.I64), 0x41, 0x01, 0x41, 0x00, 0x0e, 0x01, 0x00, 0x01, 0x0b, 0x0b, 0x0b), "br_table label type mismatch"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildFunc(rawModule(wasm.FuncType{}, tc.body), 0)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("BuildFunc error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestBuildFuncWorksWithoutPriorValidationForSimpleValidShape(t *testing.T) {
	f, err := BuildFunc(rawModule(wasm.FuncType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}, bytes(0x20, 0x00, 0x0b)), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyFunc(f); err != nil {
		t.Fatal(err)
	}
	if got := FormatFunc(f); !strings.Contains(got, "local.get 0") || !strings.Contains(got, "return %") {
		t.Fatalf("unexpected dump:\n%s", got)
	}
}

func rawModule(ft wasm.FuncType, body []byte) *wasm.Module {
	return &wasm.Module{Types: []wasm.FuncType{ft}, Functions: []uint32{0}, Code: []wasm.Code{{Body: body}}}
}
