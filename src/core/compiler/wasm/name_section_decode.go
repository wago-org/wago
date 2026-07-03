package wasm

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
