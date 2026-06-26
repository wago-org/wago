package wasm3

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
		m.Data = []Data{{Mode: DataMode{Kind: DataPassive}}}
		expectValidateErr(t, m, ErrInvalidDataCount)
	})
	t.Run("data.drop index", func(t *testing.T) {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrDataDrop, Index: 0}), ErrInvalidDataCount)
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
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrRefNull, RefType: AbsRef(HeapFunc)}, Instruction{Kind: InstrI64Const}, Instruction{Kind: InstrTableFill})
		m.Tables = []Table{funcrefTable}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
	t.Run("call_indirect rejects externref table", func(t *testing.T) {
		m := modWithFunc(nil, nil, Instruction{Kind: InstrI32Const}, Instruction{Kind: InstrCallIndirect, Index: 0, Index2: 0})
		m.Tables = []Table{{Type: TableType{Ref: AbsRef(HeapExtern), Limits: Limits{Min: 1}}}}
		expectValidateErr(t, m, ErrTypeMismatch)
	})
}
