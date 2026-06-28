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

func TestVerifyModuleRejectsFunctionMetadataMismatches(t *testing.T) {
	base := func() *Module {
		return &Module{
			Types:             []wasm.FuncType{{Results: []wasm.ValType{wasm.I32}}},
			ImportedFuncCount: 1,
			FuncTypes:         []uint32{0, 0},
			Funcs:             []Func{*validReturnI32Func()},
		}
	}
	tests := []struct {
		name string
		edit func(*Module)
		want string
	}{
		{"import_count", func(m *Module) { m.ImportedFuncCount = 3 }, "imported function count"},
		{"local_count", func(m *Module) { m.Funcs = nil }, "local function count"},
		{"index", func(m *Module) { m.Funcs[0].Index = 0 }, "has index"},
		{"local_index", func(m *Module) { m.Funcs[0].LocalIndex = 7 }, "local index"},
		{"type_index", func(m *Module) { m.Funcs[0].TypeIndex = 7 }, "type index"},
		{"signature", func(m *Module) { m.Funcs[0].Sig.Results = nil }, "signature"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := base()
			m.Funcs[0].Index = 1
			m.Funcs[0].LocalIndex = 0
			m.Funcs[0].TypeIndex = 0
			tc.edit(m)
			wantErr(t, VerifyModule(m), tc.want)
		})
	}
}

func TestVerifyFuncInModuleChecksModuleIndexes(t *testing.T) {
	f := instFunc(OpLoad, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, EffectCanTrap|EffectReadMem)
	// Standalone verification cannot see missing module metadata.
	if err := VerifyFunc(f); err != nil {
		t.Fatalf("VerifyFunc standalone = %v", err)
	}
	wantErr(t, VerifyFuncInModule(f, &Module{}), "memory index 0")
}

func TestVerifyFuncInModuleRejectsDirectCallToNonFunctionTypeIndex(t *testing.T) {
	f := instFunc(OpCall, nil, nil, EffectCanTrap|EffectCall)
	m := &Module{
		Types:      []wasm.FuncType{{}},
		TypeIsFunc: []bool{false},
		FuncTypes:  []uint32{0},
	}
	wantErr(t, VerifyFuncInModule(f, m), "non-function type")

	m.TypeIsFunc[0] = true
	if err := VerifyFuncInModule(f, m); err != nil {
		t.Fatalf("VerifyFuncInModule positive control = %v", err)
	}
}

func TestVerifyRejectsNilAndBadEntry(t *testing.T) {
	wantErr(t, VerifyModule(nil), "nil module")
	wantErr(t, VerifyFunc(nil), "nil func")
	wantErr(t, VerifyFunc(&Func{Entry: 1, Blocks: []Block{{Term: Term{Kind: TermReturn}}}}), "entry block")

	f := validReturnI32Func()
	f.Sig.Params = nil
	f.Locals = nil
	wantErr(t, VerifyFunc(f), "entry block")
}

func TestVerifyRejectsBadLocalLayout(t *testing.T) {
	f := validReturnI32Func()
	f.Locals = nil
	wantErr(t, VerifyFunc(f), "locals prefix")

	f = validReturnI32Func()
	f.Sig.Params = []wasm.ValType{wasm.I64}
	f.Locals = []wasm.ValType{wasm.I32}
	wantErr(t, VerifyFunc(f), "param type")

	f = validReturnI32Func()
	f.Sig.Params = []wasm.ValType{wasm.ValType{}}
	f.Locals = []wasm.ValType{wasm.ValType{}}
	wantErr(t, VerifyFunc(f), "signature param")

	f = validReturnI32Func()
	f.Sig.Results = []wasm.ValType{wasm.ValType{}}
	wantErr(t, VerifyFunc(f), "signature result")

	f = validReturnI32Func()
	f.Locals = []wasm.ValType{wasm.I32, wasm.ValType{}}
	wantErr(t, VerifyFunc(f), "local 1")
}

func TestVerifyRejectsMixedCompactAndExpandedLocalLayout(t *testing.T) {
	f := instFunc(OpLocalGet, nil, []wasm.ValType{wasm.I64}, EffectReadLocal)
	f.Sig.Params = []wasm.ValType{wasm.I32}
	f.Locals = []wasm.ValType{wasm.I32, wasm.I64}
	f.LocalRuns = []wasm.LocalEntry{{Count: 1, Type: wasm.I64}}
	f.Insts[0].Aux = 1
	wantErr(t, VerifyFunc(f), "mixed compact and expanded local layout")
}

func TestVerifyAcceptsCompactAndExpandedLocalLayouts(t *testing.T) {
	compact := instFunc(OpLocalGet, nil, []wasm.ValType{wasm.I64}, EffectReadLocal)
	compact.Sig.Params = []wasm.ValType{wasm.I32}
	compact.Locals = []wasm.ValType{wasm.I32}
	compact.LocalRuns = []wasm.LocalEntry{{Count: 1, Type: wasm.I64}}
	compact.Insts[0].Aux = 1
	if err := VerifyFunc(compact); err != nil {
		t.Fatalf("compact local layout: %v", err)
	}

	expanded := instFunc(OpLocalGet, nil, []wasm.ValType{wasm.I64}, EffectReadLocal)
	expanded.Sig.Params = []wasm.ValType{wasm.I32}
	expanded.Locals = []wasm.ValType{wasm.I32, wasm.I64}
	expanded.Insts[0].Aux = 1
	if err := VerifyFunc(expanded); err != nil {
		t.Fatalf("expanded local layout: %v", err)
	}
}

func TestVerifyValueTypeSupportBoundary(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64} {
		t.Run(typ.String(), func(t *testing.T) {
			f := instFunc(OpLocalGet, nil, []wasm.ValType{typ}, EffectReadLocal)
			if err := VerifyFunc(f); err != nil {
				t.Fatal(err)
			}
		})
	}

	f := validReturnI32Func()
	f.Sig.Results = []wasm.ValType{wasm.FuncRef}
	f.Values[0].Type = wasm.FuncRef
	wantErr(t, VerifyFunc(f), "invalid type funcref")
}

func TestVerifyRejectsValueDefProblems(t *testing.T) {
	f := validReturnI32Func()
	f.Values[0].Type = wasm.ValType{}
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

func TestVerifyRejectsGhostInstValue(t *testing.T) {
	f := validConstReturnI32Func()
	f.Values = append(f.Values, Value{Type: wasm.I32, DefKind: ValueDefInst, Def: 0})
	wantErr(t, VerifyFunc(f), "value 1 is missing")
}

func TestVerifyRejectsGhostBlockParam(t *testing.T) {
	f := validReturnI32Func()
	f.Values = append(f.Values, Value{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 0})
	wantErr(t, VerifyFunc(f), "value 1 is missing")
}

func TestVerifyRejectsDuplicateInstResult(t *testing.T) {
	f := instFunc(OpCall, nil, []wasm.ValType{wasm.I32, wasm.I32}, EffectCanTrap|EffectCall)
	for i, v := range f.ValueIDs {
		if v == 1 {
			f.ValueIDs[i] = 0
		}
	}
	f.Values = f.Values[:1]
	wantErr(t, VerifyFunc(f), "value 0 appears in multiple definition ranges")
}

func TestVerifyRejectsDuplicateBlockParam(t *testing.T) {
	f := validReturnI32Func()
	f.Blocks[0].Params = Range{Start: 0, Len: 2}
	wantErr(t, VerifyFunc(f), "value 0 appears in multiple definition ranges")
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

	f = instFunc(OpLocalTee, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, EffectReadLocal|EffectWriteLocal)
	wantErr(t, VerifyFunc(f), "local.tee has read effect")

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

func TestDominanceIntervalsModelIDOMTree(t *testing.T) {
	idom := []BlockID{0, 0, 1, 1, 2}
	reachable := []bool{true, true, true, true, true}
	pre, end := dominanceIntervals(0, idom, reachable)
	if !dominatesInterval(pre, end, 0, 4) || !dominatesInterval(pre, end, 1, 3) || !dominatesInterval(pre, end, 2, 4) {
		t.Fatalf("expected dominators missing: pre=%v end=%v", pre, end)
	}
	if dominatesInterval(pre, end, 2, 3) || dominatesInterval(pre, end, 3, 4) {
		t.Fatalf("unexpected sibling dominance: pre=%v end=%v", pre, end)
	}
}

func TestVerifyRejectsNonDominatingValues(t *testing.T) {
	f := &Func{
		Sig:    wasm.FuncType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}},
		Locals: []wasm.ValType{wasm.I32},
		Entry:  0,
		Values: []Value{
			{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 0},
			{Type: wasm.I32, DefKind: ValueDefInst, Def: 1},
			{Type: wasm.I32, DefKind: ValueDefInst, Def: 0},
		},
		ValueIDs: []ValueID{0, 0, 1, 2, 1, 2},
		Insts: []Inst{
			{Op: OpIBinary, Args: Range{Start: 1, Len: 2}, Results: Range{Start: 3, Len: 1}, Aux: packKindType(uint8(IBinAdd), wasm.I32)},
			{Op: OpConst, Results: Range{Start: 4, Len: 1}},
		},
		Blocks: []Block{{Params: Range{Start: 0, Len: 1}, Insts: Range{Start: 0, Len: 2}, Term: Term{Kind: TermReturn, Args: Range{Start: 5, Len: 1}}}},
	}
	wantErr(t, VerifyFunc(f), "used before its definition")

	f = &Func{
		Sig:    wasm.FuncType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}},
		Locals: []wasm.ValType{wasm.I32},
		Entry:  0,
		Values: []Value{
			{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 0},
			{Type: wasm.I32, DefKind: ValueDefInst, Def: 0},
			{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 3},
		},
		ValueIDs: []ValueID{0, 1, 1, 2, 2},
		Insts:    []Inst{{Op: OpConst, Results: Range{Start: 1, Len: 1}}},
		Edges: []Edge{
			{To: 1},
			{To: 2},
			{To: 3, Args: Range{Start: 2, Len: 1}},
		},
		Blocks: []Block{
			{Params: Range{Start: 0, Len: 1}, Term: Term{Kind: TermCondBr, Cond: 0, Edges: Range{Start: 0, Len: 2}}},
			{Insts: Range{Start: 0, Len: 1}, Term: Term{Kind: TermTrap}},
			{Term: Term{Kind: TermBr, Edges: Range{Start: 2, Len: 1}}},
			{Params: Range{Start: 3, Len: 1}, Term: Term{Kind: TermReturn, Args: Range{Start: 4, Len: 1}}},
		},
	}
	wantErr(t, VerifyFunc(f), "does not dominate")
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
			Types:             []wasm.FuncType{{Results: []wasm.ValType{wasm.I64}}, {Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I64}}},
			TypeIsFunc:        []bool{true, true},
			CanonicalTypeIDs:  []uint32{0, 1},
			ImportedFuncCount: 1,
			FuncTypes:         []uint32{1, 0},
			Globals:           []wasm.GlobalType{{Type: wasm.I32}},
			Memories:          []wasm.MemType{{}},
			Tables:            []wasm.TableType{{Ref: wasm.FuncRef.Ref}},
		}
	}
	placeFunc := func(m *Module, f *Func) {
		// Module verification now checks that the local function header agrees with
		// flattened module metadata before it validates instruction-index metadata.
		m.Types[0] = f.Sig
		f.Index = 1
		f.LocalIndex = 0
		f.TypeIndex = 0
		m.Funcs = []Func{*f}
	}
	tests := []struct {
		name string
		edit func(*Module)
		want string
	}{
		{"call_signature", func(m *Module) {
			placeFunc(m, instFunc(OpCallImport, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, EffectCanTrap|EffectCall|EffectHost))
		}, "call arg"},
		{"call_kind", func(m *Module) {
			f := instFunc(OpCall, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, EffectCanTrap|EffectCall)
			f.Insts[0].Aux = 0
			placeFunc(m, f)
			m.CanonicalTypeIDs = []uint32{0, 0}
		}, "is imported"},
		{"call_indirect_table", func(m *Module) {
			f := instFunc(OpCallIndirect, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, EffectCanTrap|EffectCall|EffectReadTable)
			f.Insts[0].Aux = packCallIndirect(1, 9)
			f.Insts[0].Aux2 = 1
			placeFunc(m, f)
		}, "table 9"},
		{"global_type", func(m *Module) {
			placeFunc(m, instFunc(OpGlobalGet, nil, []wasm.ValType{wasm.I64}, EffectReadGlobal))
		}, "global type"},
		{"memory_index", func(m *Module) {
			f := instFunc(OpLoad, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, EffectCanTrap|EffectReadMem)
			f.Insts[0].Aux = packMem(MemI32, 2, 7, 0)
			placeFunc(m, f)
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

func TestVerifyRejectsOutOfRangeCanonicalTypeID(t *testing.T) {
	m := callIndirectModuleForVerify()
	m.CanonicalTypeIDs[0] = 99
	wantErr(t, VerifyModule(m), "canonical type id for type 0 out of range")
}

func TestVerifyRejectsCanonicalTypeIDToNonFunctionType(t *testing.T) {
	m := callIndirectModuleForVerify()
	m.TypeIsFunc = []bool{true, true, false}
	m.CanonicalTypeIDs = []uint32{0, 2, 2}
	wantErr(t, VerifyModule(m), "references non-function type")
}

func TestVerifyRejectsCanonicalTypeIDWithDifferentSignature(t *testing.T) {
	m := callIndirectModuleForVerify()
	m.CanonicalTypeIDs = []uint32{0, 0, 1}
	wantErr(t, VerifyModule(m), "different signature")
}

func TestVerifyRejectsNonCanonicalEquivalentTypeID(t *testing.T) {
	m := callIndirectModuleForVerify()
	m.CanonicalTypeIDs = []uint32{0, 1, 2}
	wantErr(t, VerifyModule(m), "want 1")
}

func TestVerifyAcceptsCanonicalEquivalentTypeMetadata(t *testing.T) {
	m := callIndirectModuleForVerify()
	m.CanonicalTypeIDs = []uint32{0, 1, 1}
	if err := VerifyModule(m); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsBadCallIndirectCanonicalTypeID(t *testing.T) {
	m := callIndirectModuleForVerify()
	m.Funcs[0].Insts[0].Aux = packCallIndirect(1, 0)
	m.Funcs[0].Insts[0].Aux2 = 2
	wantErr(t, VerifyModule(m), "canonical type id")
}

func TestVerifyRejectsCallIndirectTableWithNonFunctionHeapTypeIndex(t *testing.T) {
	m := callIndirectModuleForVerify()
	m.TypeIsFunc = []bool{true, true, false}
	m.CanonicalTypeIDs = []uint32{0, 1, 2}
	m.Tables[0] = wasm.TableType{Ref: wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 2}), false)}
	wantErr(t, VerifyModule(m), "function reference table")
}

func TestVerifyAcceptsCallIndirectTableWithFunctionHeapTypeIndex(t *testing.T) {
	m := callIndirectModuleForVerify()
	m.Tables[0] = wasm.TableType{Ref: wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false)}
	if err := VerifyModule(m); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsCallIndirectNonFunctionTypeIndex(t *testing.T) {
	m := callIndirectModuleForVerify()
	m.TypeIsFunc = []bool{true, true, false}
	m.CanonicalTypeIDs = []uint32{0, 1, 2}
	m.Funcs[0].Insts[0].Aux = packCallIndirect(2, 0)
	m.Funcs[0].Insts[0].Aux2 = 2
	wantErr(t, VerifyModule(m), "not a function type")
}

func TestVerifyAcceptsCallIndirectCanonicalEquivalentType(t *testing.T) {
	m := callIndirectModuleForVerify()
	m.Funcs[0].Insts[0].Aux = packCallIndirect(2, 0)
	m.Funcs[0].Insts[0].Aux2 = 1
	if err := VerifyModule(m); err != nil {
		t.Fatal(err)
	}
}

func callIndirectModuleForVerify() *Module {
	f := instFunc(OpCallIndirect, []wasm.ValType{wasm.I32}, nil, EffectCanTrap|EffectCall|EffectReadTable)
	f.Index = 0
	f.LocalIndex = 0
	f.TypeIndex = 0
	f.Insts[0].Aux = packCallIndirect(1, 0)
	f.Insts[0].Aux2 = 1
	return &Module{
		Types:            []wasm.FuncType{{Params: []wasm.ValType{wasm.I32}}, {}, {}},
		TypeIsFunc:       []bool{true, true, true},
		CanonicalTypeIDs: []uint32{0, 1, 1},
		FuncTypes:        []uint32{0},
		Tables:           []wasm.TableType{{Ref: wasm.FuncRef.Ref}},
		Funcs:            []Func{*f},
	}
}

func TestVerifyRejectsUnexpectedEffects(t *testing.T) {
	f := instFunc(OpConst, nil, []wasm.ValType{wasm.I32}, EffectWriteMem)
	wantErr(t, VerifyFunc(f), "unexpected effects")

	f = instFunc(OpLoad, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, EffectCanTrap|EffectReadMem|EffectWriteGlobal)
	wantErr(t, VerifyFunc(f), "unexpected effects")

	f = instFunc(OpIBinary, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, EffectCanTrap)
	wantErr(t, VerifyFunc(f), "unexpected effects")

	f = instFunc(OpConvert, []wasm.ValType{wasm.F32}, []wasm.ValType{wasm.I32}, EffectNone)
	wantErr(t, VerifyFunc(f), "missing effects")
}

func TestVerifyRejectsBadMemoryAlignment(t *testing.T) {
	f := instFunc(OpLoad, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, EffectCanTrap|EffectReadMem)
	f.Insts[0].Aux = packMem(MemI32, 3, 0, 0)
	wantErr(t, VerifyFunc(f), "alignment 3 exceeds natural")
}

func TestVerifyRejectsStaleAux2OnNonCallIndirect(t *testing.T) {
	f := instFunc(OpConst, nil, []wasm.ValType{wasm.I32}, EffectNone)
	f.Insts[0].Aux2 = 1
	wantErr(t, VerifyFunc(f), "unexpected aux2")
}

func TestVerifyRejectsNonCanonicalMemoryIndexAux(t *testing.T) {
	f := instFunc(OpMemorySize, nil, []wasm.ValType{wasm.I32}, EffectReadMem)
	f.Insts[0].Aux = uint64(1) << 32
	wantErr(t, VerifyFunc(f), "non-canonical memory index")
}

func TestVerifyRejectsNonCanonicalScalarAux(t *testing.T) {
	f := instFunc(OpConst, nil, []wasm.ValType{wasm.I32}, EffectNone)
	f.Insts[0].Aux = uint64(1) << 32
	wantErr(t, VerifyFunc(f), "non-canonical aux")

	f = instFunc(OpIBinary, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, EffectNone)
	f.Insts[0].Aux = packKindType(uint8(IBinAdd), wasm.I32) | 1<<16
	wantErr(t, VerifyFunc(f), "non-canonical aux")

	f = instFunc(OpCall, nil, nil, EffectCanTrap|EffectCall)
	f.Insts[0].Aux = uint64(1) << 32
	wantErr(t, VerifyFunc(f), "non-canonical function index")
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

	f = instFunc(OpCall, nil, nil, EffectCall|EffectCanTrap|EffectReadMem)
	wantErr(t, VerifyFunc(f), "unexpected effects")
}

func validReturnI32Func() *Func {
	return &Func{
		Sig:      wasm.FuncType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}},
		Entry:    0,
		Locals:   []wasm.ValType{wasm.I32},
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
	f := &Func{Sig: wasm.FuncType{Params: args, Results: results}, Entry: 0, Locals: append([]wasm.ValType(nil), args...)}
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
			return wasm.ValType{}
		}
		return args[0]
	}
	singleResult := func() wasm.ValType {
		if len(results) == 0 {
			return wasm.ValType{}
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
		return packValType(singleResult())
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
