package wasm

// decodeModuleASTForTest is the test-only reference decoder used as an
// independent oracle for byte-backed/no-body decoding. Unlike DecodeModule, it
// materializes structured instruction trees for function bodies and
// const-expression fields.
func decodeModuleASTForTest(data []byte) (*Module, error) {
	r := newReader(data)
	magic, err := r.bytes(4)
	if err != nil {
		return nil, err
	}
	if string(magic) != "\x00asm" {
		return nil, &DecodeError{Code: ErrBadMagic, Offset: 0}
	}
	ver, err := r.le32()
	if err != nil {
		return nil, err
	}
	if ver != 1 {
		return nil, &DecodeError{Code: ErrBadVersion, Offset: 4}
	}
	m := &Module{}
	lastOrder := 0
	seen := map[byte]bool{}
	seenName := false
	for r.has() {
		id, err := r.byte()
		if err != nil {
			return nil, err
		}
		size, err := r.u32()
		if err != nil {
			return nil, err
		}
		start := r.off()
		payload, err := r.bytes(int(size))
		if err != nil {
			return nil, err
		}
		end := r.off()
		if id != secCustom {
			ord, ok := sectionOrder[id]
			if !ok {
				return nil, &DecodeError{Code: ErrInvalidSection, Offset: start - 1, SectionID: id, SectionStart: start, SectionEnd: end}
			}
			if ord < lastOrder {
				return nil, &DecodeError{Code: ErrSectionOrder, Offset: start - 1, SectionID: id, SectionStart: start, SectionEnd: end}
			}
			if seen[id] {
				return nil, &DecodeError{Code: ErrDuplicateSection, Offset: start - 1, SectionID: id, SectionStart: start, SectionEnd: end}
			}
			seen[id] = true
			lastOrder = ord
		}
		sub := newReader(payload)
		sub.memarg64 = moduleMemargOffset64(m)
		switch id {
		case secCustom:
			err = decodeASTCustomSectionForTest(m, sub, &seenName)
		case secStringRefs:
			err = decodeDirectStringRefsSection(m, sub)
		case secTable:
			err = decodeASTTableSectionForTest(m, sub)
		case secGlobal:
			err = decodeASTGlobalSectionForTest(m, sub)
		case secElement:
			err = decodeASTElementSectionForTest(m, sub)
		case secCode:
			err = decodeASTCodeSectionForTest(m, sub)
		case secData:
			err = decodeASTDataSectionForTest(m, sub)
		default:
			err = decodeSection(m, sub, id)
		}
		if err != nil {
			if de, ok := err.(*DecodeError); ok {
				de.SectionID = id
				de.SectionStart = start
				de.SectionEnd = end
				if de.Offset == 0 {
					de.Offset = start
				}
				return nil, de
			}
			return nil, err
		}
		if sub.has() {
			return nil, &DecodeError{Code: ErrSectionSizeMismatch, Offset: start + sub.off(), SectionID: id, SectionStart: start, SectionEnd: end}
		}
	}
	if len(m.FuncTypes) != len(m.Code) {
		return nil, &DecodeError{Code: ErrInvalidModule, Offset: len(data)}
	}
	return m, nil
}

func decodeASTCustomSectionForTest(m *Module, r *reader, seenName *bool) error {
	name, err := r.name()
	if err != nil {
		return err
	}
	payload, err := r.bytes(r.left())
	if err != nil {
		return err
	}
	if name == "name" {
		if *seenName {
			return &DecodeError{Code: ErrInvalidSection, Offset: r.off()}
		}
		ns, err := decodeNameSec(payload)
		if err != nil {
			return err
		}
		m.NameSec = ns
		m.RawNameSecPayload = append([]byte(nil), payload...)
		*seenName = true
	}
	m.Customs = append(m.Customs, CustomSec{Name: name, Data: append([]byte(nil), payload...)})
	return nil
}

func decodeASTTableSectionForTest(m *Module, r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	m.Tables = make([]Table, 0, minIntForTest(int(n), r.left()))
	for i := uint32(0); i < n; i++ {
		if b, ok := r.peek(); ok && b == 0x40 {
			_, _ = r.byte()
			if b2, ok := r.peek(); !ok || b2 != 0x00 {
				return &DecodeError{Code: ErrInvalidType, Offset: r.off()}
			}
			_, _ = r.byte()
			tt, err := decodeTableType(r)
			if err != nil {
				return err
			}
			init, err := decodeExpr(r, 0)
			if err != nil {
				return err
			}
			m.Tables = append(m.Tables, Table{Type: tt, Init: &init})
			continue
		}
		tt, err := decodeTableType(r)
		if err != nil {
			return err
		}
		m.Tables = append(m.Tables, Table{Type: tt})
	}
	return nil
}

func decodeASTGlobalSectionForTest(m *Module, r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	m.Globals = make([]Global, 0, minIntForTest(int(n), r.left()))
	for i := uint32(0); i < n; i++ {
		gt, err := decodeGlobalType(r)
		if err != nil {
			return err
		}
		init, err := decodeExpr(r, 0)
		if err != nil {
			return err
		}
		m.Globals = append(m.Globals, Global{Type: gt, Init: init})
	}
	return nil
}

func decodeASTCodeSectionForTest(m *Module, r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	m.Code = make([]Func, 0, minIntForTest(int(n), r.left()))
	for i := uint32(0); i < n; i++ {
		size, err := r.u32()
		if err != nil {
			return err
		}
		body, err := r.bytes(int(size))
		if err != nil {
			return err
		}
		sub := newReader(body)
		sub.memarg64 = r.memarg64
		locals, err := decodeLocals(sub)
		if err != nil {
			return err
		}
		expr, err := decodeExpr(sub, 0)
		if err != nil {
			return err
		}
		if sub.has() {
			return &DecodeError{Code: ErrSectionSizeMismatch, Offset: sub.off()}
		}
		m.Code = append(m.Code, Func{Locals: locals, Body: expr})
	}
	return nil
}

func decodeASTDataSectionForTest(m *Module, r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	m.Data = make([]Data, 0, minIntForTest(int(n), r.left()))
	for i := uint32(0); i < n; i++ {
		d, err := decodeASTDataForTest(r)
		if err != nil {
			return err
		}
		m.Data = append(m.Data, d)
	}
	return nil
}

func decodeASTDataForTest(r *reader) (Data, error) {
	flags, err := r.u32()
	if err != nil {
		return Data{}, err
	}
	d := Data{}
	switch flags {
	case 0:
		e, err := decodeExpr(r, 0)
		if err != nil {
			return d, err
		}
		d.Mode = DataMode{Kind: DataActive, Offset: e}
	case 1:
		d.Mode = DataMode{Kind: DataPassive}
	case 2:
		mi, err := r.u32()
		if err != nil {
			return d, err
		}
		e, err := decodeExpr(r, 0)
		if err != nil {
			return d, err
		}
		d.Mode = DataMode{Kind: DataActive, Mem: MemIdx(mi), Offset: e}
	default:
		return d, &DecodeError{Code: ErrInvalidSection, Offset: r.off()}
	}
	n, err := r.u32()
	if err != nil {
		return d, err
	}
	d.Init, err = r.bytes(int(n))
	return d, err
}

func decodeASTElementSectionForTest(m *Module, r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	m.Elements = make([]Elem, 0, minIntForTest(int(n), r.left()))
	for i := uint32(0); i < n; i++ {
		e, err := decodeASTElemForTest(r)
		if err != nil {
			return err
		}
		m.Elements = append(m.Elements, e)
	}
	return nil
}

func decodeASTElemForTest(r *reader) (Elem, error) {
	flags, err := r.u32()
	if err != nil {
		return Elem{}, err
	}
	var e Elem
	switch flags {
	case 0:
		off, err := decodeExpr(r, 0)
		if err != nil {
			return e, err
		}
		funcs, err := readFuncIdxVecForTest(r)
		if err != nil {
			return e, err
		}
		e.Mode = ElemMode{Kind: ElemActive, Offset: off}
		e.Kind = ElemKind{Kind: ElemFuncs, Funcs: funcs}
	case 1:
		if err := readElemKind(r); err != nil {
			return e, err
		}
		funcs, err := readFuncIdxVecForTest(r)
		if err != nil {
			return e, err
		}
		e.Mode = ElemMode{Kind: ElemPassive}
		e.Kind = ElemKind{Kind: ElemFuncs, Funcs: funcs}
	case 2:
		t, err := r.u32()
		if err != nil {
			return e, err
		}
		off, err := decodeExpr(r, 0)
		if err != nil {
			return e, err
		}
		if err := readElemKind(r); err != nil {
			return e, err
		}
		funcs, err := readFuncIdxVecForTest(r)
		if err != nil {
			return e, err
		}
		e.Mode = ElemMode{Kind: ElemActive, Table: TableIdx(t), Offset: off}
		e.Kind = ElemKind{Kind: ElemFuncs, Funcs: funcs}
	case 3:
		if err := readElemKind(r); err != nil {
			return e, err
		}
		funcs, err := readFuncIdxVecForTest(r)
		if err != nil {
			return e, err
		}
		e.Mode = ElemMode{Kind: ElemDeclarative}
		e.Kind = ElemKind{Kind: ElemFuncs, Funcs: funcs}
	case 4:
		off, err := decodeExpr(r, 0)
		if err != nil {
			return e, err
		}
		exprs, err := readASTExprVecForTest(r)
		if err != nil {
			return e, err
		}
		e.Mode = ElemMode{Kind: ElemActive, Offset: off}
		e.Kind = ElemKind{Kind: ElemFuncExprs, Exprs: exprs}
	case 5:
		rt, err := decodeRefType(r)
		if err != nil {
			return e, err
		}
		exprs, err := readASTExprVecForTest(r)
		if err != nil {
			return e, err
		}
		e.Mode = ElemMode{Kind: ElemPassive}
		e.Kind = ElemKind{Kind: ElemTypedExprs, Ref: rt, Exprs: exprs}
	case 6:
		t, err := r.u32()
		if err != nil {
			return e, err
		}
		off, err := decodeExpr(r, 0)
		if err != nil {
			return e, err
		}
		rt, err := decodeRefType(r)
		if err != nil {
			return e, err
		}
		exprs, err := readASTExprVecForTest(r)
		if err != nil {
			return e, err
		}
		e.Mode = ElemMode{Kind: ElemActive, Table: TableIdx(t), Offset: off}
		e.Kind = ElemKind{Kind: ElemTypedExprs, Ref: rt, Exprs: exprs}
	case 7:
		rt, err := decodeRefType(r)
		if err != nil {
			return e, err
		}
		exprs, err := readASTExprVecForTest(r)
		if err != nil {
			return e, err
		}
		e.Mode = ElemMode{Kind: ElemDeclarative}
		e.Kind = ElemKind{Kind: ElemTypedExprs, Ref: rt, Exprs: exprs}
	default:
		return e, &DecodeError{Code: ErrInvalidSection, Offset: r.off()}
	}
	return e, nil
}

func readFuncIdxVecForTest(r *reader) ([]FuncIdx, error) {
	n, err := r.u32()
	if err != nil {
		return nil, err
	}
	out := make([]FuncIdx, 0, minIntForTest(int(n), r.left()))
	for i := uint32(0); i < n; i++ {
		x, err := r.u32()
		if err != nil {
			return nil, err
		}
		out = append(out, FuncIdx(x))
	}
	return out, nil
}

func readASTExprVecForTest(r *reader) ([]Expr, error) {
	n, err := r.u32()
	if err != nil {
		return nil, err
	}
	out := make([]Expr, 0, minIntForTest(int(n), r.left()))
	for i := uint32(0); i < n; i++ {
		e, err := decodeExpr(r, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func minIntForTest(a, b int) int {
	if a < b {
		return a
	}
	return b
}
