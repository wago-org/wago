package ir

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBuildUnsupportedValidatedButNonLoweredOpsReturnClearErrors(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want string
	}{
		{"memory_init", bytes(0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x08, 0x00, 0x00, 0x0b), "unsupported 0xfc opcode 8"},
		{"data_drop", bytes(0xfc, 0x09, 0x00, 0x0b), "unsupported 0xfc opcode 9"},
		{"ref_null", bytes(0xd0, 0x70, 0x0b), "unsupported opcode 0xd0"},
		{"ref_is_null", bytes(0xd1, 0x0b), "unsupported opcode 0xd1"},
		{"ref_func", bytes(0xd2, 0x00, 0x0b), "unsupported opcode 0xd2"},
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

func TestBuildRejectsHugeBrTableCountBeforeAllocating(t *testing.T) {
	// The declared vector count is far larger than the remaining bytecode. BuildFunc
	// must reject it before allocating a []uint32 sized from untrusted input.
	_, err := BuildFunc(rawModule(wasm.FuncType{Params: []wasm.ValType{wasm.I32}}, bytes(0x20, 0x00, 0x0e, 0xff, 0xff, 0xff, 0xff, 0x0f)), 0)
	if err == nil || !strings.Contains(err.Error(), "br_table label count") {
		t.Fatalf("BuildFunc error = %v, want bounded br_table count error", err)
	}
}

func TestBuildRejectsReachablePopBelowControlFrameHeight(t *testing.T) {
	body := bytes(0x20, 0x00, 0x02, 0x40, 0x41, 0x00, 0x6a, 0x0b, 0x0b)
	_, err := BuildFunc(rawModule(wasm.FuncType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}, body), 0)
	if err == nil || !strings.Contains(err.Error(), "stack underflow") {
		t.Fatalf("BuildFunc error = %v, want stack underflow", err)
	}
}

func TestBuildRejectsInvalidMemoryAlignmentBeforePacking(t *testing.T) {
	m := rawModule(wasm.FuncType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}, bytes(0x20, 0x00, 0x28, 0x80, 0x02, 0x00, 0x0b))
	m.Memories = []wasm.MemType{{}}
	_, err := BuildFunc(m, 0)
	if err == nil || !strings.Contains(err.Error(), "invalid memory alignment") {
		t.Fatalf("BuildFunc error = %v, want invalid memory alignment", err)
	}
}

func TestBuildTruncatedImmediatesReturnErrors(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{"local_get_index", bytes(0x20)},
		{"global_get_index", bytes(0x23)},
		{"call_index", bytes(0x10)},
		{"call_indirect_type", bytes(0x11)},
		{"call_indirect_table", bytes(0x11, 0x00)},
		{"br_depth", bytes(0x0c)},
		{"br_if_depth", bytes(0x41, 0x00, 0x0d)},
		{"br_table_count", bytes(0x41, 0x00, 0x0e)},
		{"br_table_entry", bytes(0x41, 0x00, 0x0e, 0x01)},
		{"br_table_default", bytes(0x41, 0x00, 0x0e, 0x00)},
		{"block_type", bytes(0x02)},
		{"i32_const", bytes(0x41, 0x80)},
		{"i64_const", bytes(0x42, 0x80)},
		{"f32_const", bytes(0x43, 0x00, 0x00)},
		{"f64_const", bytes(0x44, 0x00, 0x00, 0x00)},
		{"mem_align", bytes(0x20, 0x00, 0x28)},
		{"mem_offset", bytes(0x20, 0x00, 0x28, 0x00)},
		{"memory_size_memidx", bytes(0x3f)},
		{"memory_grow_memidx", bytes(0x41, 0x00, 0x40)},
		{"select_t_count", bytes(0x1c)},
		{"select_t_type", bytes(0x1c, 0x01)},
		{"fc_subopcode", bytes(0xfc)},
		{"fc_memory_copy_dst", bytes(0xfc, 0x0a)},
		{"fc_memory_copy_src", bytes(0xfc, 0x0a, 0x00)},
		{"fc_memory_fill_mem", bytes(0xfc, 0x0b)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildFunc(rawModule(wasm.FuncType{}, tc.body), 0)
			if err == nil {
				t.Fatal("BuildFunc unexpectedly succeeded")
			}
		})
	}
}

func TestBuildModuleFunctionIndexMetadataWithImports(t *testing.T) {
	m := &wasm.Module{
		Types: []wasm.RecType{recFuncType(wasm.FuncType{Results: []wasm.ValType{wasm.I32}}), recFuncType(wasm.FuncType{})},
		Imports: []wasm.Import{
			{Type: wasm.ExternType{Kind: wasm.ExternGlobal, Global: wasm.GlobalType{Type: wasm.I32}}},
			{Type: wasm.ExternType{Kind: wasm.ExternFunc, Type: wasm.TypeIdx{Index: 0}}},
			{Type: wasm.ExternType{Kind: wasm.ExternFunc, Type: wasm.TypeIdx{Index: 1}}},
		},
		FuncTypes: []wasm.TypeIdx{{Index: 0}, {Index: 1}},
		Code: []wasm.Func{
			{BodyBytes: bytes(0x41, 0x01, 0x0b)},
			{BodyBytes: bytes(0x0b)},
		},
	}
	im, err := BuildModule(m)
	if err != nil {
		t.Fatal(err)
	}
	if im.ImportedFuncCount != 2 {
		t.Fatalf("ImportedFuncCount=%d, want 2", im.ImportedFuncCount)
	}
	if len(im.FuncTypes) != 4 || im.FuncTypes[0] != 0 || im.FuncTypes[1] != 1 || im.FuncTypes[2] != 0 || im.FuncTypes[3] != 1 {
		t.Fatalf("FuncTypes=%v", im.FuncTypes)
	}
	if im.Funcs[0].Index != 2 || im.Funcs[1].Index != 3 || im.Funcs[0].LocalIndex != 0 || im.Funcs[1].LocalIndex != 1 {
		t.Fatalf("bad function indexes: f0=(%d,%d) f1=(%d,%d)", im.Funcs[0].Index, im.Funcs[0].LocalIndex, im.Funcs[1].Index, im.Funcs[1].LocalIndex)
	}
}
