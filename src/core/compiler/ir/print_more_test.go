package ir

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestFormatModuleSeparatesFunctionsDeterministically(t *testing.T) {
	m := decodeValidate(t, module([]wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}, {Results: []wasm.ValType{wasm.I32}}}, []uint32{0, 1}, nil, nil, nil, [][]byte{
		wasmtestCode(bytes(0x41, 0x01, 0x0b)),
		wasmtestCode(bytes(0x41, 0x02, 0x0b)),
	}))
	im, err := BuildModule(m)
	if err != nil {
		t.Fatal(err)
	}
	got1 := FormatModule(im)
	got2 := FormatModule(im)
	if got1 != got2 {
		t.Fatalf("FormatModule is nondeterministic:\n1:\n%s\n2:\n%s", got1, got2)
	}
	if !strings.Contains(got1, "}\n\nfunc $1") {
		t.Fatalf("FormatModule missing function separator:\n%s", got1)
	}
}

func TestFormatTerminators(t *testing.T) {
	f := &Func{Sig: wasm.FuncType{Params: []wasm.ValType{wasm.I32}}, Entry: 0}
	f.Values = []Value{
		{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 0},
		{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 1},
		{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 2},
	}
	f.ValueIDs = []ValueID{0, 1, 2, 1, 2, 0, 1, 2, 0}
	f.Edges = []Edge{
		{To: 1, Args: Range{Start: 3, Len: 1}},
		{To: 2, Args: Range{Start: 4, Len: 1}},
		{To: 1, Args: Range{Start: 5, Len: 1}},
		{To: 2, Args: Range{Start: 6, Len: 1}},
		{To: 1, Args: Range{Start: 7, Len: 1}},
	}
	f.Blocks = []Block{
		{Params: Range{Start: 0, Len: 1}, Term: Term{Kind: TermCondBr, Cond: 0, Edges: Range{Start: 0, Len: 2}}},
		{Params: Range{Start: 1, Len: 1}, Term: Term{Kind: TermSwitch, Index: 1, Edges: Range{Start: 2, Len: 3}}},
		{Params: Range{Start: 2, Len: 1}, Term: Term{Kind: TermTrap}},
		{Term: Term{Kind: TermInvalid}},
	}
	got := FormatFunc(f)
	for _, want := range []string{
		"condbr %0 b1 %1 else b2 %2",
		"switch %1 0:b1 %0 1:b2 %1 default:b1 %2",
		"trap",
		"<invalid>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dump missing %q:\n%s", want, got)
		}
	}
}

func TestFormatAllInstructionNames(t *testing.T) {
	ops := []Op{OpConst, OpIUnary, OpIBinary, OpICmp, OpITest, OpFUnary, OpFBinary, OpFCmp, OpConvert, OpReinterpret, OpSelect, OpLoad, OpStore, OpMemorySize, OpMemoryGrow, OpMemoryCopy, OpMemoryFill, OpGlobalGet, OpGlobalSet, OpCall, OpCallImport, OpCallIndirect, OpLocalGet, OpLocalSet, OpLocalTee}
	for _, op := range ops {
		if opName(op) == "" || opName(op) == "invalid" {
			t.Fatalf("missing opName for %d", op)
		}
	}
	if opName(OpInvalid) != "invalid" || opName(Op(999)) != "invalid" {
		t.Fatalf("invalid opcode names = %q/%q", opName(OpInvalid), opName(Op(999)))
	}
}

func TestAuxNameTablesCoverKnownKinds(t *testing.T) {
	tests := []struct {
		op   Op
		kind uint8
		want string
	}{
		{OpIBinary, uint8(IBinRotr), "rotr"},
		{OpIUnary, uint8(IUnExtend32S), "extend32_s"},
		{OpICmp, uint8(ICmpGeU), "ge_u"},
		{OpITest, uint8(ITestEqz), "eqz"},
		{OpFUnary, uint8(FUnSqrt), "sqrt"},
		{OpFBinary, uint8(FBinCopySign), "copysign"},
		{OpFCmp, uint8(FCmpGe), "ge"},
		{OpConvert, uint8(ConvTruncSatFToIU), "trunc_sat_f_i_u"},
		{OpReinterpret, uint8(ReinterpI64ToF64), "i64_to_f64"},
	}
	for _, tc := range tests {
		if got := auxName(tc.op, tc.kind); got != tc.want {
			t.Fatalf("auxName(%s, %d) = %q, want %q", opName(tc.op), tc.kind, got, tc.want)
		}
	}
}

func TestFormatMalformedAuxDoesNotPanic(t *testing.T) {
	f := instFunc(OpIBinary, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, EffectNone)
	f.Insts[0].Aux = packKindType(99, wasm.I32)
	got := FormatFunc(f)
	if !strings.Contains(got, "ibinary.kind99") {
		t.Fatalf("formatted malformed aux as:\n%s", got)
	}
}

func TestAuxPackUnpackRoundTrips(t *testing.T) {
	kt := packKindType(17, wasm.F64)
	if auxKind(kt) != 17 || auxType(kt) != wasm.F64 {
		t.Fatalf("packKindType round trip failed: kind=%d type=%s", auxKind(kt), auxType(kt))
	}
	mem := packMem(MemI64Load32U, 3, 2, 0xabcdef)
	if memKind(mem) != MemI64Load32U || memAlign(mem) != 3 || memIndex(mem) != 2 || memOffset(mem) != 0xabcdef {
		t.Fatalf("packMem round trip failed: kind=%d align=%d mem=%d off=%x", memKind(mem), memAlign(mem), memIndex(mem), memOffset(mem))
	}
	ci := packCallIndirect(123, 456)
	if callIndirectType(ci) != 123 || callIndirectTable(ci) != 456 {
		t.Fatalf("packCallIndirect round trip failed: type=%d table=%d", callIndirectType(ci), callIndirectTable(ci))
	}
}

// wasmtestCode avoids importing wasmtest under the same identifier as build_test helpers.
func wasmtestCode(body []byte) []byte { return append([]byte{byte(len(body) + 1), 0x00}, body...) }
