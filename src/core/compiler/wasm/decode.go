package wasm

// Decode parses a WebAssembly binary module.
func Decode(data []byte) (*Module, error) {
	r := NewReader(data)
	if err := expectMagic(r); err != nil {
		return nil, err
	}
	ver, err := r.LEU32()
	if err != nil {
		return nil, err
	}
	if ver != 1 {
		return nil, &DecodeError{Code: ErrBadVersion, Offset: 4}
	}
	m := &Module{Version: ver}

	for r.HasNext() {
		id, err := r.Byte()
		if err != nil {
			return nil, err
		}
		size, err := r.U32()
		if err != nil {
			return nil, err
		}
		secStart := r.Offset()
		content, err := r.Bytes(int(size))
		if err != nil {
			return nil, err
		}
		sub := NewReader(content)
		if err := parseSection(id, sub, m, secStart); err != nil {
			return nil, err
		}
		if sub.HasNext() {
			return nil, &DecodeError{Code: ErrSectionSizeMismatch, Offset: secStart + sub.Offset()}
		}
	}

	if len(m.Functions) != len(m.Code) {
		return nil, &DecodeError{Code: ErrFuncCodeCountMismatch, Offset: r.Offset()}
	}
	return m, nil
}

func expectMagic(r *Reader) error {
	magic, err := r.Bytes(4)
	if err != nil {
		return err
	}
	if magic[0] != 0x00 || magic[1] != 0x61 || magic[2] != 0x73 || magic[3] != 0x6D {
		return &DecodeError{Code: ErrBadMagic, Offset: 0}
	}
	return nil
}

func parseSection(id byte, r *Reader, m *Module, base int) error {
	switch id {
	case secCustom:
		return parseCustom(r, m)
	case secType:
		return parseTypes(r, m)
	case secImport:
		return parseImports(r, m)
	case secFunction:
		return forEach(r, func() error { i, err := r.U32(); m.Functions = append(m.Functions, i); return err })
	case secTable:
		return forEach(r, func() error { t, err := readTableType(r); m.Tables = append(m.Tables, t); return err })
	case secMemory:
		return forEach(r, func() error { l, err := readLimits(r); m.Memories = append(m.Memories, MemType{l}); return err })
	case secGlobal:
		return parseGlobals(r, m)
	case secExport:
		return parseExports(r, m)
	case secStart:
		i, err := r.U32()
		m.Start = &i
		return err
	case secElement:
		return parseElements(r, m)
	case secCode:
		return parseCode(r, m)
	case secData:
		return parseData(r, m)
	case secDataCount:
		c, err := r.U32()
		m.DataCount = &c
		return err
	default:
		return &DecodeError{Code: ErrUnknownSectionID, Offset: base - 1}
	}
}

func forEach(r *Reader, fn func() error) error {
	n, err := r.U32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < n; i++ {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

func readName(r *Reader) (string, error) {
	n, err := r.U32()
	if err != nil {
		return "", err
	}
	b, err := r.Bytes(int(n))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func readValType(r *Reader) (ValType, error) {
	b, err := r.Byte()
	if err != nil {
		return 0, err
	}
	t := ValType(b)
	if !isValType(t) {
		return 0, &DecodeError{Code: ErrInvalidValType, Offset: r.Offset()}
	}
	return t, nil
}

func readValTypeVec(r *Reader) ([]ValType, error) {
	n, err := r.U32()
	if err != nil {
		return nil, err
	}
	out := make([]ValType, 0, n)
	for i := uint32(0); i < n; i++ {
		t, err := readValType(r)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func readLimits(r *Reader) (Limits, error) {
	flag, err := r.Byte()
	if err != nil {
		return Limits{}, err
	}
	if flag != 0x00 && flag != 0x01 {
		return Limits{}, &DecodeError{Code: ErrBadLimits, Offset: r.Offset()}
	}
	min, err := r.U32()
	if err != nil {
		return Limits{}, err
	}
	l := Limits{Min: min}
	if flag == 0x01 {
		l.Max, err = r.U32()
		if err != nil {
			return Limits{}, err
		}
		l.HasMax = true
	}
	return l, nil
}

func readTableType(r *Reader) (TableType, error) {
	elem, err := readValType(r)
	if err != nil {
		return TableType{}, err
	}
	lim, err := readLimits(r)
	return TableType{Elem: elem, Limits: lim}, err
}

func readGlobalType(r *Reader) (GlobalType, error) {
	val, err := readValType(r)
	if err != nil {
		return GlobalType{}, err
	}
	mut, err := r.Byte()
	if err != nil {
		return GlobalType{}, err
	}
	if mut != 0x00 && mut != 0x01 {
		return GlobalType{}, &DecodeError{Code: ErrBadMutability, Offset: r.Offset()}
	}
	return GlobalType{Val: val, Mutable: mut == 0x01}, nil
}

// readConstExpr returns raw bytes including the terminating end.
func readConstExpr(r *Reader) ([]byte, error) {
	start := r.pos
	for {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		switch op {
		case 0x0B: // end
			return r.data[start:r.pos], nil
		case 0x41: // i32.const
			_, err = r.I32()
		case 0x42: // i64.const
			_, err = r.I64()
		case 0x43: // f32.const
			err = r.Step(4)
		case 0x44: // f64.const
			err = r.Step(8)
		case 0x23: // global.get
			_, err = r.U32()
		case 0xD0: // ref.null heaptype
			_, err = r.Byte()
		case 0xD2: // ref.func
			_, err = r.U32()
		default:
			return nil, &DecodeError{Code: ErrBadConstExpr, Offset: r.pos}
		}
		if err != nil {
			return nil, err
		}
	}
}

func parseCustom(r *Reader, m *Module) error {
	name, err := readName(r)
	if err != nil {
		return err
	}
	data, err := r.Bytes(r.BytesLeft())
	if err != nil {
		return err
	}
	m.Customs = append(m.Customs, Custom{Name: name, Data: data})
	return nil
}

func parseTypes(r *Reader, m *Module) error {
	return forEach(r, func() error {
		form, err := r.Byte()
		if err != nil {
			return err
		}
		if form != 0x60 {
			return &DecodeError{Code: ErrBadTypeForm, Offset: r.Offset()}
		}
		params, err := readValTypeVec(r)
		if err != nil {
			return err
		}
		results, err := readValTypeVec(r)
		if err != nil {
			return err
		}
		m.Types = append(m.Types, FuncType{Params: params, Results: results})
		return nil
	})
}

func parseImports(r *Reader, m *Module) error {
	return forEach(r, func() error {
		mod, err := readName(r)
		if err != nil {
			return err
		}
		name, err := readName(r)
		if err != nil {
			return err
		}
		kind, err := r.Byte()
		if err != nil {
			return err
		}
		imp := Import{Module: mod, Name: name, Kind: ExternKind(kind)}
		switch ExternKind(kind) {
		case ExternFunc:
			imp.TypeIndex, err = r.U32()
		case ExternTable:
			imp.Table, err = readTableType(r)
		case ExternMem:
			imp.Mem.Limits, err = readLimits(r)
		case ExternGlobal:
			imp.Global, err = readGlobalType(r)
		default:
			return &DecodeError{Code: ErrUnknownImportKind, Offset: r.Offset()}
		}
		if err != nil {
			return err
		}
		m.Imports = append(m.Imports, imp)
		return nil
	})
}

func parseGlobals(r *Reader, m *Module) error {
	return forEach(r, func() error {
		gt, err := readGlobalType(r)
		if err != nil {
			return err
		}
		init, err := readConstExpr(r)
		if err != nil {
			return err
		}
		m.Globals = append(m.Globals, Global{Type: gt, Init: init})
		return nil
	})
}

func parseExports(r *Reader, m *Module) error {
	return forEach(r, func() error {
		name, err := readName(r)
		if err != nil {
			return err
		}
		kind, err := r.Byte()
		if err != nil {
			return err
		}
		if ExternKind(kind) > ExternGlobal {
			return &DecodeError{Code: ErrUnknownExportKind, Offset: r.Offset()}
		}
		idx, err := r.U32()
		if err != nil {
			return err
		}
		m.Exports = append(m.Exports, Export{Name: name, Kind: ExternKind(kind), Index: idx})
		return nil
	})
}

// parseCode keeps instruction bytes raw after the local declarations.
func parseCode(r *Reader, m *Module) error {
	return forEach(r, func() error {
		size, err := r.U32()
		if err != nil {
			return err
		}
		body, err := r.Bytes(int(size))
		if err != nil {
			return err
		}
		sub := NewReader(body)
		var locals []LocalEntry
		if err := forEach(sub, func() error {
			cnt, err := sub.U32()
			if err != nil {
				return err
			}
			vt, err := readValType(sub)
			if err != nil {
				return err
			}
			locals = append(locals, LocalEntry{Count: cnt, Type: vt})
			return nil
		}); err != nil {
			return err
		}
		m.Code = append(m.Code, Code{Locals: locals, Body: sub.data[sub.pos:]})
		return nil
	})
}

func parseData(r *Reader, m *Module) error {
	return forEach(r, func() error {
		flags, err := r.U32()
		if err != nil {
			return err
		}
		var d DataSegment
		switch flags {
		case 0:
			d.Offset, err = readConstExpr(r)
		case 1:
			d.Passive = true
		case 2:
			d.MemIdx, err = r.U32()
			if err == nil {
				d.Offset, err = readConstExpr(r)
			}
		default:
			return &DecodeError{Code: ErrBadDataFlags, Offset: r.Offset()}
		}
		if err != nil {
			return err
		}
		n, err := r.U32()
		if err != nil {
			return err
		}
		d.Init, err = r.Bytes(int(n))
		if err != nil {
			return err
		}
		m.Data = append(m.Data, d)
		return nil
	})
}

func readFuncIdxVec(r *Reader) ([]uint32, error) {
	n, err := r.U32()
	if err != nil {
		return nil, err
	}
	out := make([]uint32, 0, n)
	for i := uint32(0); i < n; i++ {
		v, err := r.U32()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func readExprVec(r *Reader) ([][]byte, error) {
	n, err := r.U32()
	if err != nil {
		return nil, err
	}
	out := make([][]byte, 0, n)
	for i := uint32(0); i < n; i++ {
		e, err := readConstExpr(r)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func readElemKind(r *Reader) error {
	b, err := r.Byte()
	if err != nil {
		return err
	}
	if b != 0x00 { // only "funcref" elemkind exists
		return &DecodeError{Code: ErrBadElemKind, Offset: r.Offset()}
	}
	return nil
}

// parseElements handles all MVP/reference-types element segment flag forms.
func parseElements(r *Reader, m *Module) error {
	return forEach(r, func() error {
		flags, err := r.U32()
		if err != nil {
			return err
		}
		e := Element{Flags: flags, ElemType: FuncRef}
		switch flags {
		case 0:
			if e.Offset, err = readConstExpr(r); err == nil {
				e.FuncIdx, err = readFuncIdxVec(r)
			}
		case 1:
			e.Passive = true
			if err = readElemKind(r); err == nil {
				e.FuncIdx, err = readFuncIdxVec(r)
			}
		case 2:
			if e.TableIdx, err = r.U32(); err == nil {
				if e.Offset, err = readConstExpr(r); err == nil {
					if err = readElemKind(r); err == nil {
						e.FuncIdx, err = readFuncIdxVec(r)
					}
				}
			}
		case 3:
			e.Declared = true
			if err = readElemKind(r); err == nil {
				e.FuncIdx, err = readFuncIdxVec(r)
			}
		case 4:
			if e.Offset, err = readConstExpr(r); err == nil {
				e.Exprs, err = readExprVec(r)
			}
		case 5:
			e.Passive = true
			if e.ElemType, err = readValType(r); err == nil {
				e.Exprs, err = readExprVec(r)
			}
		case 6:
			if e.TableIdx, err = r.U32(); err == nil {
				if e.Offset, err = readConstExpr(r); err == nil {
					if e.ElemType, err = readValType(r); err == nil {
						e.Exprs, err = readExprVec(r)
					}
				}
			}
		case 7:
			e.Declared = true
			if e.ElemType, err = readValType(r); err == nil {
				e.Exprs, err = readExprVec(r)
			}
		default:
			return &DecodeError{Code: ErrBadElementFlags, Offset: r.Offset()}
		}
		if err != nil {
			return err
		}
		m.Elements = append(m.Elements, e)
		return nil
	})
}
