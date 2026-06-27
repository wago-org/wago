package ir

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestBuildModuleCopiesMetadata(t *testing.T) {
	type0 := wasm.FuncType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I64}}
	type1 := wasm.FuncType{Results: []wasm.ValType{wasm.I32}}
	m := &wasm.Module{
		Version: 1,
		Types:   []wasm.FuncType{type0, type1},
		Imports: []wasm.Import{
			{Kind: wasm.ExternFunc, TypeIndex: 0, Module: "env", Name: "f"},
			{Kind: wasm.ExternGlobal, Global: wasm.GlobalType{Val: wasm.I64}},
			{Kind: wasm.ExternMem, Mem: wasm.MemType{Limits: wasm.Limits{Min: 1, Max: 2, HasMax: true}}},
			{Kind: wasm.ExternTable, Table: wasm.TableType{Elem: wasm.FuncRef, Limits: wasm.Limits{Min: 3}}},
		},
		Functions: []uint32{1},
		Globals:   []wasm.Global{{Type: wasm.GlobalType{Val: wasm.I32, Mutable: true}}},
		Memories:  []wasm.MemType{{Limits: wasm.Limits{Min: 4}}},
		Tables:    []wasm.TableType{{Elem: wasm.FuncRef, Limits: wasm.Limits{Min: 5}}},
		Elements:  []wasm.Element{{TableIdx: 1, ElemType: wasm.FuncRef, FuncIdx: []uint32{0, 1}, Passive: true}},
		Data:      []wasm.DataSegment{{MemIdx: 1, Init: []byte{1, 2, 3}}},
		Code:      []wasm.Code{{Body: bytes(0x41, 0x00, 0x0b)}},
	}
	im, err := BuildModule(m)
	if err != nil {
		t.Fatal(err)
	}
	if im.ImportedFuncCount != 1 || len(im.FuncTypes) != 2 || im.FuncTypes[0] != 0 || im.FuncTypes[1] != 1 {
		t.Fatalf("bad func metadata: imports=%d types=%v", im.ImportedFuncCount, im.FuncTypes)
	}
	if len(im.Globals) != 2 || im.Globals[0].Val != wasm.I64 || !im.Globals[1].Mutable {
		t.Fatalf("bad global metadata: %+v", im.Globals)
	}
	if len(im.Memories) != 2 || im.Memories[0].Limits.Max != 2 || im.Memories[1].Limits.Min != 4 {
		t.Fatalf("bad memory metadata: %+v", im.Memories)
	}
	if len(im.Tables) != 2 || im.Tables[0].Limits.Min != 3 || im.Tables[1].Limits.Min != 5 {
		t.Fatalf("bad table metadata: %+v", im.Tables)
	}
	if len(im.Elements) != 1 || im.Elements[0].TableIdx != 1 || im.Elements[0].Len != 2 || !im.Elements[0].Passive {
		t.Fatalf("bad element metadata: %+v", im.Elements)
	}
	if len(im.Data) != 1 || im.Data[0].MemIdx != 1 || im.Data[0].Len != 3 {
		t.Fatalf("bad data metadata: %+v", im.Data)
	}
	if err := VerifyModule(im); err != nil {
		t.Fatal(err)
	}
}

func TestBuildRejectsBadFunctionIndexesAndShapes(t *testing.T) {
	m := decodeValidate(t, module([]wasm.FuncType{{}}, []uint32{0}, nil, nil, nil, [][]byte{wasmtest.Code(bytes(0x0b))}))
	if _, err := BuildFunc(m, -1); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("BuildFunc(-1) error = %v, want out of range", err)
	}
	if _, err := BuildFunc(m, 1); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("BuildFunc(1) error = %v, want out of range", err)
	}
	badType := &wasm.Module{Types: []wasm.FuncType{{}}, Functions: []uint32{3}, Code: []wasm.Code{{Body: bytes(0x0b)}}}
	if _, err := BuildFunc(badType, 0); err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("BuildFunc bad type error = %v, want unknown type", err)
	}
	unsupported := &wasm.Module{Types: []wasm.FuncType{{}}, Functions: []uint32{0}, Code: []wasm.Code{{Body: bytes(0xd0, 0x70, 0x0b)}}}
	if _, err := BuildFunc(unsupported, 0); err == nil || !strings.Contains(err.Error(), "unsupported opcode") {
		t.Fatalf("BuildFunc unsupported error = %v, want unsupported opcode", err)
	}
}

func TestBuildConstKindsAndPrinter(t *testing.T) {
	types := []wasm.FuncType{
		{Results: []wasm.ValType{wasm.I32}},
		{Results: []wasm.ValType{wasm.I64}},
		{Results: []wasm.ValType{wasm.F32}},
		{Results: []wasm.ValType{wasm.F64}},
	}
	bodies := [][]byte{
		wasmtest.Code(bytes(0x41, 0x7f, 0x0b)),                                           // -1
		wasmtest.Code(bytes(0x42, 0x7e, 0x0b)),                                           // -2
		wasmtest.Code(bytes(0x43, 0x00, 0x00, 0x80, 0x3f, 0x0b)),                         // 1.0
		wasmtest.Code(bytes(0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf0, 0x3f, 0x0b)), // 1.0
	}
	m := decodeValidate(t, module(types, []uint32{0, 1, 2, 3}, nil, nil, nil, bodies))
	assertBuilds(t, m, "const i32 -1", "const i64 -2", "const f32 0x3f800000", "const f64 0x3ff0000000000000")
}

func TestBuildIntegerNumericOpsAndEffects(t *testing.T) {
	tests := []struct {
		name   string
		params []wasm.ValType
		result wasm.ValType
		body   []byte
		want   string
		traps  bool
	}{
		{"i32.eqz", []wasm.ValType{wasm.I32}, wasm.I32, bytes(0x20, 0x00, 0x45, 0x0b), "itest.eqz", false},
		{"i64.eqz", []wasm.ValType{wasm.I64}, wasm.I32, bytes(0x20, 0x00, 0x50, 0x0b), "itest.eqz", false},
		{"i32.lt_s", []wasm.ValType{wasm.I32, wasm.I32}, wasm.I32, bytes(0x20, 0x00, 0x20, 0x01, 0x48, 0x0b), "icmp.lt_s", false},
		{"i64.ge_u", []wasm.ValType{wasm.I64, wasm.I64}, wasm.I32, bytes(0x20, 0x00, 0x20, 0x01, 0x5a, 0x0b), "icmp.ge_u", false},
		{"i32.clz", []wasm.ValType{wasm.I32}, wasm.I32, bytes(0x20, 0x00, 0x67, 0x0b), "iunary.clz", false},
		{"i64.popcnt", []wasm.ValType{wasm.I64}, wasm.I64, bytes(0x20, 0x00, 0x7b, 0x0b), "iunary.popcnt", false},
		{"i32.add", []wasm.ValType{wasm.I32, wasm.I32}, wasm.I32, bytes(0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b), "ibinary.add", false},
		{"i32.div_s", []wasm.ValType{wasm.I32, wasm.I32}, wasm.I32, bytes(0x20, 0x00, 0x20, 0x01, 0x6d, 0x0b), "ibinary.div_s", true},
		{"i64.rem_u", []wasm.ValType{wasm.I64, wasm.I64}, wasm.I64, bytes(0x20, 0x00, 0x20, 0x01, 0x82, 0x0b), "ibinary.rem_u", true},
		{"i64.rotr", []wasm.ValType{wasm.I64, wasm.I64}, wasm.I64, bytes(0x20, 0x00, 0x20, 0x01, 0x8a, 0x0b), "ibinary.rotr", false},
		{"i32.extend8_s", []wasm.ValType{wasm.I32}, wasm.I32, bytes(0x20, 0x00, 0xc0, 0x0b), "iunary.extend8_s", false},
		{"i64.extend32_s", []wasm.ValType{wasm.I64}, wasm.I64, bytes(0x20, 0x00, 0xc4, 0x0b), "iunary.extend32_s", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := decodeValidate(t, module([]wasm.FuncType{{Params: tc.params, Results: []wasm.ValType{tc.result}}}, []uint32{0}, nil, nil, nil, [][]byte{wasmtest.Code(tc.body)}))
			f, dump := buildOne(t, m)
			if !strings.Contains(dump, tc.want) {
				t.Fatalf("dump missing %q:\n%s", tc.want, dump)
			}
			inst := lastValueProducingInst(f)
			if got := inst.Effects&EffectCanTrap != 0; got != tc.traps {
				t.Fatalf("EffectCanTrap = %v, want %v for %s", got, tc.traps, dump)
			}
		})
	}
}

func TestBuildFloatNumericOps(t *testing.T) {
	tests := []struct {
		name   string
		params []wasm.ValType
		result wasm.ValType
		body   []byte
		want   string
	}{
		{"f32.eq", []wasm.ValType{wasm.F32, wasm.F32}, wasm.I32, bytes(0x20, 0x00, 0x20, 0x01, 0x5b, 0x0b), "fcmp.eq"},
		{"f64.ge", []wasm.ValType{wasm.F64, wasm.F64}, wasm.I32, bytes(0x20, 0x00, 0x20, 0x01, 0x66, 0x0b), "fcmp.ge"},
		{"f32.sqrt", []wasm.ValType{wasm.F32}, wasm.F32, bytes(0x20, 0x00, 0x91, 0x0b), "funary.sqrt"},
		{"f64.neg", []wasm.ValType{wasm.F64}, wasm.F64, bytes(0x20, 0x00, 0x9a, 0x0b), "funary.neg"},
		{"f32.min", []wasm.ValType{wasm.F32, wasm.F32}, wasm.F32, bytes(0x20, 0x00, 0x20, 0x01, 0x96, 0x0b), "fbinary.min"},
		{"f64.copysign", []wasm.ValType{wasm.F64, wasm.F64}, wasm.F64, bytes(0x20, 0x00, 0x20, 0x01, 0xa6, 0x0b), "fbinary.copysign"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := decodeValidate(t, module([]wasm.FuncType{{Params: tc.params, Results: []wasm.ValType{tc.result}}}, []uint32{0}, nil, nil, nil, [][]byte{wasmtest.Code(tc.body)}))
			_, dump := buildOne(t, m)
			if !strings.Contains(dump, tc.want) {
				t.Fatalf("dump missing %q:\n%s", tc.want, dump)
			}
		})
	}
}

func TestBuildConversionsReinterpretAndSaturatingTrunc(t *testing.T) {
	tests := []struct {
		name   string
		params []wasm.ValType
		result wasm.ValType
		body   []byte
		want   string
		traps  bool
	}{
		{"i32.wrap_i64", []wasm.ValType{wasm.I64}, wasm.I32, bytes(0x20, 0x00, 0xa7, 0x0b), "convert.wrap_i64_i32", false},
		{"i32.trunc_f32_s", []wasm.ValType{wasm.F32}, wasm.I32, bytes(0x20, 0x00, 0xa8, 0x0b), "convert.trunc_f_i_s", true},
		{"i64.extend_i32_u", []wasm.ValType{wasm.I32}, wasm.I64, bytes(0x20, 0x00, 0xad, 0x0b), "convert.extend_i32_u", false},
		{"f32.convert_i64_s", []wasm.ValType{wasm.I64}, wasm.F32, bytes(0x20, 0x00, 0xb4, 0x0b), "convert.convert_i_f_s", false},
		{"f64.promote_f32", []wasm.ValType{wasm.F32}, wasm.F64, bytes(0x20, 0x00, 0xbb, 0x0b), "convert.promote_f32_f64", false},
		{"i32.reinterpret_f32", []wasm.ValType{wasm.F32}, wasm.I32, bytes(0x20, 0x00, 0xbc, 0x0b), "reinterpret.f32_to_i32", false},
		{"f64.reinterpret_i64", []wasm.ValType{wasm.I64}, wasm.F64, bytes(0x20, 0x00, 0xbf, 0x0b), "reinterpret.i64_to_f64", false},
		{"i64.trunc_sat_f64_u", []wasm.ValType{wasm.F64}, wasm.I64, bytes(0x20, 0x00, 0xfc, 0x07, 0x0b), "convert.trunc_sat_f_i_u", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := decodeValidate(t, module([]wasm.FuncType{{Params: tc.params, Results: []wasm.ValType{tc.result}}}, []uint32{0}, nil, nil, nil, [][]byte{wasmtest.Code(tc.body)}))
			f, dump := buildOne(t, m)
			if !strings.Contains(dump, tc.want) {
				t.Fatalf("dump missing %q:\n%s", tc.want, dump)
			}
			inst := lastValueProducingInst(f)
			if got := inst.Effects&EffectCanTrap != 0; got != tc.traps {
				t.Fatalf("EffectCanTrap=%v want %v", got, tc.traps)
			}
		})
	}
}

func TestBuildSelectTypedAndUntyped(t *testing.T) {
	m := decodeValidate(t, module([]wasm.FuncType{
		{Params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}},
		{Params: []wasm.ValType{wasm.F64, wasm.F64, wasm.I32}, Results: []wasm.ValType{wasm.F64}},
	}, []uint32{0, 1}, nil, nil, nil, [][]byte{
		wasmtest.Code(bytes(0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x1b, 0x0b)),
		wasmtest.Code(bytes(0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0x1c, 0x01, byte(wasm.F64), 0x0b)),
	}))
	assertBuilds(t, m, "select i32", "select f64")
}

func TestBuildIfWithoutElseUsesBlockParamsOnFalsePath(t *testing.T) {
	types := []wasm.FuncType{{Params: []wasm.ValType{wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}, {Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}
	body := wasmtest.Code(bytes(0x20, 0x00, 0x20, 0x01, 0x04, 0x01, 0x0b, 0x0b))
	m := decodeValidate(t, module(types, []uint32{0}, nil, nil, nil, [][]byte{body}))
	_, dump := buildOne(t, m)
	if !strings.Contains(dump, "condbr") || !strings.Contains(dump, "else b2") || !strings.Contains(dump, "b3(%") {
		t.Fatalf("unexpected dump:\n%s", dump)
	}
}

func TestBuildLoopWithBlockParamsAndBackedge(t *testing.T) {
	types := []wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}, {Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}
	body := wasmtest.Code(bytes(0x20, 0x00, 0x03, 0x01, 0x20, 0x00, 0x0d, 0x00, 0x0b, 0x0b))
	m := decodeValidate(t, module(types, []uint32{0}, nil, nil, nil, [][]byte{body}))
	_, dump := buildOne(t, m)
	if !strings.Contains(dump, "condbr") || !strings.Contains(dump, "b1(%") || !strings.Contains(dump, "else b3") {
		t.Fatalf("unexpected dump:\n%s", dump)
	}
}

func TestBuildBrIfToOuterKeepsValueOnFalsePath(t *testing.T) {
	body := wasmtest.Code(bytes(0x02, byte(wasm.I32), 0x41, 0x07, 0x20, 0x00, 0x0d, 0x00, 0x0b, 0x0b))
	m := decodeValidate(t, module([]wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0}, nil, nil, nil, [][]byte{body}))
	_, dump := buildOne(t, m)
	if !strings.Contains(dump, "condbr") || !strings.Contains(dump, "else b3") || !strings.Contains(dump, "return %") {
		t.Fatalf("unexpected dump:\n%s", dump)
	}
}

func TestBuildMemorySizeGrowAndEffects(t *testing.T) {
	m := decodeValidate(t, module([]wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}, {Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}}, []uint32{0, 1}, nil, []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}, nil, [][]byte{
		wasmtest.Code(bytes(0x3f, 0x00, 0x0b)),
		wasmtest.Code(bytes(0x20, 0x00, 0x40, 0x00, 0x0b)),
	}))
	im, err := BuildModule(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyModule(im); err != nil {
		t.Fatal(err)
	}
	if im.Funcs[0].Insts[0].Effects&EffectReadMem == 0 {
		t.Fatalf("memory.size effects=%v, want EffectReadMem", im.Funcs[0].Insts[0].Effects)
	}
	if eff := im.Funcs[1].Insts[1].Effects; eff&(EffectCanTrap|EffectReadMem|EffectWriteMem) != (EffectCanTrap | EffectReadMem | EffectWriteMem) {
		t.Fatalf("memory.grow effects=%v", eff)
	}
}

func TestBuildAllLoadStoreWidths(t *testing.T) {
	tests := []struct {
		name   string
		op     byte
		val    wasm.ValType
		result wasm.ValType
		want   string
		store  bool
	}{
		{"i32.load", 0x28, 0, wasm.I32, "load.i32", false}, {"i64.load", 0x29, 0, wasm.I64, "load.i64", false}, {"f32.load", 0x2a, 0, wasm.F32, "load.f32", false}, {"f64.load", 0x2b, 0, wasm.F64, "load.f64", false},
		{"i32.load8_s", 0x2c, 0, wasm.I32, "load.i32.load8_s", false}, {"i32.load16_u", 0x2f, 0, wasm.I32, "load.i32.load16_u", false}, {"i64.load8_u", 0x31, 0, wasm.I64, "load.i64.load8_u", false}, {"i64.load32_s", 0x34, 0, wasm.I64, "load.i64.load32_s", false},
		{"i32.store", 0x36, wasm.I32, 0, "store.i32", true}, {"i64.store", 0x37, wasm.I64, 0, "store.i64", true}, {"f32.store", 0x38, wasm.F32, 0, "store.f32", true}, {"f64.store", 0x39, wasm.F64, 0, "store.f64", true}, {"i32.store8", 0x3a, wasm.I32, 0, "store.i32.store8", true}, {"i64.store32", 0x3e, wasm.I64, 0, "store.i64.store32", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var ft wasm.FuncType
			var body []byte
			if tc.store {
				ft = wasm.FuncType{Params: []wasm.ValType{wasm.I32, tc.val}}
				body = bytes(0x20, 0x00, 0x20, 0x01, tc.op, 0x00, 0x03, 0x0b)
			} else {
				ft = wasm.FuncType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{tc.result}}
				body = bytes(0x20, 0x00, tc.op, 0x00, 0x03, 0x0b)
			}
			m := decodeValidate(t, module([]wasm.FuncType{ft}, []uint32{0}, nil, []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}, nil, [][]byte{wasmtest.Code(body)}))
			f, dump := buildOne(t, m)
			if !strings.Contains(dump, tc.want) || !strings.Contains(dump, "offset=3 align=0 mem=0") {
				t.Fatalf("dump missing %q:\n%s", tc.want, dump)
			}
			inst := &f.Insts[len(f.Insts)-1]
			if tc.store && inst.Op != OpStore {
				t.Fatalf("op=%s want store", opName(inst.Op))
			}
			if !tc.store && inst.Op != OpLoad {
				t.Fatalf("op=%s want load", opName(inst.Op))
			}
		})
	}
}

func TestBuildImportedCallAndGlobal(t *testing.T) {
	m := &wasm.Module{Types: []wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}}, Imports: []wasm.Import{{Kind: wasm.ExternFunc, TypeIndex: 0}, {Kind: wasm.ExternGlobal, Global: wasm.GlobalType{Val: wasm.I32}}}, Functions: []uint32{0}, Code: []wasm.Code{{Body: bytes(0x10, 0x00, 0x23, 0x00, 0x6a, 0x0b)}}}
	f, err := BuildFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyFunc(f); err != nil {
		t.Fatal(err)
	}
	dump := FormatFunc(f)
	if !strings.Contains(dump, "call_import $0") || !strings.Contains(dump, "global.get 0") {
		t.Fatalf("unexpected dump:\n%s", dump)
	}
	if f.Insts[0].Op != OpCallImport || f.Insts[0].Effects&EffectHost == 0 {
		t.Fatalf("import call effects/op = %s %v", opName(f.Insts[0].Op), f.Insts[0].Effects)
	}
}

func buildOne(t *testing.T, m *wasm.Module) (*Func, string) {
	t.Helper()
	f, err := BuildFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyFunc(f); err != nil {
		t.Fatal(err)
	}
	return f, FormatFunc(f)
}
func lastValueProducingInst(f *Func) *Inst {
	for i := len(f.Insts) - 1; i >= 0; i-- {
		if f.Insts[i].Results.Len > 0 {
			return &f.Insts[i]
		}
	}
	return nil
}
