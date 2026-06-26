package wasm3

import (
	"errors"
	"testing"
)

func ft(params, results []ValType) RecType {
	return RecType{SubTypes: []SubType{{Final: true, Comp: CompType{Kind: CompFunc, Params: params, Results: results}}}}
}

func modWithFunc(params, results []ValType, body ...Instruction) *Module {
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
			Instruction{Kind: InstrIf, BlockType: BlockType{Kind: BlockVal, Val: I32}, Then: []Instruction{{Kind: InstrI64Const}}, Else: []Instruction{{Kind: InstrI32Const}}},
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
}

func TestValidateBranchesAndCalls(t *testing.T) {
	t.Run("br invalid label", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrBr, Index: 1}), ErrUnknownLabel)
	})
	t.Run("br insufficient payload", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrBlock, BlockType: BlockType{Kind: BlockVal, Val: I32}, Body: Expr{Instrs: []Instruction{{Kind: InstrBr, Index: 0}}}})
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("br_if condition", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrBrIf, Index: 0}), ErrTypeMismatch)
	})
	t.Run("br_table invalid default", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrBrTable, Index: 2}), ErrUnknownLabel)
	})
	t.Run("br_table target type mismatch", func(t *testing.T) {
		m := modWithFunc(nil, nil,
			Instruction{Kind: InstrBlock, BlockType: BlockType{Kind: BlockVal, Val: I64}, Body: Expr{Instrs: []Instruction{
				{Kind: InstrBlock, BlockType: BlockType{Kind: BlockVal, Val: I32}, Body: Expr{Instrs: []Instruction{
					{Kind: InstrI64Const},
					{Kind: InstrI32Const},
					{Kind: InstrBrTable, Index: 1, Indices: []uint32{0}},
				}}},
				{Kind: InstrDrop},
				{Kind: InstrI64Const},
			}}},
			Instruction{Kind: InstrDrop},
		)
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("direct call payload mismatch", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{ft([]ValType{I32}, nil), ft(nil, nil)},
			Imports:   []Import{{Type: ExternType{Kind: ExternFunc, Type: TypeIdx{Index: 0}}}},
			FuncTypes: []TypeIdx{{Index: 1}},
			Code:      []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrCall, Index: 0}}}}},
		}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("return_call result mismatch", func(t *testing.T) {
		m := &Module{
			Types:     []RecType{ft(nil, []ValType{I64}), ft(nil, []ValType{I32})},
			Imports:   []Import{{Type: ExternType{Kind: ExternFunc, Type: TypeIdx{Index: 0}}}},
			FuncTypes: []TypeIdx{{Index: 1}},
			Code:      []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrReturnCall, Index: 0}}}}},
		}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}

func TestValidateModuleLevelIndexes(t *testing.T) {
	t.Run("unknown function type", func(t *testing.T) {
		expectValidateErr(t, &Module{Types: []RecType{ft(nil, nil)}, FuncTypes: []TypeIdx{{Index: 99}}, Code: []Func{{}}}, ErrUnknownType)
	})
	t.Run("tag invalid type", func(t *testing.T) {
		expectValidateErr(t, &Module{Types: []RecType{ft(nil, nil)}, Tags: []TagType{{Type: TypeIdx{Index: 2}}}}, ErrUnknownType)
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
		expectValidateErr(t, &Module{Types: []RecType{ft(nil, nil)}, Imports: []Import{{Type: ExternType{Kind: ExternGlobal, Global: GlobalType{Type: badRef}}}}}, ErrUnknownType)
	})
	t.Run("table unknown heap type", func(t *testing.T) {
		expectValidateErr(t, &Module{Tables: []Table{{Type: TableType{Ref: badRef.Ref, Limits: Limits{Min: 1}}}}}, ErrUnknownType)
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
		m := &Module{Types: []RecType{ft([]ValType{I32}, nil)}, Imports: []Import{{Type: ExternType{Kind: ExternFunc, Type: TypeIdx{Index: 0}}}}, Start: &start}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("start function with result", func(t *testing.T) {
		start := FuncIdx(0)
		m := &Module{Types: []RecType{ft(nil, []ValType{I32})}, Imports: []Import{{Type: ExternType{Kind: ExternFunc, Type: TypeIdx{Index: 0}}}}, Start: &start}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}

func TestValidateGlobalsTablesMemoryAndConstExprs(t *testing.T) {
	t.Run("immutable global.set", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrGlobalSet, Index: 0})
		m.Imports = []Import{{Type: ExternType{Kind: ExternGlobal, Global: GlobalType{Type: I32}}}}
		expectValidateErr(t, m, ErrImmutableGlobal)
	})
	t.Run("global initializer cannot read local global", func(t *testing.T) {
		m := &Module{Globals: []Global{{Type: GlobalType{Type: I32}, Init: Expr{Instrs: []Instruction{{Kind: InstrGlobalGet, Index: 0}}}}}}
		expectValidateErr(t, m, ErrConstExprRequired)
	})
	t.Run("global initializer can read imported immutable global", func(t *testing.T) {
		m := &Module{Imports: []Import{{Type: ExternType{Kind: ExternGlobal, Global: GlobalType{Type: I32}}}}, Globals: []Global{{Type: GlobalType{Type: I32}, Init: Expr{Instrs: []Instruction{{Kind: InstrGlobalGet, Index: 0}}}}}}
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
	t.Run("memory load requires memory", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Load}), ErrUnknownMemory)
	})
	t.Run("memory.grow delta type", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrMemoryGrow}, Instruction{Kind: InstrDrop})
		m.Memories = []MemType{{}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("memory load alignment", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Load, MemArg: MemArg{Align: 3}}, Instruction{Kind: InstrDrop})
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
}
