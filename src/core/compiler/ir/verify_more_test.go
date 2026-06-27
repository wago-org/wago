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

func TestVerifyRejectsBadLocalLayout(t *testing.T) {
	f := validReturnI32Func()
	f.Sig.Params = []wasm.ValType{wasm.I32}
	wantErr(t, VerifyFunc(f), "locals prefix")

	f = validReturnI32Func()
	f.Sig.Params = []wasm.ValType{wasm.I64}
	f.Locals = []wasm.ValType{wasm.I32}
	wantErr(t, VerifyFunc(f), "param type")
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

	f = instFunc(OpIBinary, []wasm.ValType{wasm.F32, wasm.F32}, []wasm.ValType{wasm.F32}, EffectNone)
	wantErr(t, VerifyFunc(f), "integer binary")

	f = instFunc(OpFUnary, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, EffectNone)
	wantErr(t, VerifyFunc(f), "float unary")

	f = instFunc(OpICmp, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, EffectNone)
	wantErr(t, VerifyFunc(f), "compare type mismatch")

	f = instFunc(OpITest, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, EffectNone)
	wantErr(t, VerifyFunc(f), "test result")

	f = instFunc(OpSelect, []wasm.ValType{wasm.I32, wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32}, EffectNone)
	wantErr(t, VerifyFunc(f), "select type mismatch")

	f = instFunc(OpLoad, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}, EffectCanTrap|EffectReadMem)
	wantErr(t, VerifyFunc(f), "address is not i32")

	f = instFunc(OpLoad, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, EffectCanTrap|EffectReadMem)
	f.Insts[0].Aux = packMem(MemI32, 2, 0, 0)
	wantErr(t, VerifyFunc(f), "load type mismatch")

	f = instFunc(OpStore, []wasm.ValType{wasm.I64, wasm.I32}, nil, EffectCanTrap|EffectWriteMem)
	wantErr(t, VerifyFunc(f), "address is not i32")

	f = instFunc(OpStore, []wasm.ValType{wasm.I32, wasm.F64}, nil, EffectCanTrap|EffectWriteMem)
	f.Insts[0].Aux = packMem(MemI32, 2, 0, 0)
	wantErr(t, VerifyFunc(f), "store type mismatch")

	f = instFunc(OpLocalGet, nil, []wasm.ValType{wasm.I32}, EffectReadLocal)
	f.Locals = nil
	wantErr(t, VerifyFunc(f), "local index")

	f = instFunc(OpMemoryCopy, []wasm.ValType{wasm.I32, wasm.I64, wasm.I32}, nil, EffectCanTrap|EffectReadMem|EffectWriteMem)
	wantErr(t, VerifyFunc(f), "bulk arg")

	f = instFunc(OpMemorySize, nil, []wasm.ValType{wasm.I32}, EffectNone)
	wantErr(t, VerifyFunc(f), "memory.size missing effects")
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

	f = validReturnI32Func()
	f.Blocks[0].Term = Term{Kind: TermTrap}
	f.Blocks[0].Flags = BlockSyntheticReturn
	wantErr(t, VerifyFunc(f), "synthetic return")
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

func TestVerifyModuleRejectsInstructionMetadataMismatches(t *testing.T) {
	base := func() *Module {
		return &Module{
			Types:             []wasm.FuncType{{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I64}}},
			ImportedFuncCount: 1,
			FuncTypes:         []uint32{0, 0},
			Globals:           []wasm.GlobalType{{Val: wasm.I32}},
			Memories:          []wasm.MemType{{}},
			Tables:            []wasm.TableType{{Elem: wasm.FuncRef}},
		}
	}
	tests := []struct {
		name string
		edit func(*Module)
		want string
	}{
		{"call_signature", func(m *Module) {
			m.Funcs = []Func{*instFunc(OpCallImport, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, EffectCanTrap|EffectCall|EffectHost)}
		}, "call arg"},
		{"call_kind", func(m *Module) {
			f := instFunc(OpCall, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, EffectCanTrap|EffectCall)
			f.Insts[0].Aux = 0
			m.Funcs = []Func{*f}
		}, "is imported"},
		{"call_indirect_table", func(m *Module) {
			f := instFunc(OpCallIndirect, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, EffectCanTrap|EffectCall|EffectReadTable)
			f.Insts[0].Aux = packCallIndirect(0, 9)
			m.Funcs = []Func{*f}
		}, "table 9"},
		{"global_type", func(m *Module) {
			f := instFunc(OpGlobalGet, nil, []wasm.ValType{wasm.I64}, EffectReadGlobal)
			m.Funcs = []Func{*f}
		}, "global type"},
		{"memory_index", func(m *Module) {
			f := instFunc(OpLoad, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, EffectCanTrap|EffectReadMem)
			f.Insts[0].Aux = packMem(MemI32, 2, 7, 0)
			m.Funcs = []Func{*f}
		}, "memory index 7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := base()
			tc.edit(m)
			wantErr(t, VerifyModule(m), tc.want)
		})
	}
}

func TestVerifyRejectsCallEffects(t *testing.T) {
	for _, op := range []Op{OpCall, OpCallImport, OpCallIndirect} {
		f := instFunc(op, nil, nil, EffectCall)
		wantErr(t, VerifyFunc(f), "call missing effects")
	}

	f := instFunc(OpCallImport, nil, nil, EffectCall|EffectCanTrap)
	wantErr(t, VerifyFunc(f), "call missing effects")

	f = instFunc(OpCallIndirect, nil, nil, EffectCall|EffectCanTrap)
	wantErr(t, VerifyFunc(f), "call missing effects")

	f = instFunc(OpCall, nil, nil, EffectCall|EffectCanTrap|EffectHost)
	wantErr(t, VerifyFunc(f), "host effect")

	f = instFunc(OpCallImport, nil, nil, EffectCall|EffectCanTrap|EffectHost|EffectReadTable)
	wantErr(t, VerifyFunc(f), "table effect")
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
	if op == OpLocalGet && len(results) == 1 {
		f.Locals = []wasm.ValType{results[0]}
	} else if (op == OpLocalSet || op == OpLocalTee) && len(args) > 0 {
		f.Locals = []wasm.ValType{args[0]}
	}
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
	f.Insts = []Inst{{Op: op, Args: argRange, Results: resRange, Aux: defaultAux(op, args, results), Effects: effects}}
	retStart := uint32(len(f.ValueIDs))
	for i := range results {
		f.ValueIDs = append(f.ValueIDs, ValueID(len(args)+i))
	}
	f.Blocks = []Block{{Params: argRange, Insts: Range{Len: 1}, Term: Term{Kind: TermReturn, Args: Range{Start: retStart, Len: uint32(len(results))}}}}
	return f
}

func defaultAux(op Op, args []wasm.ValType, results []wasm.ValType) uint64 {
	singleArg := func() wasm.ValType {
		if len(args) == 0 {
			return 0
		}
		return args[0]
	}
	singleResult := func() wasm.ValType {
		if len(results) == 0 {
			return 0
		}
		return results[0]
	}
	switch op {
	case OpIUnary:
		return packKindType(uint8(IUnClz), singleArg())
	case OpIBinary:
		return packKindType(uint8(IBinAdd), singleArg())
	case OpICmp:
		return packKindType(uint8(ICmpEq), singleArg())
	case OpITest:
		return packKindType(uint8(ITestEqz), singleArg())
	case OpFUnary:
		return packKindType(uint8(FUnAbs), singleArg())
	case OpFBinary:
		return packKindType(uint8(FBinAdd), singleArg())
	case OpFCmp:
		return packKindType(uint8(FCmpEq), singleArg())
	case OpConvert:
		src, dst := singleArg(), singleResult()
		switch {
		case src == wasm.I64 && dst == wasm.I32:
			return packKindType(uint8(ConvWrapI64ToI32), dst)
		case src == wasm.I32 && dst == wasm.I64:
			return packKindType(uint8(ConvExtendI32S), dst)
		case (src == wasm.I32 || src == wasm.I64) && (dst == wasm.F32 || dst == wasm.F64):
			return packKindType(uint8(ConvConvertIToFS), dst)
		case src == wasm.F64 && dst == wasm.F32:
			return packKindType(uint8(ConvDemoteF64ToF32), dst)
		case src == wasm.F32 && dst == wasm.F64:
			return packKindType(uint8(ConvPromoteF32ToF64), dst)
		default:
			return packKindType(uint8(ConvTruncFToIS), dst)
		}
	case OpReinterpret:
		src, dst := singleArg(), singleResult()
		switch {
		case src == wasm.F32 && dst == wasm.I32:
			return packKindType(uint8(ReinterpF32ToI32), dst)
		case src == wasm.F64 && dst == wasm.I64:
			return packKindType(uint8(ReinterpF64ToI64), dst)
		case src == wasm.I64 && dst == wasm.F64:
			return packKindType(uint8(ReinterpI64ToF64), dst)
		default:
			return packKindType(uint8(ReinterpI32ToF32), dst)
		}
	case OpSelect:
		return uint64(singleResult())
	case OpLoad:
		switch singleResult() {
		case wasm.I64:
			return packMem(MemI64, 3, 0, 0)
		case wasm.F32:
			return packMem(MemF32, 2, 0, 0)
		case wasm.F64:
			return packMem(MemF64, 3, 0, 0)
		default:
			return packMem(MemI32, 2, 0, 0)
		}
	case OpStore:
		if len(args) > 1 {
			switch args[1] {
			case wasm.I64:
				return packMem(MemI64, 3, 0, 0)
			case wasm.F32:
				return packMem(MemF32, 2, 0, 0)
			case wasm.F64:
				return packMem(MemF64, 3, 0, 0)
			}
		}
		return packMem(MemI32, 2, 0, 0)
	}
	return 0
}

func wantErr(t *testing.T, err error, contains string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatalf("error = %v, want containing %q", err, contains)
	}
}
