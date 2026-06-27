package wasm3

const (
	secCustom     = 0
	secType       = 1
	secImport     = 2
	secFunction   = 3
	secTable      = 4
	secMemory     = 5
	secGlobal     = 6
	secExport     = 7
	secStart      = 8
	secElement    = 9
	secCode       = 10
	secData       = 11
	secDataCount  = 12
	secTag        = 13
	secStringRefs = 14
)

// Section order includes proposal sections by decode position, not numeric id:
// tag/stringrefs are decoded after memory and before globals, while data_count
// is decoded before code.
var sectionOrder = map[byte]int{
	secType: 1, secImport: 2, secFunction: 3, secTable: 4, secMemory: 5,
	secTag: 6, secStringRefs: 7, secGlobal: 8, secExport: 9, secStart: 10,
	secElement: 11, secDataCount: 12, secCode: 13, secData: 14,
}

// DecodeModule decodes a WebAssembly binary into the structured wasm3 module
// representation. Standard sections are accepted only in canonical order;
// custom/name sections may appear between standard sections.
func DecodeModule(data []byte) (*Module, error) {
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
	var stringRefs [][]byte
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
		if err := decodeSection(m, sub, id, &stringRefs); err != nil {
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

func decodeSection(m *Module, r *reader, id byte, stringRefs *[][]byte) error {
	switch id {
	case secCustom:
		name, err := r.name()
		if err != nil {
			return err
		}
		payload, err := r.bytes(r.left())
		if err != nil {
			return err
		}
		if name == "name" {
			ns, err := decodeNameSec(payload)
			if err != nil {
				return err
			}
			m.NameSec = ns
			m.RawNameSecPayload = append([]byte(nil), payload...)
		}
		m.Customs = append(m.Customs, CustomSec{Name: name, Data: append([]byte(nil), payload...)})
	case secType:
		v, err := readVec(r, decodeRecType)
		if err != nil {
			return err
		}
		m.Types = v
	case secImport:
		v, err := readVec(r, decodeImport)
		if err != nil {
			return err
		}
		m.Imports = v
	case secFunction:
		v, err := readVec(r, func(r *reader) (TypeIdx, error) { return decodeTypeIdx(r) })
		if err != nil {
			return err
		}
		m.FuncTypes = v
	case secTable:
		v, err := readVec(r, decodeTable)
		if err != nil {
			return err
		}
		m.Tables = v
	case secMemory:
		v, err := readVec(r, decodeMemType)
		if err != nil {
			return err
		}
		m.Memories = v
	case secTag:
		v, err := readVec(r, decodeTagType)
		if err != nil {
			return err
		}
		m.Tags = v
	case secStringRefs:
		v, err := readVec(r, func(r *reader) ([]byte, error) {
			n, err := r.u32()
			if err != nil {
				return nil, err
			}
			return r.bytes(int(n))
		})
		if err != nil {
			return err
		}
		m.StringRefs = v
		*stringRefs = v
	case secGlobal:
		v, err := readVec(r, decodeGlobal)
		if err != nil {
			return err
		}
		m.Globals = v
	case secExport:
		v, err := readVec(r, decodeExport)
		if err != nil {
			return err
		}
		m.Exports = v
	case secStart:
		i, err := r.u32()
		if err != nil {
			return err
		}
		m.Start = ptr(FuncIdx(i))
	case secElement:
		v, err := readVec(r, decodeElem)
		if err != nil {
			return err
		}
		m.Elements = v
	case secDataCount:
		c, err := r.u32()
		if err != nil {
			return err
		}
		m.DataCount = &c
	case secCode:
		v, err := readVec(r, decodeFunc)
		if err != nil {
			return err
		}
		m.Code = v
	case secData:
		v, err := readVec(r, decodeData)
		if err != nil {
			return err
		}
		m.Data = v
	default:
		return &DecodeError{Code: ErrInvalidSection, Offset: r.off()}
	}
	return nil
}

func decodeNumType(r *reader) (NumType, error) {
	b, err := r.byte()
	if err != nil {
		return 0, err
	}
	switch NumType(b) {
	case NumI32, NumI64, NumF32, NumF64:
		return NumType(b), nil
	}
	return 0, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
}
func decodeAbsHeapType(r *reader) (AbsHeapType, error) {
	b, err := r.byte()
	if err != nil {
		return 0, err
	}
	if b >= 0x69 && b <= 0x74 || b == 0x64 {
		return AbsHeapType(b), nil
	}
	return 0, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
}
func decodeTypeIdx(r *reader) (TypeIdx, error) { x, err := r.u32(); return TypeIdx{Index: x}, err }
func decodeS33TypeIdx(r *reader) (TypeIdx, error) {
	x, err := r.s33()
	if err != nil {
		return TypeIdx{}, err
	}
	if x < 0 {
		return TypeIdx{}, &DecodeError{Code: ErrInvalidType, Offset: r.off()}
	}
	return TypeIdx{Index: uint32(x)}, nil
}
func decodeHeapType(r *reader) (HeapType, error) {
	if b, ok := r.peek(); ok && (b == 0x64 || b >= 0x69 && b <= 0x74) {
		a, err := decodeAbsHeapType(r)
		return AbsHeap(a), err
	}
	idx, err := decodeS33TypeIdx(r)
	if err != nil {
		return HeapType{}, err
	}
	return IndexedHeap(idx), nil
}

func decodeRefHeapType(r *reader) (bool, HeapType, error) {
	if b, ok := r.peek(); ok && b == 0x62 {
		_, _ = r.byte()
		idx, err := decodeS33TypeIdx(r)
		if err != nil {
			return false, HeapType{}, err
		}
		return true, IndexedHeap(idx), nil
	}
	ht, err := decodeHeapType(r)
	return false, ht, err
}
func decodeRefType(r *reader) (RefType, error) {
	b, err := r.byte()
	if err != nil {
		return RefType{}, err
	}
	switch b {
	case 0x63:
		exact, ht, err := decodeRefHeapType(r)
		if err != nil {
			return RefType{}, err
		}
		if !exact && ht.Kind == HeapAbs {
			return AbsRef(ht.Abs), nil
		}
		return Ref(true, ht, exact), nil
	case 0x64:
		exact, ht, err := decodeRefHeapType(r)
		if err != nil {
			return RefType{}, err
		}
		return Ref(false, ht, exact), nil
	default:
		if b == 0x64 || b >= 0x69 && b <= 0x74 {
			return AbsRef(AbsHeapType(b)), nil
		}
		return RefType{}, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
	}
}
func decodeValType(r *reader) (ValType, error) {
	b, ok := r.peek()
	if !ok {
		return ValType{}, &DecodeError{Code: ErrIndexOutOfBounds, Offset: r.off()}
	}
	switch b {
	case 0x7f, 0x7e, 0x7d, 0x7c:
		n, err := decodeNumType(r)
		return ValType{Kind: ValNum, Num: n}, err
	case 0x7b:
		_, _ = r.byte()
		return V128, nil
	case 0x63, 0x64, 0x6f, 0x70, 0x6e, 0x6d, 0x6c, 0x6b, 0x6a, 0x69, 0x71, 0x72, 0x73, 0x74:
		start := r.pos
		rt, err := decodeRefType(r)
		if err != nil && b == 0x64 {
			// stringref uses the same byte as the non-null ref prefix. Treat a bare
			// 0x64 that cannot complete a ref type as stringref.
			r.pos = start + 1
			return StringRef, nil
		}
		return RefVal(rt), err
	default:
		return ValType{}, &DecodeError{Code: ErrInvalidType, Offset: r.off()}
	}
}

func decodeResultType(r *reader) ([]ValType, error) { return readVec(r, decodeValType) }
func decodeMut(r *reader) (Mut, error) {
	b, err := r.byte()
	if err != nil {
		return 0, err
	}
	if b == 0 || b == 1 {
		return Mut(b), nil
	}
	return 0, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
}
func decodeStorageType(r *reader) (StorageType, error) {
	if b, ok := r.peek(); ok && (b == 0x77 || b == 0x78) {
		_, _ = r.byte()
		return StorageType{Packed: true, Pack: PackType(b)}, nil
	}
	vt, err := decodeValType(r)
	return StorageType{Val: vt}, err
}
func decodeFieldType(r *reader) (FieldType, error) {
	st, err := decodeStorageType(r)
	if err != nil {
		return FieldType{}, err
	}
	m, err := decodeMut(r)
	if err != nil {
		return FieldType{}, err
	}
	return FieldType{Storage: st, Mut: m}, nil
}

func decodeCompType(r *reader) (CompType, error) {
	b, err := r.byte()
	if err != nil {
		return CompType{}, err
	}
	switch b {
	case 0x5e:
		ft, err := decodeFieldType(r)
		return CompType{Kind: CompArray, Array: ft}, err
	case 0x5f:
		f, err := readVec(r, decodeFieldType)
		return CompType{Kind: CompStruct, Fields: f}, err
	case 0x60:
		p, err := decodeResultType(r)
		if err != nil {
			return CompType{}, err
		}
		res, err := decodeResultType(r)
		return CompType{Kind: CompFunc, Params: p, Results: res}, err
	default:
		return CompType{}, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
	}
}
func decodeTypeMetadata(r *reader) (TypeMetadata, error) {
	var tm TypeMetadata
	for {
		b, ok := r.peek()
		if !ok || (b != 0x4c && b != 0x4d) {
			return tm, nil
		}
		_, _ = r.byte()
		idx, err := decodeTypeIdx(r)
		if err != nil {
			return tm, err
		}
		if b == 0x4c {
			if tm.Describes != nil || tm.Descriptor != nil {
				return tm, &DecodeError{Code: ErrInvalidType, Offset: r.off()}
			}
			tm.Describes = &idx
		} else {
			if tm.Descriptor != nil {
				return tm, &DecodeError{Code: ErrInvalidType, Offset: r.off()}
			}
			tm.Descriptor = &idx
		}
	}
}
func decodeSubType(r *reader) (SubType, error) {
	b, ok := r.peek()
	if !ok {
		return SubType{}, &DecodeError{Code: ErrIndexOutOfBounds, Offset: r.off()}
	}
	st := SubType{Final: true}
	if b == 0x4f || b == 0x50 {
		_, _ = r.byte()
		st.HasPrefix = true
		st.Final = b == 0x4f
		supers, err := readVec(r, decodeTypeIdx)
		if err != nil {
			return st, err
		}
		st.Supers = supers
	}
	meta, err := decodeTypeMetadata(r)
	if err != nil {
		return st, err
	}
	st.Metadata = meta
	ct, err := decodeCompType(r)
	if err != nil {
		return st, err
	}
	st.Comp = ct
	return st, nil
}
func decodeRecType(r *reader) (RecType, error) {
	if b, ok := r.peek(); ok && b == 0x4e {
		_, _ = r.byte()
		sts, err := readVec(r, decodeSubType)
		return RecType{SubTypes: sts}, err
	}
	st, err := decodeSubType(r)
	if err != nil {
		return RecType{}, err
	}
	return RecType{SubTypes: []SubType{st}}, nil
}

func decodeLimits(r *reader) (Limits, error) {
	flag, err := r.byte()
	if err != nil {
		return Limits{}, err
	}
	l := Limits{}
	switch flag {
	case 0x00, 0x01, 0x04, 0x05:
		l.Addr64 = flag >= 0x04
		min, err := r.u64()
		if err != nil {
			return l, err
		}
		l.Min = min
		if flag == 0x01 || flag == 0x05 {
			max, err := r.u64()
			if err != nil {
				return l, err
			}
			l.Max = &max
		}
		return l, nil
	default:
		return l, &DecodeError{Code: ErrInvalidLimits, Offset: r.off() - 1}
	}
}
func decodeMemType(r *reader) (MemType, error) {
	flag, err := r.byte()
	if err != nil {
		return MemType{}, err
	}
	mt := MemType{}
	switch flag {
	case 0, 1, 2, 3, 4, 5, 6, 7:
		mt.Shared = flag == 2 || flag == 3 || flag == 6 || flag == 7
		mt.Limits.Addr64 = flag >= 4
		min, err := r.u64()
		if err != nil {
			return mt, err
		}
		mt.Limits.Min = min
		if flag == 1 || flag == 3 || flag == 5 || flag == 7 {
			max, err := r.u64()
			if err != nil {
				return mt, err
			}
			mt.Limits.Max = &max
		}
		return mt, nil
	default:
		return mt, &DecodeError{Code: ErrInvalidLimits, Offset: r.off() - 1}
	}
}
func decodeTableType(r *reader) (TableType, error) {
	rt, err := decodeRefType(r)
	if err != nil {
		return TableType{}, err
	}
	lim, err := decodeLimits(r)
	return TableType{Ref: rt, Limits: lim}, err
}
func decodeGlobalType(r *reader) (GlobalType, error) {
	vt, err := decodeValType(r)
	if err != nil {
		return GlobalType{}, err
	}
	m, err := decodeMut(r)
	if err != nil {
		return GlobalType{}, err
	}
	return GlobalType{Type: vt, Mutable: m == Var}, nil
}
func decodeTagType(r *reader) (TagType, error) {
	b, err := r.byte()
	if err != nil {
		return TagType{}, err
	}
	if b != 0 {
		return TagType{}, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
	}
	idx, err := decodeTypeIdx(r)
	return TagType{Type: idx}, err
}
func decodeExternType(r *reader) (ExternType, error) {
	k, err := r.byte()
	if err != nil {
		return ExternType{}, err
	}
	et := ExternType{Kind: ExternKind(k)}
	switch ExternKind(k) {
	case ExternFunc:
		et.Type, err = decodeTypeIdx(r)
	case ExternTable:
		et.Table, err = decodeTableType(r)
	case ExternMem:
		et.Mem, err = decodeMemType(r)
	case ExternGlobal:
		et.Global, err = decodeGlobalType(r)
	case ExternTag:
		et.Tag, err = decodeTagType(r)
	default:
		return et, &DecodeError{Code: ErrInvalidImport, Offset: r.off() - 1}
	}
	return et, err
}
func decodeImport(r *reader) (Import, error) {
	mod, err := r.name()
	if err != nil {
		return Import{}, err
	}
	nm, err := r.name()
	if err != nil {
		return Import{}, err
	}
	et, err := decodeExternType(r)
	return Import{Module: mod, Name: nm, Type: et}, err
}
func decodeTable(r *reader) (Table, error) {
	if b, ok := r.peek(); ok && b == 0x40 {
		_, _ = r.byte()
		z, err := r.byte()
		if err != nil {
			return Table{}, err
		}
		if z != 0 {
			return Table{}, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
		}
		tt, err := decodeTableType(r)
		if err != nil {
			return Table{}, err
		}
		e, err := decodeExpr(r, 0)
		if err != nil {
			return Table{}, err
		}
		return Table{Type: tt, Init: &e}, nil
	}
	tt, err := decodeTableType(r)
	return Table{Type: tt}, err
}
func decodeGlobal(r *reader) (Global, error) {
	gt, err := decodeGlobalType(r)
	if err != nil {
		return Global{}, err
	}
	e, err := decodeExpr(r, 0)
	return Global{Type: gt, Init: e}, err
}
func decodeExternIdx(r *reader) (ExternIdx, error) {
	k, err := r.byte()
	if err != nil {
		return ExternIdx{}, err
	}
	idx, err := r.u32()
	if err != nil {
		return ExternIdx{}, err
	}
	if k > 4 {
		return ExternIdx{}, &DecodeError{Code: ErrInvalidExport, Offset: r.off()}
	}
	return ExternIdx{Kind: ExternKind(k), Index: idx}, nil
}
func decodeExport(r *reader) (Export, error) {
	nm, err := r.name()
	if err != nil {
		return Export{}, err
	}
	idx, err := decodeExternIdx(r)
	return Export{Name: nm, Index: idx}, err
}
func decodeLocals(r *reader) (Locals, error) {
	runs, err := readVec(r, func(r *reader) (LocalRun, error) {
		c, err := r.u32()
		if err != nil {
			return LocalRun{}, err
		}
		vt, err := decodeValType(r)
		return LocalRun{Count: c, Type: vt}, err
	})
	return Locals{Runs: runs}, err
}
func decodeFunc(r *reader) (Func, error) {
	size, err := r.u32()
	if err != nil {
		return Func{}, err
	}
	body, err := r.bytes(int(size))
	if err != nil {
		return Func{}, err
	}
	sub := newReader(body)
	locals, err := decodeLocals(sub)
	if err != nil {
		return Func{}, err
	}
	exprStart := sub.off()
	expr, err := decodeExpr(sub, 0)
	if err != nil {
		return Func{}, err
	}
	exprBytes := body[exprStart:sub.off()]
	if sub.has() {
		return Func{}, &DecodeError{Code: ErrSectionSizeMismatch, Offset: sub.off()}
	}
	return Func{Locals: locals, Body: expr, BodyBytes: exprBytes}, nil
}
func decodeData(r *reader) (Data, error) {
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
