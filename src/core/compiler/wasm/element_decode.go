package wasm3

func decodeElem(r *reader) (Elem, error) {
	flags, err := r.u32()
	if err != nil {
		return Elem{}, err
	}
	e := Elem{}
	switch flags {
	case 0:
		off, err := decodeExpr(r, 0)
		if err != nil {
			return e, err
		}
		funcs, err := readVec(r, func(r *reader) (FuncIdx, error) { x, err := r.u32(); return FuncIdx(x), err })
		e.Mode = ElemMode{Kind: ElemActive, Offset: off}
		e.Kind = ElemKind{Kind: ElemFuncs, Funcs: funcs}
		return e, err
	case 1:
		if err := readElemKind(r); err != nil {
			return e, err
		}
		funcs, err := readVec(r, func(r *reader) (FuncIdx, error) { x, err := r.u32(); return FuncIdx(x), err })
		e.Mode = ElemMode{Kind: ElemPassive}
		e.Kind = ElemKind{Kind: ElemFuncs, Funcs: funcs}
		return e, err
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
		funcs, err := readVec(r, func(r *reader) (FuncIdx, error) { x, err := r.u32(); return FuncIdx(x), err })
		e.Mode = ElemMode{Kind: ElemActive, Table: TableIdx(t), Offset: off}
		e.Kind = ElemKind{Kind: ElemFuncs, Funcs: funcs}
		return e, err
	case 3:
		if err := readElemKind(r); err != nil {
			return e, err
		}
		funcs, err := readVec(r, func(r *reader) (FuncIdx, error) { x, err := r.u32(); return FuncIdx(x), err })
		e.Mode = ElemMode{Kind: ElemDeclarative}
		e.Kind = ElemKind{Kind: ElemFuncs, Funcs: funcs}
		return e, err
	case 4:
		off, err := decodeExpr(r, 0)
		if err != nil {
			return e, err
		}
		exprs, err := readVec(r, func(r *reader) (Expr, error) { return decodeExpr(r, 0) })
		e.Mode = ElemMode{Kind: ElemActive, Offset: off}
		e.Kind = ElemKind{Kind: ElemFuncExprs, Exprs: exprs}
		return e, err
	case 5:
		rt, err := decodeRefType(r)
		if err != nil {
			return e, err
		}
		exprs, err := readVec(r, func(r *reader) (Expr, error) { return decodeExpr(r, 0) })
		e.Mode = ElemMode{Kind: ElemPassive}
		e.Kind = ElemKind{Kind: ElemTypedExprs, Ref: rt, Exprs: exprs}
		return e, err
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
		exprs, err := readVec(r, func(r *reader) (Expr, error) { return decodeExpr(r, 0) })
		e.Mode = ElemMode{Kind: ElemActive, Table: TableIdx(t), Offset: off}
		e.Kind = ElemKind{Kind: ElemTypedExprs, Ref: rt, Exprs: exprs}
		return e, err
	case 7:
		rt, err := decodeRefType(r)
		if err != nil {
			return e, err
		}
		exprs, err := readVec(r, func(r *reader) (Expr, error) { return decodeExpr(r, 0) })
		e.Mode = ElemMode{Kind: ElemDeclarative}
		e.Kind = ElemKind{Kind: ElemTypedExprs, Ref: rt, Exprs: exprs}
		return e, err
	default:
		return e, &DecodeError{Code: ErrInvalidSection, Offset: r.off()}
	}
}
func readElemKind(r *reader) error {
	b, err := r.byte()
	if err != nil {
		return err
	}
	if b != 0 {
		return &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
	}
	return nil
}

func decodeNameSec(payload []byte) (*NameSec, error) {
	r := newReader(payload)
	ns := &NameSec{}
	var prev byte
	seen := false
	for r.has() {
		id, err := r.byte()
		if err != nil {
			return nil, err
		}
		if seen && id <= prev {
			return nil, &DecodeError{Code: ErrInvalidSection, Offset: r.off() - 1}
		}
		prev = id
		seen = true
		size, err := r.u32()
		if err != nil {
			return nil, err
		}
		subb, err := r.bytes(int(size))
		if err != nil {
			return nil, err
		}
		sub := newReader(subb)
		known := true
		switch id {
		case 0:
			name, err := sub.name()
			if err != nil {
				return nil, err
			}
			ns.ModuleName = &name
		case 1:
			m, err := decodeNameMap(sub)
			if err != nil {
				return nil, err
			}
			ns.FunctionNames = m
		case 2:
			m, err := decodeIndirectNameMap(sub)
			if err != nil {
				return nil, err
			}
			ns.LocalNames = m
		case 3:
			m, err := decodeIndirectNameMap(sub)
			if err != nil {
				return nil, err
			}
			ns.LabelNames = m
		case 4:
			m, err := decodeNameMap(sub)
			if err != nil {
				return nil, err
			}
			ns.TypeNames = m
		case 5:
			m, err := decodeNameMap(sub)
			if err != nil {
				return nil, err
			}
			ns.TableNames = m
		case 6:
			m, err := decodeNameMap(sub)
			if err != nil {
				return nil, err
			}
			ns.MemoryNames = m
		case 7:
			m, err := decodeNameMap(sub)
			if err != nil {
				return nil, err
			}
			ns.GlobalNames = m
		case 8:
			m, err := decodeNameMap(sub)
			if err != nil {
				return nil, err
			}
			ns.ElementNames = m
		case 9:
			m, err := decodeNameMap(sub)
			if err != nil {
				return nil, err
			}
			ns.DataNames = m
		case 10:
			m, err := decodeIndirectNameMap(sub)
			if err != nil {
				return nil, err
			}
			ns.FieldNames = m
		case 11:
			m, err := decodeNameMap(sub)
			if err != nil {
				return nil, err
			}
			ns.TagNames = m
		default:
			known = false
		}
		if known && sub.has() {
			return nil, &DecodeError{Code: ErrSectionSizeMismatch, Offset: r.off() - sub.left()}
		}
	}
	return ns, nil
}
func decodeNameMap(r *reader) (NameMap, error) {
	entries, err := readVec(r, func(r *reader) (NameAssoc, error) {
		i, err := r.u32()
		if err != nil {
			return NameAssoc{}, err
		}
		n, err := r.name()
		return NameAssoc{Index: i, Name: n}, err
	})
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].Index <= entries[i-1].Index {
			return nil, &DecodeError{Code: ErrInvalidSection, Offset: r.off()}
		}
	}
	return entries, nil
}
func decodeIndirectNameMap(r *reader) (IndirectNameMap, error) {
	entries, err := readVec(r, func(r *reader) (IndirectNameAssoc, error) {
		i, err := r.u32()
		if err != nil {
			return IndirectNameAssoc{}, err
		}
		m, err := decodeNameMap(r)
		return IndirectNameAssoc{Index: i, Names: m}, err
	})
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].Index <= entries[i-1].Index {
			return nil, &DecodeError{Code: ErrInvalidSection, Offset: r.off()}
		}
	}
	return entries, nil
}
