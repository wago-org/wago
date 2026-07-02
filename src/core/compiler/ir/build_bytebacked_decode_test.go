package ir

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestBuildModuleFromByteBackedDecodedModule(t *testing.T) {
	data := module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{
		wasmtest.Code(bytes(0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b)),
	})
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("ValidateModule: %v", err)
	}

	im, err := BuildModule(m)
	if err != nil {
		t.Fatalf("BuildModule: %v", err)
	}
	if len(im.Funcs) != 1 {
		t.Fatalf("func len = %d, want 1", len(im.Funcs))
	}
	if err := VerifyFunc(&im.Funcs[0]); err != nil {
		t.Fatalf("VerifyFunc: %v", err)
	}
	got := FormatFunc(&im.Funcs[0])
	want := "func $0(i32) -> i32 {\n" +
		"b0():\n" +
		"  %0:i32 = local.get 0\n" +
		"  %1:i32 = const i32 1\n" +
		"  %2:i32 = ibinary.add %0, %1\n" +
		"  return %2\n" +
		"}\n"
	if got != want {
		t.Fatalf("IR dump mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestBuildModuleFromByteBackedDecodedModuleWithMetadata(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), // type 0: local/call_indirect target
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),                      // type 1: unused metadata shape
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),                      // type 2: host import
		)),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0x00, 0x02))),
		wasmtest.Section(3, append(wasmtest.ULEB(1), wasmtest.ULEB(0)...)),
		wasmtest.Section(4, []byte{0x01, 0x70, 0x00, 0x01}),
		wasmtest.Section(5, []byte{0x01, 0x00, 0x01}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0x07, 0x0b}))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(bytes(
			0x20, 0x00, // local.get 0
			0x10, 0x00, // call imported host(i32)->()
			0x20, 0x00, // argument to indirect local function
			0x20, 0x00, // table index
			0x11, 0x00, 0x00, // call_indirect type 0 table 0
			0x0b,
		)))),
		wasmtest.Section(11, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x03, 'x', 'y', 'z'})),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatalf("ValidateModule: %v", err)
	}
	if len(m.Code[0].Body.Instrs) != 0 || len(m.Code[0].BodyBytes) == 0 {
		t.Fatalf("function body materialization mismatch: instrs=%d bytes=%d", len(m.Code[0].Body.Instrs), len(m.Code[0].BodyBytes))
	}
	if got := m.Globals[0].Init.BodyBytes; len(got) == 0 {
		t.Fatalf("global initializer BodyBytes not populated")
	}
	if got := m.Elements[0].Mode.Offset.BodyBytes; len(got) == 0 {
		t.Fatalf("element offset BodyBytes not populated")
	}
	if got := m.Data[0].Mode.Offset.BodyBytes; len(got) == 0 {
		t.Fatalf("data offset BodyBytes not populated")
	}
	im, err := BuildModule(m)
	if err != nil {
		t.Fatalf("BuildModule: %v", err)
	}
	if err := VerifyModule(im); err != nil {
		t.Fatalf("VerifyModule: %v", err)
	}
	if len(im.Elements) != 1 || len(im.Data) != 1 || len(im.Globals) != 1 || im.ImportedFuncCount != 1 {
		t.Fatalf("metadata not lowered: imports=%d globals=%d elems=%d data=%d", im.ImportedFuncCount, len(im.Globals), len(im.Elements), len(im.Data))
	}
	if len(im.Tables) != 1 || im.Tables[0].Limits.Min != 1 {
		t.Fatalf("table metadata not lowered: %+v", im.Tables)
	}
	if e := im.Elements[0]; e.TableIdx != 0 || e.Passive || e.Declared || e.Len != 1 || e.ElemType != wasm.FuncRef {
		t.Fatalf("active element metadata = %+v", e)
	}
	if d := im.Data[0]; d.MemIdx != 0 || d.Passive || d.Len != 3 {
		t.Fatalf("active data metadata = %+v", d)
	}
	dump := FormatModule(im)
	for _, want := range []string{"call_import $0", "call_indirect type=0 table=0"} {
		if !strings.Contains(dump, want) {
			t.Fatalf("dump missing %q:\n%s", want, dump)
		}
	}
}

func TestBuildModuleFromByteBackedDecodedControlCoverage(t *testing.T) {
	cases := []struct {
		name    string
		data    []byte
		needles []string
	}{
		{
			name: "block_loop_br_br_if",
			data: module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{
				wasmtest.Code(bytes(
					0x02, 0x40, // block
					0x03, 0x40, // loop
					0x20, 0x00, // local.get 0
					0x0d, 0x01, // br_if outer block
					0x0c, 0x00, // br loop
					0x0b, // end loop
					0x0b, // end block
					0x41, 0x01,
					0x0b,
				)),
			}),
			needles: []string{"condbr", "br b"},
		},
		{
			name: "if_else_result",
			data: module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{
				wasmtest.Code(bytes(0x20, 0x00, 0x04, wasm.MustEncodeValType(wasm.I32), 0x41, 0x01, 0x05, 0x41, 0x02, 0x0b, 0x0b)),
			}),
			needles: []string{"condbr", "br b", "return %"},
		},
		{
			name: "br_table",
			data: module([]wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{
				wasmtest.Code(bytes(0x02, wasm.MustEncodeValType(wasm.I32), 0x02, wasm.MustEncodeValType(wasm.I32), 0x41, 0x09, 0x41, 0x00, 0x0e, 0x01, 0x00, 0x01, 0x0b, 0x0b, 0x0b)),
			}),
			needles: []string{"switch %", "default:b"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := decodeValidate(t, tc.data)
			assertRawFunctionBodies(t, m)
			assertBuilds(t, m, tc.needles...)
		})
	}
}

func TestBuildModuleFromByteBackedDecodedCallsGlobalsAndMemory(t *testing.T) {
	t.Run("direct_call", func(t *testing.T) {
		m := decodeValidate(t, module([]wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}}, []uint32{0, 0}, nil, nil, nil, [][]byte{
			wasmtest.Code(bytes(0x41, 0x03, 0x0b)),
			wasmtest.Code(bytes(0x10, 0x00, 0x0b)),
		}))
		assertRawFunctionBodies(t, m)
		assertBuilds(t, m, "call $0")
	})

	t.Run("global_get_set_memory_size_grow", func(t *testing.T) {
		glob := []global{{typ: wasm.GlobalType{Type: wasm.I32, Mutable: true}, init: bytes(0x41, 0x00, 0x0b)}}
		m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}, glob, [][]byte{
			wasmtest.Code(bytes(
				0x20, 0x00, 0x24, 0x00, // global.set 0
				0x3f, 0x00, // memory.size 0
				0x20, 0x00, 0x40, 0x00, // memory.grow 0
				0x6a,       // add size + previous size
				0x23, 0x00, // global.get 0
				0x6a,
				0x0b,
			)),
		}))
		assertRawFunctionBodies(t, m)
		assertBuilds(t, m, "global.set 0", "global.get 0", "memory.size mem=0", "memory.grow mem=0")
	})

	t.Run("memory_copy_fill", func(t *testing.T) {
		m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}}}, []uint32{0}, nil, []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}, nil, [][]byte{
			wasmtest.Code(bytes(
				0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x00, 0x00,
				0x20, 0x00, 0x41, 0xff, 0x01, 0x20, 0x02, 0xfc, 0x0b, 0x00,
				0x0b,
			)),
		}))
		assertRawFunctionBodies(t, m)
		assertBuilds(t, m, "memory.copy dstmem=0 srcmem=0", "memory.fill mem=0")
	})
}

func assertRawFunctionBodies(t *testing.T, m *wasm.Module) {
	t.Helper()
	for i := range m.Code {
		if len(m.Code[i].Body.Instrs) != 0 || len(m.Code[i].BodyBytes) == 0 {
			t.Fatalf("function %d body materialization mismatch: instrs=%d bytes=%d", i, len(m.Code[i].Body.Instrs), len(m.Code[i].BodyBytes))
		}
	}
}
