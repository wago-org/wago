package wasm3

import "testing"

func TestMoreValidateLimitsAndStartTopology(t *testing.T) {
	t.Run("imported table invalid limit range", func(t *testing.T) {
		max := uint64(1)
		m := &Module{Imports: []Import{{Type: ExternType{Kind: ExternTable, Table: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 2, Max: &max}}}}}}
		expectValidateErr(t, m, ErrInvalidLimitRange)
	})
	t.Run("local table invalid limit range", func(t *testing.T) {
		max := uint64(1)
		m := &Module{Tables: []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 2, Max: &max}}}}}
		expectValidateErr(t, m, ErrInvalidLimitRange)
	})
	t.Run("local memory invalid limit range", func(t *testing.T) {
		max := uint64(1)
		m := &Module{Memories: []MemType{{Limits: Limits{Min: 2, Max: &max}}}}
		expectValidateErr(t, m, ErrInvalidLimitRange)
	})
	t.Run("imported shared memory64 without max", func(t *testing.T) {
		m := &Module{Imports: []Import{{Type: ExternType{Kind: ExternMem, Mem: MemType{Shared: true, Limits: Limits{Addr64: true, Min: 1}}}}}}
		expectValidateErr(t, m, ErrInvalidSharedMemory)
	})
	t.Run("start wrong-kind index", func(t *testing.T) {
		start := FuncIdx(0)
		m := &Module{Memories: []MemType{{}}, Start: &start}
		expectValidateErr(t, m, ErrUnknownFunc)
	})
}

func TestMoreValidateMemory64OffsetsAndOps(t *testing.T) {
	t.Run("memory64 active data offset accepts i64", func(t *testing.T) {
		m := &Module{Memories: []MemType{{Limits: Limits{Addr64: true}}}, Data: []Data{{Mode: DataMode{Kind: DataActive, Offset: Expr{Instrs: []Instruction{{Kind: InstrI64Const}}}}}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("memory64 active data offset rejects i32", func(t *testing.T) {
		m := &Module{Memories: []MemType{{Limits: Limits{Addr64: true}}}, Data: []Data{{Mode: DataMode{Kind: DataActive, Offset: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("memory.size result is i64 for memory64", func(t *testing.T) {
		m := modWithFunc(nil, []ValType{I64}, Instruction{Kind: InstrMemorySize})
		m.Memories = []MemType{{Limits: Limits{Addr64: true}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("memory.grow delta/result are i64 for memory64", func(t *testing.T) {
		m := modWithFunc(nil, []ValType{I64}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrMemoryGrow})
		m.Memories = []MemType{{Limits: Limits{Addr64: true}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("memory.grow rejects i32 delta for memory64", func(t *testing.T) {
		m := modWithFunc(nil, []ValType{I64}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrMemoryGrow})
		m.Memories = []MemType{{Limits: Limits{Addr64: true}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("explicit memarg index out of range", func(t *testing.T) {
		mi := MemIdx(1)
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Load, MemArg: MemArg{Mem: &mi}}, Instruction{Kind: InstrDrop})
		m.Memories = []MemType{{}}
		expectValidateErr(t, m, ErrUnknownMemory)
	})
}

func TestMoreValidateControlBoundaries(t *testing.T) {
	t.Run("block invalid typeidx blocktype", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrBlock, BlockType: BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 99}}})
		expectValidateErr(t, m, ErrUnknownType)
	})
	t.Run("block non-func comptype blocktype", func(t *testing.T) {
		m := &Module{Types: []RecType{{SubTypes: []SubType{{Comp: CompType{Kind: CompStruct}}}}}, FuncTypes: []TypeIdx{{Index: 0}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrBlock, BlockType: BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 0}}}}}}}}
		expectValidateErr(t, m, ErrUnknownType)
	})
	t.Run("block body cannot consume outer stack without params", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrBlock, Body: Expr{Instrs: []Instruction{{Kind: InstrDrop}}}}, Instruction{Kind: InstrDrop})
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("unreachable block body satisfies result", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrBlock, BlockType: BlockType{Kind: BlockVal, Val: I32}, Body: Expr{Instrs: []Instruction{{Kind: InstrUnreachable}}}}, Instruction{Kind: InstrDrop})
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("loop unconditional backedge validates", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrLoop, Body: Expr{Instrs: []Instruction{{Kind: InstrBr, Index: 0}}}})
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
}
