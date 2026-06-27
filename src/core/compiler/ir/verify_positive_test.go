package ir

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestVerifyAcceptsValidHandBuiltControlFlow(t *testing.T) {
	f := &Func{Sig: wasm.FuncType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}, Locals: []wasm.ValType{wasm.I32}, Entry: 0}
	f.Values = []Value{
		{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 0},
		{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 1},
		{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 2},
		{Type: wasm.I32, DefKind: ValueDefInst, Def: 0},
		{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 3},
	}
	f.ValueIDs = []ValueID{0, 1, 2, 0, 3, 3, 0, 1, 2, 4, 4}
	f.Insts = []Inst{{Op: OpConst, Results: Range{Start: 4, Len: 1}}}
	f.Edges = []Edge{
		{To: 1, Args: Range{Start: 5, Len: 1}},
		{To: 2, Args: Range{Start: 6, Len: 1}},
		{To: 3, Args: Range{Start: 7, Len: 1}},
		{To: 3, Args: Range{Start: 8, Len: 1}},
	}
	f.Blocks = []Block{
		{Params: Range{Start: 0, Len: 1}, Insts: Range{Start: 0, Len: 1}, Term: Term{Kind: TermCondBr, Cond: 0, Edges: Range{Start: 0, Len: 2}}},
		{Params: Range{Start: 1, Len: 1}, Term: Term{Kind: TermBr, Edges: Range{Start: 2, Len: 1}}},
		{Params: Range{Start: 2, Len: 1}, Term: Term{Kind: TermBr, Edges: Range{Start: 3, Len: 1}}},
		{Params: Range{Start: 9, Len: 1}, Term: Term{Kind: TermReturn, Args: Range{Start: 10, Len: 1}}},
	}
	if err := VerifyFunc(f); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyAcceptsValidInstructionShapes(t *testing.T) {
	tests := []struct {
		name    string
		op      Op
		args    []wasm.ValType
		results []wasm.ValType
		effects EffectFlags
	}{
		{"const", OpConst, nil, []wasm.ValType{wasm.I32}, EffectNone},
		{"local_get", OpLocalGet, nil, []wasm.ValType{wasm.I64}, EffectReadLocal},
		{"local_set", OpLocalSet, []wasm.ValType{wasm.I64}, nil, EffectWriteLocal},
		{"local_tee", OpLocalTee, []wasm.ValType{wasm.F32}, []wasm.ValType{wasm.F32}, EffectReadLocal | EffectWriteLocal},
		{"global_get", OpGlobalGet, nil, []wasm.ValType{wasm.I32}, EffectReadGlobal},
		{"global_set", OpGlobalSet, []wasm.ValType{wasm.I32}, nil, EffectWriteGlobal},
		{"iunary", OpIUnary, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, EffectNone},
		{"funary", OpFUnary, []wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}, EffectNone},
		{"convert", OpConvert, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.F64}, EffectNone},
		{"reinterpret", OpReinterpret, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.F32}, EffectNone},
		{"icmp", OpICmp, []wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I32}, EffectNone},
		{"fcmp", OpFCmp, []wasm.ValType{wasm.F32, wasm.F32}, []wasm.ValType{wasm.I32}, EffectNone},
		{"select", OpSelect, []wasm.ValType{wasm.F64, wasm.F64, wasm.I32}, []wasm.ValType{wasm.F64}, EffectNone},
		{"load", OpLoad, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, EffectCanTrap | EffectReadMem},
		{"store", OpStore, []wasm.ValType{wasm.I32, wasm.I64}, nil, EffectCanTrap | EffectWriteMem},
		{"memory_size", OpMemorySize, nil, []wasm.ValType{wasm.I32}, EffectReadMem},
		{"memory_grow", OpMemoryGrow, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, EffectCanTrap | EffectReadMem | EffectWriteMem},
		{"memory_copy", OpMemoryCopy, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil, EffectCanTrap | EffectReadMem | EffectWriteMem},
		{"memory_fill", OpMemoryFill, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil, EffectCanTrap | EffectWriteMem},
		{"call", OpCall, nil, nil, EffectCanTrap | EffectCall},
		{"call_import", OpCallImport, nil, nil, EffectCanTrap | EffectCall | EffectHost},
		{"call_indirect", OpCallIndirect, []wasm.ValType{wasm.I32}, nil, EffectCanTrap | EffectCall | EffectReadTable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := VerifyFunc(instFunc(tc.op, tc.args, tc.results, tc.effects)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
