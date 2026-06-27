package wasm

import "testing"

func TestBulkMemoryValidationEdges(t *testing.T) {
	t.Run("memory.fill value type", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrMemoryFill})
		m.Memories = []MemType{{}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("memory.fill dest type", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrMemoryFill})
		m.Memories = []MemType{{}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("memory.copy source memory index", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrMemoryCopy, Index: 0, Index2: 1})
		m.Memories = []MemType{{}}
		expectValidateErr(t, m, ErrUnknownMemory)
	})
	t.Run("memory64.copy source type", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrMemoryCopy, Index: 0, Index2: 0})
		m.Memories = []MemType{{Limits: Limits{Addr64: true}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("memory.init data index", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrMemoryInit, Index: 1, Index2: 0})
		m.Memories = []MemType{{}}
		count := uint32(1)
		m.DataCount = &count
		m.Data = []Data{{Mode: DataMode{Kind: DataPassive}}}
		expectValidateErr(t, m, ErrInvalidDataCount)
	})
	// memory.init/data.drop are bulk-memory instructions: the data count
	// section must be present, and the target data segment must be passive.
	t.Run("memory.init requires data count", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrMemoryInit, Index: 0, Index2: 0})
		m.Memories = []MemType{{}}
		m.Data = []Data{{Mode: DataMode{Kind: DataPassive}}}
		expectValidateErr(t, m, ErrInvalidDataCount)
	})
	t.Run("memory.init rejects active data", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrMemoryInit, Index: 0, Index2: 0})
		m.Memories = []MemType{{}}
		count := uint32(1)
		m.DataCount = &count
		m.Data = []Data{{Mode: DataMode{Kind: DataActive, Offset: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("data.drop index", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrDataDrop, Index: 0}), ErrInvalidDataCount)
	})
	t.Run("data.drop rejects active data", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrDataDrop, Index: 0})
		m.Memories = []MemType{{}}
		count := uint32(1)
		m.DataCount = &count
		m.Data = []Data{{Mode: DataMode{Kind: DataActive, Offset: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}

func TestTableBulkValidationEdges(t *testing.T) {
	funcrefTable := Table{Type: TableType{Ref: AbsRef(HeapFunc), Limits: Limits{Min: 1}}}
	elem := Elem{Mode: ElemMode{Kind: ElemPassive}, Kind: ElemKind{Kind: ElemFuncs}}
	t.Run("table.init table index", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrTableInit, Index: 0, Index2: 1})
		m.Tables = []Table{funcrefTable}
		m.Elements = []Elem{elem}
		expectValidateErr(t, m, ErrUnknownTable)
	})
	t.Run("table.init source type", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrTableInit, Index: 0, Index2: 0})
		m.Tables = []Table{funcrefTable}
		m.Elements = []Elem{elem}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	// table.init consumes only passive element segments; active/declarative
	// segments have already been applied or discarded at instantiation time.
	// The element reference type must also be compatible with the table.
	for name, elem := range map[string]Elem{
		"active segment":      {Mode: ElemMode{Kind: ElemActive, Offset: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}, Kind: ElemKind{Kind: ElemFuncs}},
		"declarative segment": {Mode: ElemMode{Kind: ElemDeclarative}, Kind: ElemKind{Kind: ElemFuncs}},
		"externref segment":   {Mode: ElemMode{Kind: ElemPassive}, Kind: ElemKind{Kind: ElemTypedExprs, Ref: AbsRef(HeapExtern), Exprs: []Expr{{Instrs: []Instruction{{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapExtern)}}}}}}},
	} {
		t.Run("table.init rejects "+name, func(t *testing.T) {
			m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrTableInit, Index: 0, Index2: 0})
			m.Tables = []Table{funcrefTable}
			m.Elements = []Elem{elem}
			expectValidateErr(t, m, ErrTypeMismatch)
		})
	}
	t.Run("elem.drop rejects active segment", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrElemDrop, Index: 0})
		m.Tables = []Table{funcrefTable}
		m.Elements = []Elem{{Mode: ElemMode{Kind: ElemActive, Offset: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}, Kind: ElemKind{Kind: ElemFuncs}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("table.copy dest index", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrTableCopy, Index: 1, Index2: 0})
		m.Tables = []Table{funcrefTable}
		expectValidateErr(t, m, ErrUnknownTable)
	})
	t.Run("table.grow value type", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrTableGrow}, Instruction{Kind: InstrDrop})
		m.Tables = []Table{funcrefTable}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("table.fill length type", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: AbsRef(HeapFunc)}}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrTableFill})
		m.Tables = []Table{funcrefTable}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("call_indirect rejects externref table", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrCallIndirect, Index: 0, Index2: 0})
		m.Tables = []Table{{Type: TableType{Ref: AbsRef(HeapExtern), Limits: Limits{Min: 1}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}
