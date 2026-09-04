package wasm

import (
	"errors"
	"testing"
	"unsafe"
)

func ft(params, results []ValType) RecType {
	return RecType{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: params, Results: results}}}}
}

func modWithFunc(params, results []ValType, body ...Instruction) *Module {
	for i := range body {
		if body[i].ext != nil {
			continue
		}
		var (
			align uint8
			ok    bool
		)
		switch body[i].Kind {
		case InstrMemoryAtomicNotify, InstrMemoryAtomicWait32:
			align, ok = 2, true
		case InstrMemoryAtomicWait64:
			align, ok = 3, true
		case InstrAtomicRmw:
			align, ok = atomicRmwEffect(body[i].AtomicOp).align, true
		case InstrAtomicCmpxchg:
			align, ok = atomicCmpxchgEffect(body[i].AtomicOp).align, true
		default:
			if effect, found := lookupAtomicEffect(atomicLoadEffects[:], InstrI32AtomicLoad, body[i].Kind); found {
				align, ok = effect.align, true
			} else if effect, found := lookupAtomicEffect(atomicLoadEffects[:], InstrI32AtomicStore, body[i].Kind); found {
				align, ok = effect.align, true
			}
		}
		if ok {
			body[i].ext = &instrExt{MemArg: MemArg{Align: uint32(align)}}
		}
	}
	return &Module{Types: []RecType{ft(params, results)}, FuncTypes: []TypeIdx{{Index: 0}}, Code: []Func{{Body: Expr{Instrs: body}}}}
}

func expectValidateErr(t *testing.T, m *Module, code ValidationErrorCode) {
	t.Helper()
	err := ValidateModule(m)
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != code {
		t.Fatalf("expected %v, got %v", code, err)
	}
}

func TestValidateStackReuseAcrossFunctionShapes(t *testing.T) {
	nested := []Instruction{
		{Kind: InstrLocalGet, Index: 0},
		{Kind: InstrIf, ext: &instrExt{
			BlockType: BlockType{Kind: BlockVal, Val: I32},
			Then: []Instruction{{Kind: InstrBlock, ext: &instrExt{
				BlockType: BlockType{Kind: BlockVal, Val: I32},
				Body: Expr{Instrs: []Instruction{
					{Kind: InstrLoop, ext: &instrExt{Body: Expr{}}},
					{Kind: InstrI32Const},
				}},
			}}},
			Else: []Instruction{{Kind: InstrI32Const}},
		}},
	}
	m := &Module{
		Types:     []RecType{ft(nil, []ValType{I32}), ft([]ValType{I32}, []ValType{I32}), ft(nil, []ValType{I64})},
		FuncTypes: []TypeIdx{{Index: 0}, {Index: 1}, {Index: 2}},
		Globals:   []Global{{Type: GlobalType{Type: I32}, Init: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}},
		Code: []Func{
			{Body: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}},
			{Body: Expr{Instrs: nested}},
			{Body: Expr{Instrs: []Instruction{{Kind: InstrUnreachable}}}},
		},
	}
	if err := ValidateModule(m); err != nil {
		t.Fatalf("ValidateModule: %v", err)
	}
}

func TestValidateFunctionStackDiscipline(t *testing.T) {
	t.Run("result type mismatch", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, []ValType{I32}, Instruction{Kind: InstrI64Const}), ErrTypeMismatch)
	})
	t.Run("unreachable stack is polymorphic", func(t *testing.T) {
		if err := ValidateModule(modWithFunc(nil, []ValType{I32}, Instruction{Kind: InstrUnreachable})); err != nil {
			t.Fatalf("unreachable should validate: %v", err)
		}
	})
	t.Run("unknown local", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrLocalGet, Index: 0}), ErrUnknownLocal)
	})
	t.Run("local.set value mismatch", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrF32Const}, Instruction{Kind: InstrLocalSet, Index: 0})
		m.Code[0].Locals = Locals{Runs: []LocalRun{{Count: 1, Type: I32}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("local.tee value mismatch", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrF64Const}, Instruction{Kind: InstrLocalTee, Index: 0}, Instruction{Kind: InstrDrop})
		m.Code[0].Locals = Locals{Runs: []LocalRun{{Count: 1, Type: I64}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("if condition must be i32", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrIf}), ErrTypeMismatch)
	})
	t.Run("if branch result mismatch", func(t *testing.T) {
		m := modWithFunc(nil, nil,
			Instruction{Kind: InstrI32Const},
			Instruction{Kind: InstrIf, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: I32}, Then: []Instruction{{Kind: InstrI64Const}}, Else: []Instruction{{Kind: InstrI32Const}}}},
			Instruction{Kind: InstrDrop},
		)
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("select condition type", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrF32Const}, Instruction{Kind: InstrSelect}), ErrTypeMismatch)
	})
	t.Run("select operand type", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrF32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrSelect}), ErrTypeMismatch)
	})
	t.Run("typed select immediate has one type", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrSelect, ext: &instrExt{ValTypes: []ValType{I32, I32}}}), ErrTypeMismatch)
	})
}

func TestValidateBranchesAndCalls(t *testing.T) {
	t.Run("br invalid label", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrBr, Index: 1}), ErrUnknownLabel)
	})
	t.Run("br insufficient payload", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: I32}, Body: Expr{Instrs: []Instruction{{Kind: InstrBr, Index: 0}}}}})
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("br_if condition", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrBrIf, Index: 0}), ErrTypeMismatch)
	})
	t.Run("br_table invalid default", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrBrTable, Index: 2}), ErrUnknownLabel)
	})
	t.Run("br_table bottom payload matches heterogeneous targets", func(t *testing.T) {
		m := modWithFunc(nil, nil,
			Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: F64}, Body: Expr{Instrs: []Instruction{
				{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: F32}, Body: Expr{Instrs: []Instruction{
					{Kind: InstrUnreachable},
					{Kind: InstrI32Const},
					{Kind: InstrBrTable, Index: 1, ext: &instrExt{Indices: []uint32{0}}},
				}}}},
				{Kind: InstrDrop},
				{Kind: InstrF64Const},
			}}}},
			Instruction{Kind: InstrDrop},
		)
		if err := ValidateModule(m); err != nil {
			t.Fatalf("bottom-polymorphic br_table should validate: %v", err)
		}
	})
	t.Run("reachable br_table target type mismatch", func(t *testing.T) {
		m := modWithFunc(nil, nil,
			Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: I64}, Body: Expr{Instrs: []Instruction{
				{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: I32}, Body: Expr{Instrs: []Instruction{
					{Kind: InstrI64Const},
					{Kind: InstrI32Const},
					{Kind: InstrBrTable, Index: 1, ext: &instrExt{Indices: []uint32{0}}},
				}}}},
				{Kind: InstrDrop},
				{Kind: InstrI64Const},
			}}}},
			Instruction{Kind: InstrDrop},
		)
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("direct call payload mismatch", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{ft([]ValType{I32}, nil), ft(nil, nil)},
			Imports:   []Import{{Type: NewFuncExternType(TypeIdx{Index: 0})}},
			FuncTypes: []TypeIdx{{Index: 1}},
			Code:      []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrCall, Index: 0}}}}},
		}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("return_call result mismatch", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{ft(nil, []ValType{I64}), ft(nil, []ValType{I32})},
			Imports:   []Import{{Type: NewFuncExternType(TypeIdx{Index: 0})}},
			FuncTypes: []TypeIdx{{Index: 1}},
			Code:      []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrReturnCall, Index: 0}}}}},
		}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}

func TestBranchTableFrameEpochDeduplicatesWithoutAllocation(t *testing.T) {
	v := funcValidator{ctrls: make([]ctrlFrame, maxInstructionNestingDepth)}
	v.beginBranchTable()
	if !v.markBranchTableLabel(17) {
		t.Fatal("first label was already present")
	}
	for i := 0; i < 100_000; i++ {
		if v.markBranchTableLabel(17) {
			t.Fatalf("duplicate label %d reported fresh", i)
		}
	}
	deep := uint32(maxInstructionNestingDepth - 1)
	if !v.markBranchTableLabel(deep) || v.markBranchTableLabel(deep) {
		t.Fatal("maximum-depth label was not deduplicated")
	}
	v.beginBranchTable()
	if !v.markBranchTableLabel(17) {
		t.Fatal("label remained marked in a later table")
	}

	// A reused synthetic validator must clear stale frame stamps on epoch rollover.
	v.branchTableEpoch = ^uint32(0)
	v.ctrls[0].branchTableEpoch = 1
	v.beginBranchTable()
	if v.branchTableEpoch != 1 || v.ctrls[0].branchTableEpoch != 0 {
		t.Fatal("epoch rollover did not clear active frame stamps")
	}

	indices := make([]uint32, 1_000)
	for i := range indices {
		indices[i] = uint32(i % maxInstructionNestingDepth)
	}
	in := Instruction{Kind: InstrBrTable, Index: 0, ext: &instrExt{Indices: indices}}
	vals := []val{{t: I32}}
	if allocs := testing.AllocsPerRun(100, func() {
		v.vals = vals
		v.ctrls[len(v.ctrls)-1].unreachable = false
		if err := v.step(&in); err != nil {
			panic(err)
		}
	}); allocs != 0 {
		t.Fatalf("branch-table validation allocations = %.2f, want 0", allocs)
	}
}

func TestBranchTableFrameEpochFitsValidatorPadding(t *testing.T) {
	// The embedded byte reader also holds the shared decode-budget pointer.
	wantValidator, wantFrame := uintptr(656)+unsafe.Sizeof((*decodeBudget)(nil)), uintptr(80)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		wantValidator, wantFrame = 412+unsafe.Sizeof((*decodeBudget)(nil)), 44
	}
	if got := unsafe.Sizeof(funcValidator{}); got != wantValidator {
		t.Fatalf("funcValidator size = %d, want %d", got, wantValidator)
	}
	if got := unsafe.Sizeof(ctrlFrame{}); got != wantFrame {
		t.Fatalf("ctrlFrame size = %d, want %d", got, wantFrame)
	}
}

func TestValidateModuleLevelIndexes(t *testing.T) {
	t.Run("unknown function type", func(t *testing.T) {
		expectValidateErr(t, &Module{Types: []RecType{ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 99}}, Code: []Func{{}}}, ErrUnknownType)
	})
	t.Run("tag invalid type", func(t *testing.T) {
		expectValidateErr(t, &Module{Types: []RecType{ft(nil, nil)}, Tags: []TagType{{Type: TypeIdx{Index: 2}}}}, ErrUnknownType)
	})
	t.Run("tag result type", func(t *testing.T) {
		expectValidateErr(t, &Module{Types: []RecType{ft(nil, []ValType{I32})}, Tags: []TagType{{Type: TypeIdx{Index: 0}}}}, ErrTypeMismatch)
		expectValidateErr(t, &Module{Types: []RecType{ft(nil, []ValType{I32})}, Imports: []Import{{Type: NewTagExternType(TagType{Type: TypeIdx{Index: 0}})}}}, ErrTypeMismatch)
	})
	badRef := RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 99}), false))
	t.Run("function signature unknown heap type", func(t *testing.T) {
		expectValidateErr(t, &Module{Types: []RecType{ft(nil, []ValType{badRef})}}, ErrUnknownType)
	})
	badField := field(badRef, Const)
	t.Run("struct field unknown heap type", func(t *testing.T) {
		expectValidateErr(t, &Module{Types: []RecType{{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompStruct, Fields: []FieldType{badField}}}}}}}, ErrUnknownType)
	})
	t.Run("imported global unknown heap type", func(t *testing.T) {
		expectValidateErr(t, &Module{Types: []RecType{ft(nil, nil)}, Imports: []Import{{Type: NewGlobalExternType(GlobalType{Type: badRef})}}}, ErrUnknownType)
	})
	t.Run("table unknown heap type", func(t *testing.T) {
		expectValidateErr(t, &Module{Tables: []Table{{Type: TableType{Ref: badRef.Ref(), Limits: Limits{Min: 1}}}}}, ErrUnknownType)
	})
	t.Run("unused local unknown heap type", func(t *testing.T) {
		m := modWithFunc(nil, nil)
		m.Code[0].Locals = Locals{Runs: []LocalRun{{Count: 1, Type: badRef}}}
		expectValidateErr(t, m, ErrUnknownType)
	})
	t.Run("data count too small", func(t *testing.T) {
		c := uint32(0)
		m := &Module{Memories: []MemType{{}}, DataCount: &c, Data: []Data{{Mode: DataMode{Kind: DataPassive}}}}
		expectValidateErr(t, m, ErrInvalidDataCount)
	})
	t.Run("invalid export indexes", func(t *testing.T) {
		cases := []ExternIdx{{Kind: ExternFunc}, {Kind: ExternTable}, {Kind: ExternMem}, {Kind: ExternGlobal}, {Kind: ExternTag}}
		for _, idx := range cases {
			expectValidateErr(t, &Module{Exports: []Export{{Name: "x", Index: idx}}}, ErrUnknownFunc)
		}
	})
	t.Run("duplicate export", func(t *testing.T) {
		m := &Module{Types: []RecType{ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 0}}, Code: []Func{{}}, Exports: []Export{{Name: "x", Index: ExternIdx{Kind: ExternFunc, Index: 0}}, {Name: "x", Index: ExternIdx{Kind: ExternFunc, Index: 0}}}}
		expectValidateErr(t, m, ErrDuplicateExport)
	})
	t.Run("start function with parameter", func(t *testing.T) {
		start := FuncIdx(0)
		m := &Module{Types: []RecType{ft([]ValType{I32}, nil)}, Imports: []Import{{Type: NewFuncExternType(TypeIdx{Index: 0})}}, Start: &start}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("start function with result", func(t *testing.T) {
		start := FuncIdx(0)
		m := &Module{Types: []RecType{ft(nil, []ValType{I32})}, Imports: []Import{{Type: NewFuncExternType(TypeIdx{Index: 0})}}, Start: &start}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}

func TestValidateGlobalsTablesMemoryAndConstExprs(t *testing.T) {
	t.Run("immutable global.set", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrGlobalSet, Index: 0})
		m.Imports = []Import{{Type: NewGlobalExternType(GlobalType{Type: I32})}}
		expectValidateErr(t, m, ErrImmutableGlobal)
	})
	t.Run("global initializer cannot read local global", func(t *testing.T) {
		m := &Module{Globals: []Global{{Type: GlobalType{Type: I32}, Init: Expr{Instrs: []Instruction{{Kind: InstrGlobalGet, Index: 0}}}}}}
		expectValidateErr(t, m, ErrConstExprRequired)
	})
	t.Run("global initializer can read imported immutable global", func(t *testing.T) {
		m := &Module{Imports: []Import{{Type: NewGlobalExternType(GlobalType{Type: I32})}}, Globals: []Global{{Type: GlobalType{Type: I32}, Init: Expr{Instrs: []Instruction{{Kind: InstrGlobalGet, Index: 0}}}}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("table.set value type", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrTableSet, Index: 0})
		m.Tables = []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("table.get index type", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrTableGet, Index: 0}, Instruction{Kind: InstrDrop})
		m.Tables = []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	makeTable64Elem := func(offset Instruction) *Module {
		return &Module{
			Tables:   []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1, Addr64: true}}}},
			Elements: []Elem{{Mode: ElemMode{Kind: ElemActive, Offset: Expr{Instrs: []Instruction{offset}}}, Kind: ElemKind{Kind: ElemFuncs}}},
		}
	}
	t.Run("table64 active element offset accepts i64", func(t *testing.T) {
		if err := ValidateModule(makeTable64Elem(Instruction{Kind: InstrI64Const})); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("table64 active element offset rejects i32", func(t *testing.T) {
		expectValidateErr(t, makeTable64Elem(Instruction{Kind: InstrI32Const}), ErrTypeMismatch)
	})
	// Active segments write directly into the target table; funcref element
	// payloads are not assignment-compatible with an externref table.
	t.Run("active element type must match table type", func(t *testing.T) {
		m := &Module{
			Tables:   []Table{{Type: TableType{Ref: AbsRef(HeapExtern), Limits: Limits{Min: 1}}}},
			Elements: []Elem{{Mode: ElemMode{Kind: ElemActive, Offset: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}, Kind: ElemKind{Kind: ElemFuncs}}},
		}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("memory load requires memory", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Load}), ErrUnknownMemory)
	})
	t.Run("memory.grow delta type", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrMemoryGrow}, Instruction{Kind: InstrDrop})
		m.Memories = []MemType{{}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("memory load alignment", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Load, ext: &instrExt{MemArg: MemArg{Align: 3}}}, Instruction{Kind: InstrDrop})
		m.Memories = []MemType{{}}
		expectValidateErr(t, m, ErrInvalidAlignment)
	})
	t.Run("memory64 load address type", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Load}, Instruction{Kind: InstrDrop})
		m.Memories = []MemType{{Limits: Limits{Addr64: true}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("string.const validates against stringrefs", func(t *testing.T) {
		m := modWithFunc(nil, []ValType{StringRef}, Instruction{Kind: InstrStringConst, Index: 0})
		m.StringRefs = [][]byte{[]byte("hello")}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("string.const invalid index", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, []ValType{StringRef}, Instruction{Kind: InstrStringConst, Index: 1}), ErrTypeMismatch)
	})
	t.Run("func and string refs do not subtype anyref", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, []ValType{AnyRef}, Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}), ErrTypeMismatch)
		expectValidateErr(t, modWithFunc(nil, []ValType{AnyRef}, Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapString)}}), ErrTypeMismatch)
		m := modWithFunc(nil, []ValType{AnyRef}, Instruction{Kind: InstrStringConst, Index: 0})
		m.StringRefs = [][]byte{[]byte("hello")}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("noexn is the exception bottom type", func(t *testing.T) {
		m := modWithFunc(nil, []ValType{RefVal(AbsRef(HeapExn))}, Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapNoExn)}})
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("nofunc is below indexed function heaps", func(t *testing.T) {
		m := &Module{Types: []RecType{ft(nil, nil), ft(nil, []ValType{RefVal(Ref(true, IndexedHeap(TypeIdx{Index: 0}), false))})}, FuncTypes: []TypeIdx{{Index: 1}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapNoFunc)}}}}}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
}
