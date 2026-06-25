package wasm

func (m *Module) importedGlobalCount() int {
	n := 0
	for i := range m.Imports {
		if m.Imports[i].Kind == ExternGlobal {
			n++
		}
	}
	return n
}

func (m *Module) funcCount() int { return m.ImportedFuncCount() + len(m.Functions) }
func (m *Module) globalCnt() int { return m.importedGlobalCount() + len(m.Globals) }

func (m *Module) tableType(idx uint32) (TableType, bool) {
	i := 0
	for j := range m.Imports {
		if m.Imports[j].Kind != ExternTable {
			continue
		}
		if uint32(i) == idx {
			return m.Imports[j].Table, true
		}
		i++
	}
	local := int(idx) - i
	if local < 0 || local >= len(m.Tables) {
		return TableType{}, false
	}
	return m.Tables[local], true
}

func (m *Module) FuncSignature(idx uint32) (*FuncType, bool) { return m.funcType(idx) }

func merr(c ErrCode) error { return &ValidationError{Code: c, Func: -1} }

// validateConstExpr allows const ops and imported immutable global.get.
func (m *Module) validateConstExpr(expr []byte, want ValType) error {
	r := NewReader(expr)
	var produced ValType
	count := 0
	for {
		op, err := r.Byte()
		if err != nil {
			return merr(ErrConstExprRequired)
		}
		switch op {
		case 0x0B: // end
			if count != 1 {
				return merr(ErrTypeMismatch)
			}
			if produced != want {
				return merr(ErrTypeMismatch)
			}
			return nil
		case 0x41:
			if _, err := r.I32(); err != nil {
				return err
			}
			produced = I32
		case 0x42:
			if _, err := r.I64(); err != nil {
				return err
			}
			produced = I64
		case 0x43:
			if err := r.Step(4); err != nil {
				return err
			}
			produced = F32
		case 0x44:
			if err := r.Step(8); err != nil {
				return err
			}
			produced = F64
		case 0x23: // global.get
			x, err := r.U32()
			if err != nil {
				return err
			}
			gt, ok := m.globalType(x)
			if !ok {
				return merr(ErrUnknownGlobal)
			}
			if int(x) >= m.importedGlobalCount() || gt.Mutable {
				return merr(ErrConstExprRequired) // only imported immutable globals
			}
			produced = gt.Val
		case 0xD0: // ref.null t
			b, err := r.Byte()
			if err != nil {
				return err
			}
			produced = ValType(b)
		case 0xD2: // ref.func
			if _, err := r.U32(); err != nil {
				return err
			}
			produced = FuncRef
		default:
			return merr(ErrConstExprRequired)
		}
		count++
		if count > 1 {
			return merr(ErrTypeMismatch)
		}
	}
}

// validateModuleLevel checks cross-section indexes and const expressions.
func (m *Module) validateModuleLevel() error {
	for i := range m.Imports {
		if m.Imports[i].Kind == ExternFunc && int(m.Imports[i].TypeIndex) >= len(m.Types) {
			return merr(ErrUnknownType)
		}
	}
	for _, ti := range m.Functions {
		if int(ti) >= len(m.Types) {
			return merr(ErrUnknownType)
		}
	}
	for i := range m.Globals {
		if err := m.validateConstExpr(m.Globals[i].Init, m.Globals[i].Type.Val); err != nil {
			return err
		}
	}
	fc, tc, mc, gc := m.funcCount(), m.tableCount(), m.memCount(), m.globalCnt()
	for i := range m.Exports {
		e := &m.Exports[i]
		ok := false
		switch e.Kind {
		case ExternFunc:
			ok = int(e.Index) < fc
		case ExternTable:
			ok = int(e.Index) < tc
		case ExternMem:
			ok = int(e.Index) < mc
		case ExternGlobal:
			ok = int(e.Index) < gc
		}
		if !ok {
			return merr(ErrUnknownExport)
		}
	}
	for i := range m.Elements {
		if err := m.validateElement(&m.Elements[i], fc, tc); err != nil {
			return err
		}
	}
	for i := range m.Data {
		d := &m.Data[i]
		if !d.Passive {
			if int(d.MemIdx) >= mc {
				return merr(ErrUnknownMemory)
			}
			if err := m.validateConstExpr(d.Offset, I32); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateElement verifies table compatibility and referenced function indexes.
func (m *Module) validateElement(e *Element, funcCnt, tblCnt int) error {
	active := !e.Passive && !e.Declared
	if active {
		if int(e.TableIdx) >= tblCnt {
			return merr(ErrUnknownTable)
		}
		tt, ok := m.tableType(e.TableIdx)
		if !ok {
			return merr(ErrUnknownTable)
		}
		if tt.Elem != e.ElemType {
			return merr(ErrTypeMismatch)
		}
		if err := m.validateConstExpr(e.Offset, I32); err != nil {
			return err
		}
	}
	for _, fi := range e.FuncIdx {
		if int(fi) >= funcCnt {
			return merr(ErrUnknownFunc)
		}
	}
	for _, ex := range e.Exprs {
		if err := m.validateConstExpr(ex, e.ElemType); err != nil {
			return err
		}
	}
	return nil
}
