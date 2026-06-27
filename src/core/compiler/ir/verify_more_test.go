package ir

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestVerifyModuleRejectsBadFuncType(t *testing.T) {
	err := VerifyModule(&Module{FuncTypes: []uint32{1}})
	wantErr(t, err, "unknown type")
}

func TestVerifyRejectsNilAndBadEntry(t *testing.T) {
	wantErr(t, VerifyModule(nil), "nil module")
	wantErr(t, VerifyFunc(nil), "nil func")
	wantErr(t, VerifyFunc(&Func{Entry: 1, Blocks: []Block{{Term: Term{Kind: TermReturn}}}}), "entry block")
}

func TestVerifyRejectsValueDefProblems(t *testing.T) {
	f := validReturnI32Func()
	f.Values[0].Type = wasm.ValType(0xff)
	wantErr(t, VerifyFunc(f), "invalid type")

	f = validReturnI32Func()
	f.Values[0].DefKind = ValueDefKind(99)
	wantErr(t, VerifyFunc(f), "invalid def kind")

	f = validReturnI32Func()
	f.Values[0].Def = 9
	wantErr(t, VerifyFunc(f), "invalid block def")

	f = validConstReturnI32Func()
	f.Values[0].Def = 9
	wantErr(t, VerifyFunc(f), "invalid inst def")
}

func TestVerifyRejectsBadRangesAndInstructionPlacement(t *testing.T) {
	f := validReturnI32Func()
	f.Blocks[0].Params = Range{Start: 99, Len: 1}
	wantErr(t, VerifyFunc(f), "range out of bounds")

	f = validReturnI32Func()
	f.Blocks[0].Params = Range{Start: ^uint32(0), Len: 2}
	wantErr(t, VerifyFunc(f), "range out of bounds")

	f = validReturnI32Func()
	f.Values[0].Def = 0
	f.Blocks = append(f.Blocks, Block{Params: Range{Start: 0, Len: 1}, Term: Term{Kind: TermTrap}})
	wantErr(t, VerifyFunc(f), "param value")

	f = validConstReturnI32Func()
	f.Blocks[0].Insts = Range{Start: 0, Len: 2}
	wantErr(t, VerifyFunc(f), "instruction range out of bounds")

	f = validConstReturnI32Func()
	f.Blocks = append(f.Blocks, Block{Insts: Range{Start: 0, Len: 1}, Term: Term{Kind: TermTrap}})
	wantErr(t, VerifyFunc(f), "multiple blocks")

	f = validConstReturnI32Func()
	f.Blocks[0].Insts = Range{}
	wantErr(t, VerifyFunc(f), "not in any block")
}

func TestVerifyRejectsInstructionArityAndTypeProblems(t *testing.T) {
	f := instFunc(OpIBinary, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, EffectNone)
	wantErr(t, VerifyFunc(f), "args/results 1/1")

	f = instFunc(OpIBinary, []wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32}, EffectNone)
	wantErr(t, VerifyFunc(f), "type mismatch")

	f = instFunc(OpICmp, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, EffectNone)
	wantErr(t, VerifyFunc(f), "compare type mismatch")

	f = instFunc(OpITest, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, EffectNone)
	wantErr(t, VerifyFunc(f), "test result")

	f = instFunc(OpSelect, []wasm.ValType{wasm.I32, wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32}, EffectNone)
	wantErr(t, VerifyFunc(f), "select type mismatch")

	f = instFunc(OpLoad, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}, EffectCanTrap|EffectReadMem)
	wantErr(t, VerifyFunc(f), "address is not i32")

	f = instFunc(OpStore, []wasm.ValType{wasm.I64, wasm.I32}, nil, EffectCanTrap|EffectWriteMem)
	wantErr(t, VerifyFunc(f), "address is not i32")

	f = instFunc(OpMemoryCopy, []wasm.ValType{wasm.I32, wasm.I64, wasm.I32}, nil, EffectCanTrap|EffectReadMem|EffectWriteMem)
	wantErr(t, VerifyFunc(f), "bulk arg")
}

func TestVerifyRejectsResultAndPoisonMisuse(t *testing.T) {
	f := validConstReturnI32Func()
	f.Values[0].DefKind = ValueDefBlockParam
	wantErr(t, VerifyFunc(f), "wrong def")

	f = validConstReturnI32Func()
	f.Values = append(f.Values, Value{Type: wasm.I32, DefKind: ValueDefPoison})
	f.Insts[0].Args = Range{Start: uint32(len(f.ValueIDs)), Len: 1}
	f.ValueIDs = append(f.ValueIDs, ValueID(len(f.Values)-1))
	wantErr(t, VerifyFunc(f), "uses poison")

	f = &Func{Sig: wasm.FuncType{Results: []wasm.ValType{wasm.I32}}, Entry: 0, Values: []Value{{Type: wasm.I32, DefKind: ValueDefPoison}}, ValueIDs: []ValueID{0}, Blocks: []Block{{Term: Term{Kind: TermReturn, Args: Range{Len: 1}}}}}
	wantErr(t, VerifyFunc(f), "returns poison")
}

func TestVerifyRejectsTerminatorProblems(t *testing.T) {
	f := validReturnI32Func()
	f.Blocks[0].Term = Term{Kind: TermCondBr, Cond: 0, Edges: Range{Len: 1}}
	wantErr(t, VerifyFunc(f), "condbr has 1 edges")

	f = validReturnI32Func()
	f.Blocks[0].Term = Term{Kind: TermCondBr, Cond: InvalidValue, Edges: Range{Len: 2}}
	f.Edges = []Edge{{To: 0}, {To: 0}}
	wantErr(t, VerifyFunc(f), "invalid value")

	f = validReturnI32Func()
	f.Values[0].Type = wasm.I64
	f.Blocks[0].Term = Term{Kind: TermCondBr, Cond: 0, Edges: Range{Len: 2}}
	f.Edges = []Edge{{To: 0, Args: Range{Start: 0, Len: 1}}, {To: 0, Args: Range{Start: 0, Len: 1}}}
	wantErr(t, VerifyFunc(f), "condition is not i32")

	f = validReturnI32Func()
	f.Blocks[0].Term = Term{Kind: TermSwitch, Index: 0}
	wantErr(t, VerifyFunc(f), "switch has no edges")

	f = validReturnI32Func()
	f.Values[0].Type = wasm.I64
	f.Blocks[0].Term = Term{Kind: TermSwitch, Index: 0, Edges: Range{Len: 1}}
	f.Edges = []Edge{{To: 0, Args: Range{Start: 0, Len: 1}}}
	wantErr(t, VerifyFunc(f), "switch index is not i32")

	f = validReturnI32Func()
	f.Blocks[0].Term = Term{Kind: TermReturn}
	wantErr(t, VerifyFunc(f), "return arity")

	f = validReturnI32Func()
	f.Blocks[0].Term = Term{Kind: TermKind(99)}
	wantErr(t, VerifyFunc(f), "unknown terminator")
}

func TestVerifyRejectsEdgeProblems(t *testing.T) {
	f := validReturnI32Func()
	f.Blocks[0].Term = Term{Kind: TermBr, Edges: Range{Len: 2}}
	f.Edges = []Edge{{To: 0}, {To: 0}}
	wantErr(t, VerifyFunc(f), "br has 2 edges")

	f = validReturnI32Func()
	f.Blocks[0].Term = Term{Kind: TermBr, Edges: Range{Start: 99, Len: 1}}
	wantErr(t, VerifyFunc(f), "edge range")

	f = validReturnI32Func()
	f.Blocks[0].Term = Term{Kind: TermCondBr, Cond: 0, Edges: Range{Start: ^uint32(0), Len: 2}}
	wantErr(t, VerifyFunc(f), "edge range")

	f = validReturnI32Func()
	f.Blocks[0].Term = Term{Kind: TermBr, Edges: Range{Len: 1}}
	f.Edges = []Edge{{To: 99}}
	wantErr(t, VerifyFunc(f), "target 99")

	f = validReturnI32Func()
	f.Blocks[0].Term = Term{Kind: TermBr, Edges: Range{Len: 1}}
	f.Edges = []Edge{{To: 0, Args: Range{Start: 99, Len: 1}}}
	wantErr(t, VerifyFunc(f), "range out of bounds")

	f = &Func{Entry: 0, Values: []Value{{Type: wasm.I64, DefKind: ValueDefBlockParam, Def: 0}, {Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 1}}, ValueIDs: []ValueID{0, 1}, Edges: []Edge{{To: 1, Args: Range{Start: 0, Len: 2}}}, Blocks: []Block{{Term: Term{Kind: TermBr, Edges: Range{Len: 1}}}, {Params: Range{Start: ^uint32(0), Len: 2}, Term: Term{Kind: TermTrap}}}}
	wantErr(t, VerifyFunc(f), "target b1 params")

	f = &Func{Entry: 0, Values: []Value{{Type: wasm.I64, DefKind: ValueDefBlockParam, Def: 0}, {Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 1}}, ValueIDs: []ValueID{0, 1, 0}, Edges: []Edge{{To: 1, Args: Range{Start: 2, Len: 1}}}, Blocks: []Block{{Params: Range{Start: 0, Len: 1}, Term: Term{Kind: TermBr, Edges: Range{Len: 1}}}, {Params: Range{Start: 1, Len: 1}, Term: Term{Kind: TermTrap}}}}
	wantErr(t, VerifyFunc(f), "arg 0 type")

	f = &Func{Sig: wasm.FuncType{Results: []wasm.ValType{wasm.I32}}, Entry: 0, Values: []Value{{Type: wasm.I64, DefKind: ValueDefBlockParam, Def: 0}, {Type: wasm.I64, DefKind: ValueDefBlockParam, Def: 1}}, ValueIDs: []ValueID{0, 1, 0, 1}, Edges: []Edge{{To: 1, Args: Range{Start: 2, Len: 1}}}, Blocks: []Block{{Params: Range{Start: 0, Len: 1}, Term: Term{Kind: TermBr, Edges: Range{Len: 1}}}, {Params: Range{Start: 1, Len: 1}, Term: Term{Kind: TermReturn, Args: Range{Start: 3, Len: 1}}}}}
	wantErr(t, VerifyFunc(f), "return arg")
}

func TestVerifyRejectsCallEffects(t *testing.T) {
	for _, op := range []Op{OpCall, OpCallImport, OpCallIndirect} {
		f := instFunc(op, nil, nil, EffectCall)
		wantErr(t, VerifyFunc(f), "call missing effects")
	}
}

func validReturnI32Func() *Func {
	return &Func{
		Sig:      wasm.FuncType{Results: []wasm.ValType{wasm.I32}},
		Entry:    0,
		Values:   []Value{{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 0}},
		ValueIDs: []ValueID{0, 0},
		Blocks:   []Block{{Params: Range{Start: 0, Len: 1}, Term: Term{Kind: TermReturn, Args: Range{Start: 1, Len: 1}}}},
	}
}

func validConstReturnI32Func() *Func {
	return &Func{
		Sig:      wasm.FuncType{Results: []wasm.ValType{wasm.I32}},
		Entry:    0,
		Values:   []Value{{Type: wasm.I32, DefKind: ValueDefInst, Def: 0}},
		ValueIDs: []ValueID{0, 0},
		Insts:    []Inst{{Op: OpConst, Results: Range{Start: 0, Len: 1}}},
		Blocks:   []Block{{Insts: Range{Start: 0, Len: 1}, Term: Term{Kind: TermReturn, Args: Range{Start: 1, Len: 1}}}},
	}
}

func instFunc(op Op, args []wasm.ValType, results []wasm.ValType, effects EffectFlags) *Func {
	f := &Func{Sig: wasm.FuncType{Results: results}, Entry: 0}
	for _, t := range args {
		f.Values = append(f.Values, Value{Type: t, DefKind: ValueDefBlockParam, Def: 0})
		f.ValueIDs = append(f.ValueIDs, ValueID(len(f.Values)-1))
	}
	argRange := Range{Start: 0, Len: uint32(len(args))}
	resStart := uint32(len(f.ValueIDs))
	for _, t := range results {
		f.Values = append(f.Values, Value{Type: t, DefKind: ValueDefInst, Def: 0})
		f.ValueIDs = append(f.ValueIDs, ValueID(len(f.Values)-1))
	}
	resRange := Range{Start: resStart, Len: uint32(len(results))}
	f.Insts = []Inst{{Op: op, Args: argRange, Results: resRange, Effects: effects}}
	retStart := uint32(len(f.ValueIDs))
	for i := range results {
		f.ValueIDs = append(f.ValueIDs, ValueID(len(args)+i))
	}
	f.Blocks = []Block{{Params: argRange, Insts: Range{Len: 1}, Term: Term{Kind: TermReturn, Args: Range{Start: retStart, Len: uint32(len(results))}}}}
	return f
}

func wantErr(t *testing.T, err error, contains string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatalf("error = %v, want containing %q", err, contains)
	}
}
