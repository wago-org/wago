package wasm

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
	// The decoder stores table/memory proposal limits as uint64, but non-64-bit
	// limits must still obey their original 32-bit/page-count bounds.
	// Otherwise invalid MVP modules could pass validation and fail only later.
	boundsMax := uint64(maxMemory32Pages + 1)
	uint32MaxPlusOne := uint64(maxTable32Limit + 1)
	t.Run("table32 limit over u32 range", func(t *testing.T) {
		m := &Module{Tables: []Table{{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: uint32MaxPlusOne}}}}}
		expectValidateErr(t, m, ErrInvalidLimitRange)
	})
	t.Run("memory32 min over page range", func(t *testing.T) {
		m := &Module{Memories: []MemType{{Limits: Limits{Min: maxMemory32Pages + 1}}}}
		expectValidateErr(t, m, ErrInvalidLimitRange)
	})
	t.Run("memory32 max over page range", func(t *testing.T) {
		m := &Module{Memories: []MemType{{Limits: Limits{Min: 0, Max: &boundsMax}}}}
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
	t.Run("memory.size rejects nonzero memory index", func(t *testing.T) {
		m := modWithFunc(nil, []ValType{I64}, Instruction{Kind: InstrMemorySize, Index: 1})
		m.Memories = []MemType{{Limits: Limits{Addr64: true}}}
		expectValidateErr(t, m, ErrUnknownMemory)
	})
	t.Run("memory.grow rejects nonzero memory index", func(t *testing.T) {
		m := modWithFunc(nil, []ValType{I64}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrMemoryGrow, Index: 1})
		m.Memories = []MemType{{Limits: Limits{Addr64: true}}}
		expectValidateErr(t, m, ErrUnknownMemory)
	})
	t.Run("memory64.init uses i64 destination and i32 data offsets", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrMemoryInit, Index: 0, Index2: 0})
		m.Memories = []MemType{{Limits: Limits{Addr64: true}}}
		count := uint32(1)
		m.DataCount = &count
		m.Data = []Data{{Mode: DataMode{Kind: DataPassive}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("memory64.init length remains i32", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrMemoryInit, Index: 0, Index2: 0})
		m.Memories = []MemType{{Limits: Limits{Addr64: true}}}
		count := uint32(1)
		m.DataCount = &count
		m.Data = []Data{{Mode: DataMode{Kind: DataPassive}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("explicit memarg index out of range", func(t *testing.T) {
		mi := MemIdx(1)
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Load, ext: &instrExt{MemArg: MemArg{Mem: &mi}}}, Instruction{Kind: InstrDrop})
		m.Memories = []MemType{{}}
		expectValidateErr(t, m, ErrUnknownMemory)
	})
}

func TestMoreValidateTable64Ops(t *testing.T) {
	table64 := Table{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1, Addr64: true}}}
	t.Run("table.get uses i64 index", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrTableGet, Index: 0}, Instruction{Kind: InstrDrop})
		m.Tables = []Table{table64}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("table.get rejects i32 index", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrTableGet, Index: 0}, Instruction{Kind: InstrDrop})
		m.Tables = []Table{table64}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("call_indirect uses table64 index", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrCallIndirect, Index: 0, Index2: 0})
		m.Tables = []Table{table64}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("table.size and table.grow use i64 sizes", func(t *testing.T) {
		size := modWithFunc(nil, []ValType{I64}, Instruction{Kind: InstrTableSize, Index: 0})
		size.Tables = []Table{table64}
		if err := ValidateModule(size); err != nil {
			t.Fatalf("table.size ValidateModule: %v", err)
		}
		grow := modWithFunc(nil, []ValType{I64}, Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrTableGrow, Index: 0})
		grow.Tables = []Table{table64}
		if err := ValidateModule(grow); err != nil {
			t.Fatalf("table.grow ValidateModule: %v", err)
		}
	})
	t.Run("table.fill length uses i64", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrTableFill, Index: 0})
		m.Tables = []Table{table64}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("table.init uses table64 destination and i32 element offsets", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrTableInit, Index: 0, Index2: 0})
		m.Tables = []Table{table64}
		m.Elements = []Elem{{Mode: ElemMode{Kind: ElemPassive}, Kind: ElemKind{Kind: ElemFuncs}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("table.copy may use source and destination address widths", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrTableCopy, Index: 0, Index2: 1})
		m.Tables = []Table{table64, Table{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("table.copy length uses minimum address width", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrTableCopy, Index: 0, Index2: 1})
		m.Tables = []Table{table64, Table{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}

func TestValidateEmptyTypedElementSegmentChecksRefType(t *testing.T) {
	m := &Module{Elements: []Elem{{
		Mode: ElemMode{Kind: ElemPassive},
		Kind: ElemKind{Kind: ElemTypedExprs, Ref: Ref(true, IndexedHeap(TypeIdx{Index: 99}), false)},
	}}}
	// Empty element segments still declare a reference type, and that type may
	// contain indexed heap types that must be checked against the module types.
	expectValidateErr(t, m, ErrUnknownType)
}

func TestMoreValidateControlBoundaries(t *testing.T) {
	t.Run("block invalid typeidx blocktype", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 99}}}})
		expectValidateErr(t, m, ErrUnknownType)
	})
	t.Run("block non-func comptype blocktype", func(t *testing.T) {
		m := &Module{Types: []RecType{{SubTypes: []SubType{{Comp: CompType{Kind: CompStruct}}}}}, FuncTypes: []TypeIdx{{Index: 0}}, Code: []Func{{Body: Expr{Instrs: []Instruction{{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: 0}}}}}}}}}
		expectValidateErr(t, m, ErrUnknownType)
	})
	t.Run("block body cannot consume outer stack without params", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrBlock, ext: &instrExt{Body: Expr{Instrs: []Instruction{{Kind: InstrDrop}}}}}, Instruction{Kind: InstrDrop})
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("unreachable block body satisfies result", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: BlockType{Kind: BlockVal, Val: I32}, Body: Expr{Instrs: []Instruction{{Kind: InstrUnreachable}}}}}, Instruction{Kind: InstrDrop})
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("loop unconditional backedge validates", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrLoop, ext: &instrExt{Body: Expr{Instrs: []Instruction{{Kind: InstrBr, Index: 0}}}}})
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
}
