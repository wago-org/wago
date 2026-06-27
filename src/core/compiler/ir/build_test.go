package ir

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestBuildStraightLineArithmeticGolden(t *testing.T) {
	m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{
		wasmtest.Code(bytes(0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b)),
	}))
	f, err := BuildFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyFunc(f); err != nil {
		t.Fatal(err)
	}
	got := FormatFunc(f)
	want := "func $0(i32) -> i32 {\n" +
		"b0(%0:i32):\n" +
		"  %1:i32 = local.get 0\n" +
		"  %2:i32 = const i32 1\n" +
		"  %3:i32 = ibinary.add %1, %2\n" +
		"  return %3\n" +
		"}\n"
	if got != want {
		t.Fatalf("IR dump mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestBuildLocals(t *testing.T) {
	body := codeWithLocals([]wasm.LocalEntry{{Count: 1, Type: wasm.I32}}, bytes(0x41, 0x07, 0x22, 0x00, 0x20, 0x00, 0x6a, 0x0b))
	m := decodeValidate(t, module([]wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{body}))
	assertBuilds(t, m, "local.tee 0", "local.get 0")
}

func TestBuildIfElseWithResult(t *testing.T) {
	body := wasmtest.Code(bytes(0x20, 0x00, 0x04, byte(wasm.I32), 0x41, 0x01, 0x05, 0x41, 0x02, 0x0b, 0x0b))
	m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{body}))
	assertBuilds(t, m, "condbr", "br b3", "b3(%")
}

func TestBuildLoopWithBrIf(t *testing.T) {
	body := wasmtest.Code(bytes(0x03, 0x40, 0x20, 0x00, 0x0d, 0x00, 0x0b, 0x41, 0x01, 0x0b))
	m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{body}))
	assertBuilds(t, m, "condbr", "br b1")
}

func TestBuildBranchToOuterBlockWithValue(t *testing.T) {
	body := wasmtest.Code(bytes(0x02, byte(wasm.I32), 0x41, 0x2a, 0x0c, 0x00, 0x0b, 0x0b))
	m := decodeValidate(t, module([]wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{body}))
	assertBuilds(t, m, "br b2 %")
}

func TestBuildBrTable(t *testing.T) {
	body := wasmtest.Code(bytes(0x02, byte(wasm.I32), 0x02, byte(wasm.I32), 0x41, 0x09, 0x41, 0x00, 0x0e, 0x01, 0x00, 0x01, 0x0b, 0x0b, 0x0b))
	m := decodeValidate(t, module([]wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{body}))
	assertBuilds(t, m, "switch %", "default:b")
}

func TestBuildUnreachableStackPolymorphicCode(t *testing.T) {
	body := wasmtest.Code(bytes(0x00, 0x41, 0x01, 0x1a, 0x7c, 0x1a, 0x0b)) // unreachable; i32.const; drop; i64.add; drop; end
	m := decodeValidate(t, module([]wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{body}))
	assertBuilds(t, m, "trap")
}

func TestBuildCall(t *testing.T) {
	m := decodeValidate(t, module([]wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}}, []uint32{0, 0}, nil, nil, nil, [][]byte{
		wasmtest.Code(bytes(0x41, 0x03, 0x0b)),
		wasmtest.Code(bytes(0x10, 0x00, 0x0b)),
	}))
	assertBuilds(t, m, "call $0")
}

func TestBuildCallIndirect(t *testing.T) {
	m := decodeValidate(t, module([]wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, []wasm.TableType{{Elem: wasm.FuncRef, Limits: wasm.Limits{Min: 1}}}, nil, nil, [][]byte{
		wasmtest.Code(bytes(0x41, 0x00, 0x11, 0x00, 0x00, 0x0b)),
	}))
	assertBuilds(t, m, "call_indirect type=0 table=0 canon=0")
}

func TestBuildLoadStore(t *testing.T) {
	types := []wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}, {Params: []wasm.ValType{wasm.I32, wasm.I32}}}
	bodies := [][]byte{
		wasmtest.Code(bytes(0x20, 0x00, 0x28, 0x02, 0x08, 0x0b)),
		wasmtest.Code(bytes(0x20, 0x00, 0x20, 0x01, 0x36, 0x02, 0x0c, 0x0b)),
	}
	m := decodeValidate(t, module(types, []uint32{0, 1}, nil, []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}, nil, bodies))
	assertBuilds(t, m, "load.i32 offset=8 align=2 mem=0", "store.i32 offset=12 align=2 mem=0")
}

func TestBuildMemoryCopyFill(t *testing.T) {
	body := wasmtest.Code(bytes(
		0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x00, 0x00,
		0x20, 0x00, 0x41, 0xff, 0x01, 0x20, 0x02, 0xfc, 0x0b, 0x00,
		0x0b))
	m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}}}, []uint32{0}, nil, []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}, nil, [][]byte{body}))
	assertBuilds(t, m, "memory.copy dstmem=0 srcmem=0", "memory.fill mem=0")
}

func TestBuildGlobalGetSet(t *testing.T) {
	glob := []global{{typ: wasm.GlobalType{Val: wasm.I32, Mutable: true}, init: bytes(0x41, 0x00, 0x0b)}}
	body := wasmtest.Code(bytes(0x41, 0x05, 0x24, 0x00, 0x23, 0x00, 0x0b))
	m := decodeValidate(t, module([]wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, glob, [][]byte{body}))
	assertBuilds(t, m, "global.set 0", "global.get 0")
}

func TestBuildMultiResultBlockAndFunction(t *testing.T) {
	types := []wasm.FuncType{{Results: []wasm.ValType{wasm.I32, wasm.I64}}, {Results: []wasm.ValType{wasm.I32, wasm.I64}}}
	body := wasmtest.Code(bytes(0x02, 0x01, 0x41, 0x01, 0x42, 0x02, 0x0b, 0x0b)) // block type index 1
	m := decodeValidate(t, module(types, []uint32{0}, nil, nil, nil, [][]byte{body}))
	assertBuilds(t, m, "return %")
}

func TestBuildAllocsAreBounded(t *testing.T) {
	m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{
		wasmtest.Code(bytes(0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b)),
	}))
	allocs := testing.AllocsPerRun(100, func() {
		f, err := BuildFunc(m, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(f.Insts) == 0 {
			t.Fatal("empty ir")
		}
	})
	if allocs > 80 {
		t.Fatalf("BuildFunc allocations = %.1f, want <= 80", allocs)
	}
}

func assertBuilds(t *testing.T, m *wasm.Module, needles ...string) {
	t.Helper()
	im, err := BuildModule(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyModule(im); err != nil {
		t.Fatal(err)
	}
	dump := FormatModule(im)
	for _, n := range needles {
		if !strings.Contains(dump, n) {
			t.Fatalf("dump missing %q:\n%s", n, dump)
		}
	}
}

func decodeValidate(t *testing.T, data []byte) *wasm.Module {
	t.Helper()
	m, err := wasm.Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := wasm.Validate(m); err != nil {
		t.Fatalf("validate: %v", err)
	}
	return m
}

type global struct {
	typ  wasm.GlobalType
	init []byte
}

func module(types []wasm.FuncType, funcs []uint32, tables []wasm.TableType, mems []wasm.MemType, globals []global, codes [][]byte) []byte {
	var secs [][]byte
	if len(types) > 0 {
		var its [][]byte
		for _, ft := range types {
			its = append(its, wasmtest.FuncType(ft.Params, ft.Results))
		}
		secs = append(secs, wasmtest.Section(1, wasmtest.Vec(its...)))
	}
	if len(funcs) > 0 {
		payload := wasmtest.ULEB(uint32(len(funcs)))
		for _, x := range funcs {
			payload = append(payload, wasmtest.ULEB(x)...)
		}
		secs = append(secs, wasmtest.Section(3, payload))
	}
	if len(tables) > 0 {
		payload := wasmtest.ULEB(uint32(len(tables)))
		for _, tb := range tables {
			payload = append(payload, byte(tb.Elem))
			payload = appendLimits(payload, tb.Limits)
		}
		secs = append(secs, wasmtest.Section(4, payload))
	}
	if len(mems) > 0 {
		payload := wasmtest.ULEB(uint32(len(mems)))
		for _, mem := range mems {
			payload = appendLimits(payload, mem.Limits)
		}
		secs = append(secs, wasmtest.Section(5, payload))
	}
	if len(globals) > 0 {
		payload := wasmtest.ULEB(uint32(len(globals)))
		for _, g := range globals {
			mut := byte(0)
			if g.typ.Mutable {
				mut = 1
			}
			payload = append(payload, byte(g.typ.Val), mut)
			payload = append(payload, g.init...)
		}
		secs = append(secs, wasmtest.Section(6, payload))
	}
	if len(codes) > 0 {
		secs = append(secs, wasmtest.Section(10, wasmtest.Vec(codes...)))
	}
	return wasmtest.Module(secs...)
}

func codeWithLocals(locals []wasm.LocalEntry, instr []byte) []byte {
	body := wasmtest.ULEB(uint32(len(locals)))
	for _, l := range locals {
		body = append(body, wasmtest.ULEB(l.Count)...)
		body = append(body, byte(l.Type))
	}
	body = append(body, instr...)
	return append(wasmtest.ULEB(uint32(len(body))), body...)
}

func appendLimits(out []byte, l wasm.Limits) []byte {
	if l.HasMax {
		out = append(out, 0x01)
		out = append(out, wasmtest.ULEB(l.Min)...)
		return append(out, wasmtest.ULEB(l.Max)...)
	}
	out = append(out, 0x00)
	return append(out, wasmtest.ULEB(l.Min)...)
}
func bytes(bs ...byte) []byte { return bs }
