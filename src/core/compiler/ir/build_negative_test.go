package ir

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBuildRejectsNilModule(t *testing.T) {
	if _, err := BuildModule(nil); err == nil || !strings.Contains(err.Error(), "nil wasm module") {
		t.Fatalf("BuildModule error = %v, want nil wasm module", err)
	}
	if _, err := BuildFunc(nil, 0); err == nil || !strings.Contains(err.Error(), "nil wasm module") {
		t.Fatalf("BuildFunc error = %v, want nil wasm module", err)
	}
}

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
		{"unknown_call_indirect_type", moduleWith(rawModule(wasm.FuncType{}, bytes(0x41, 0x00, 0x11, 0x09, 0x00, 0x0b)), func(m *wasm.Module) { m.Tables = []wasm.Table{{Type: wasm.TableType{Ref: wasm.FuncRef.Ref}}} }), "unknown type"},
		{"call_indirect_non_funcref_table", moduleWith(rawModule(wasm.FuncType{}, bytes(0x41, 0x00, 0x11, 0x00, 0x00, 0x0b)), func(m *wasm.Module) { m.Tables = []wasm.Table{{Type: wasm.TableType{Ref: wasm.ExternRef.Ref}}} }), "element type"},
		{"load_without_memory", rawModule(wasm.FuncType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}, bytes(0x20, 0x00, 0x28, 0x00, 0x00, 0x0b)), "unknown memory"},
		{"memory_size_without_memory", rawModule(wasm.FuncType{Results: []wasm.ValType{wasm.I32}}, bytes(0x3f, 0x00, 0x0b)), "unknown memory"},
		{"memory_size_nonzero_memory", moduleWith(rawModule(wasm.FuncType{Results: []wasm.ValType{wasm.I32}}, bytes(0x3f, 0x01, 0x0b)), func(m *wasm.Module) { m.Memories = []wasm.MemType{{}} }), "multi-memory unsupported"},
		{"memory_copy_without_memory", rawModule(wasm.FuncType{Params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}}, bytes(0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x00, 0x00, 0x0b)), "unknown memory"},
		{"immutable_global_set", moduleWith(rawModule(wasm.FuncType{}, bytes(0x41, 0x00, 0x24, 0x00, 0x0b)), func(m *wasm.Module) {
			m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32, Mutable: false}}}
		}), "immutable global"},
		{"invalid_block_type", rawModule(wasm.FuncType{}, bytes(0x02, 0x02, 0x0b, 0x0b)), "invalid block type"},
		{"huge_block_type_index", rawModule(wasm.FuncType{}, bytes(0x02, 0x80, 0x80, 0x80, 0x80, 0x10, 0x0b, 0x0b)), "invalid block type index"},
		{"block_fallthrough_leftover", rawModule(wasm.FuncType{}, bytes(0x02, 0x40, 0x41, 0x00, 0x0b, 0x0b)), "block fallthrough"},
		{"block_ended_by_else", rawModule(wasm.FuncType{}, bytes(0x02, 0x40, 0x05, 0x0b)), "block ended by else"},
		{"loop_ended_by_else", rawModule(wasm.FuncType{}, bytes(0x03, 0x40, 0x05, 0x0b)), "loop ended by else"},
		{"unreachable_if_without_else_type_mismatch", rawModule(wasm.FuncType{}, bytes(0x00, 0x04, wasm.MustEncodeValType(wasm.I32), 0x0b, 0x0b)), "if without else type mismatch"},
		{"bad_select_arity", rawModule(wasm.FuncType{Params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}, bytes(0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x1c, 0x02, wasm.MustEncodeValType(wasm.I32), wasm.MustEncodeValType(wasm.I32), 0x0b)), "select result arity"},
		{"bad_fc_subopcode", rawModule(wasm.FuncType{}, bytes(0xfc, 0x0c, 0x00, 0x00, 0x0b)), "unsupported 0xfc opcode"},
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

func TestBuildRejectsUnsupportedInlineBlockResultType(t *testing.T) {
	m := rawModule(wasm.FuncType{}, bytes(0x02, wasm.MustEncodeValType(wasm.FuncRef), 0x0b, 0x0b))
	_, err := BuildFunc(m, 0)
	wantErr(t, err, "unsupported IR value type funcref")
}

func TestBuildRejectsUnsupportedTypeIndexedBlockSignature(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.RecType{
			recFuncType(wasm.FuncType{}),
			recFuncType(wasm.FuncType{Results: []wasm.ValType{wasm.FuncRef}}),
		},
		FuncTypes: []wasm.TypeIdx{{Index: 0}},
		Code:      []wasm.Func{{BodyBytes: bytes(0x02, 0x01, 0x0b, 0x0b)}},
	}
	_, err := BuildFunc(m, 0)
	wantErr(t, err, "unsupported IR value type funcref")
}

func TestBuildRejectsUnsupportedTypedSelectType(t *testing.T) {
	m := rawModule(wasm.FuncType{}, bytes(0x00, 0x1c, 0x01, wasm.MustEncodeValType(wasm.FuncRef), 0x0b))
	_, err := BuildFunc(m, 0)
	wantErr(t, err, "unsupported IR value type funcref")
}

func TestBuildRejectsUnsupportedCallSignature(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.RecType{
			recFuncType(wasm.FuncType{Params: []wasm.ValType{wasm.FuncRef}}),
			recFuncType(wasm.FuncType{}),
		},
		Imports:   []wasm.Import{{Type: wasm.ExternType{Kind: wasm.ExternFunc, Type: wasm.TypeIdx{Index: 0}}}},
		FuncTypes: []wasm.TypeIdx{{Index: 1}},
		Code:      []wasm.Func{{BodyBytes: bytes(0x10, 0x00, 0x0b)}},
	}
	_, err := BuildFunc(m, 0)
	wantErr(t, err, "unsupported IR value type funcref")
}

func TestBuildRejectsUnsupportedCallIndirectSignature(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.RecType{
			recFuncType(wasm.FuncType{Results: []wasm.ValType{wasm.FuncRef}}),
			recFuncType(wasm.FuncType{}),
		},
		FuncTypes: []wasm.TypeIdx{{Index: 1}},
		Tables:    []wasm.Table{{Type: wasm.TableType{Ref: wasm.FuncRef.Ref}}},
		Code:      []wasm.Func{{BodyBytes: bytes(0x41, 0x00, 0x11, 0x00, 0x00, 0x0b)}},
	}
	_, err := BuildFunc(m, 0)
	wantErr(t, err, "unsupported IR value type funcref")
}

func TestBuildRejectsUnsupportedGlobalType(t *testing.T) {
	m := rawModule(wasm.FuncType{}, bytes(0x00, 0x23, 0x00, 0x0b))
	m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.FuncRef}}}
	_, err := BuildFunc(m, 0)
	wantErr(t, err, "unsupported IR value type funcref")
}

func TestBuildRejectsMultiMemoryModule(t *testing.T) {
	m := moduleWith(rawModule(wasm.FuncType{}, bytes(0x0b)), func(m *wasm.Module) { m.Memories = []wasm.MemType{{}, {}} })
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
		{"br_table_type_mismatch", bytes(0x02, wasm.MustEncodeValType(wasm.I32), 0x02, wasm.MustEncodeValType(wasm.I64), 0x41, 0x01, 0x41, 0x00, 0x0e, 0x01, 0x00, 0x01, 0x0b, 0x0b, 0x0b), "br_table label type mismatch"},
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
	return &wasm.Module{Types: []wasm.RecType{recFuncType(ft)}, FuncTypes: []wasm.TypeIdx{{Index: 0}}, Code: []wasm.Func{{BodyBytes: append([]byte{}, body...)}}}
}

func recFuncType(ft wasm.FuncType) wasm.RecType {
	return wasm.RecType{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: ft.Params, Results: ft.Results}}}}
}

func moduleWith(m *wasm.Module, mutate func(*wasm.Module)) *wasm.Module {
	mutate(m)
	return m
}
