package wago

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

func marshalCompiled(c *Compiled) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("compiled module is nil")
	}
	w := compiledWriter{buf: make([]byte, 0, len(c.Code)+256)}
	w.buf = append(w.buf, wagoMagic...)
	w.u8(wagoVersion)
	w.bytes(c.Code)
	w.intSlice(c.Entry)
	w.uvar(uint64(c.NumImports))
	w.stringSlice(c.Imports)
	if err := w.funcSigs(c.Funcs); err != nil {
		return nil, err
	}
	w.stringIntMap(c.Exports)
	w.nameSec(c.Names)
	if err := w.globalImports(c.GlobalImports); err != nil {
		return nil, err
	}
	if err := w.globals(c.Globals); err != nil {
		return nil, err
	}
	w.stringIntMap(c.GlobalExports)
	w.bool(c.HasTable)
	w.uvar(uint64(c.TableSize))
	w.u32Slice(c.FuncTypeID)
	w.elems(c.Elems)
	w.data(c.Data)
	w.str(c.memoryImport)
	w.gcTypeDescs(c.GCTypeDescs)
	return w.buf, nil
}

type compiledWriter struct {
	buf []byte
	tmp [binary.MaxVarintLen64]byte
}

func (w *compiledWriter) u8(v byte) { w.buf = append(w.buf, v) }
func (w *compiledWriter) bool(v bool) {
	if v {
		w.u8(1)
	} else {
		w.u8(0)
	}
}
func (w *compiledWriter) uvar(v uint64) {
	n := binary.PutUvarint(w.tmp[:], v)
	w.buf = append(w.buf, w.tmp[:n]...)
}
func (w *compiledWriter) ivar(v int) {
	n := binary.PutVarint(w.tmp[:], int64(v))
	w.buf = append(w.buf, w.tmp[:n]...)
}
func (w *compiledWriter) u32(v uint32) {
	w.buf = binary.LittleEndian.AppendUint32(w.buf, v)
}
func (w *compiledWriter) u64(v uint64) {
	w.buf = binary.LittleEndian.AppendUint64(w.buf, v)
}
func (w *compiledWriter) bytes(b []byte) {
	w.uvar(uint64(len(b)))
	w.buf = append(w.buf, b...)
}
func (w *compiledWriter) str(s string) {
	w.uvar(uint64(len(s)))
	w.buf = append(w.buf, s...)
}
func (w *compiledWriter) stringSlice(v []string) {
	w.uvar(uint64(len(v)))
	for _, s := range v {
		w.str(s)
	}
}
func (w *compiledWriter) intSlice(v []int) {
	w.uvar(uint64(len(v)))
	for _, x := range v {
		w.ivar(x)
	}
}
func (w *compiledWriter) u32Slice(v []uint32) {
	w.uvar(uint64(len(v)))
	for _, x := range v {
		w.u32(x)
	}
}
func (w *compiledWriter) stringIntMap(m map[string]int) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	w.uvar(uint64(len(keys)))
	for _, k := range keys {
		w.str(k)
		w.ivar(m[k])
	}
}
func (w *compiledWriter) nameMap(m wasm.NameMap) {
	w.uvar(uint64(len(m)))
	for _, a := range m {
		w.u32(a.Index)
		w.str(a.Name)
	}
}
func (w *compiledWriter) indirectNameMap(m wasm.IndirectNameMap) {
	w.uvar(uint64(len(m)))
	for _, a := range m {
		w.u32(a.Index)
		w.nameMap(a.Names)
	}
}
func (w *compiledWriter) nameSec(n *wasm.NameSec) {
	w.bool(n != nil)
	if n == nil {
		return
	}
	w.bool(n.ModuleName != nil)
	if n.ModuleName != nil {
		w.str(*n.ModuleName)
	}
	w.nameMap(n.FunctionNames)
	w.indirectNameMap(n.LocalNames)
	w.indirectNameMap(n.LabelNames)
	w.nameMap(n.TypeNames)
	w.nameMap(n.TableNames)
	w.nameMap(n.MemoryNames)
	w.nameMap(n.GlobalNames)
	w.nameMap(n.ElementNames)
	w.nameMap(n.DataNames)
	w.indirectNameMap(n.FieldNames)
	w.nameMap(n.TagNames)
}
func (w *compiledWriter) valType(t ValType) error {
	w.u8(t.code())
	return nil
}
func (w *compiledWriter) funcSigs(v []FuncSig) error {
	w.uvar(uint64(len(v)))
	for _, sig := range v {
		w.uvar(uint64(len(sig.Params)))
		for _, t := range sig.Params {
			if err := w.valType(t); err != nil {
				return err
			}
		}
		w.uvar(uint64(len(sig.Results)))
		for _, t := range sig.Results {
			if err := w.valType(t); err != nil {
				return err
			}
		}
	}
	return nil
}
func (w *compiledWriter) offset(o OffsetInit) {
	w.u32(o.Base)
	w.bool(o.HasGlobal)
	w.ivar(o.Global)
}
func (w *compiledWriter) elems(v []ElemInit) {
	w.uvar(uint64(len(v)))
	for _, e := range v {
		w.offset(e.Offset)
		w.uvar(uint64(len(e.Funcs)))
		for _, f := range e.Funcs {
			w.u32(f)
		}
	}
}
func (w *compiledWriter) data(v []DataInit) {
	w.uvar(uint64(len(v)))
	for _, d := range v {
		w.offset(d.Offset)
		w.bytes(d.Bytes)
	}
}
func (w *compiledWriter) globals(v []GlobalDef) error {
	w.uvar(uint64(len(v)))
	for _, g := range v {
		if err := w.valType(g.Type); err != nil {
			return err
		}
		w.bool(g.Mutable)
		w.u64(g.Bits)
		w.bool(g.HasInitGlobal)
		w.ivar(g.InitGlobal)
	}
	return nil
}
func (w *compiledWriter) globalImports(v []GlobalImportDef) error {
	w.uvar(uint64(len(v)))
	for _, g := range v {
		w.str(g.Module)
		w.str(g.Name)
		if err := w.valType(g.Type); err != nil {
			return err
		}
		w.bool(g.Mutable)
	}
	return nil
}
func (w *compiledWriter) gcTypeDescs(v []gc.TypeDesc) {
	w.uvar(uint64(len(v)))
	for _, d := range v {
		w.u32(uint32(d.ID))
		w.u8(byte(d.Kind))
		w.bool(d.Fields != nil)
		w.uvar(uint64(len(d.Fields)))
		for _, f := range d.Fields {
			w.u8(byte(f.Kind))
			w.u32(f.Offset)
		}
		w.u8(byte(d.Elem))
		w.u32(d.Size)
		w.u32(d.ElemSize)
		w.u32(d.Align)
		w.bool(d.HasRefs)
		w.bool(d.Final)
		w.u32(uint32(d.Super))
		w.bool(d.HasSuper)
	}
}

func unmarshalCompiled(c *Compiled, data []byte) error {
	r := compiledReader{data: data}
	var err error
	c.Code, err = r.bytes()
	if err != nil {
		return err
	}
	c.Entry, err = r.intSlice()
	if err != nil {
		return err
	}
	n, err := r.uvar()
	if err != nil {
		return err
	}
	if n > uint64(maxInt()) {
		return fmt.Errorf("NumImports overflows int")
	}
	c.NumImports = int(n)
	c.Imports, err = r.stringSlice()
	if err != nil {
		return err
	}
	c.Funcs, err = r.funcSigs()
	if err != nil {
		return err
	}
	c.Exports, err = r.stringIntMap()
	if err != nil {
		return err
	}
	c.Names, err = r.nameSec()
	if err != nil {
		return err
	}
	c.GlobalImports, err = r.globalImports()
	if err != nil {
		return err
	}
	c.Globals, err = r.globals()
	if err != nil {
		return err
	}
	c.GlobalExports, err = r.stringIntMap()
	if err != nil {
		return err
	}
	c.HasTable, err = r.bool()
	if err != nil {
		return err
	}
	n, err = r.uvar()
	if err != nil {
		return err
	}
	if n > uint64(maxInt()) {
		return fmt.Errorf("TableSize overflows int")
	}
	c.TableSize = int(n)
	c.FuncTypeID, err = r.u32Slice()
	if err != nil {
		return err
	}
	c.Elems, err = r.elems()
	if err != nil {
		return err
	}
	c.Data, err = r.dataInits()
	if err != nil {
		return err
	}
	c.memoryImport, err = r.str()
	if err != nil {
		return err
	}
	c.GCTypeDescs, err = r.gcTypeDescs()
	if err != nil {
		return err
	}
	if len(r.data) != 0 {
		return fmt.Errorf("trailing %d byte(s)", len(r.data))
	}
	return nil
}

type compiledReader struct{ data []byte }

const (
	minStringBytes       = 1
	minVarintBytes       = 1
	minU32Bytes          = 4
	minStringIntMapBytes = minStringBytes + minVarintBytes
	minNameAssocBytes    = minU32Bytes + minStringBytes
	minFuncSigBytes      = minVarintBytes + minVarintBytes
	minOffsetInitBytes   = minU32Bytes + 1 + minVarintBytes
	minElemInitBytes     = minOffsetInitBytes + minVarintBytes
	minDataInitBytes     = minOffsetInitBytes + minStringBytes
	minGlobalBytes       = 1 + 1 + 8 + 1 + minVarintBytes
	minGlobalImportBytes = minStringBytes + minStringBytes + 1 + 1
	minGCDescTailBytes   = 20
	minGCDescBytes       = minU32Bytes + 1 + 1 + minVarintBytes + minGCDescTailBytes
	minGCFieldBytes      = 1 + minU32Bytes
)

func (r *compiledReader) take(n int) ([]byte, error) {
	if n < 0 || n > len(r.data) {
		return nil, fmt.Errorf("unexpected EOF")
	}
	b := r.data[:n]
	r.data = r.data[n:]
	return b, nil
}
func (r *compiledReader) u8() (byte, error) {
	b, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}
func (r *compiledReader) bool() (bool, error) {
	b, err := r.u8()
	if err != nil {
		return false, err
	}
	switch b {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("invalid bool %d", b)
	}
}
func (r *compiledReader) uvar() (uint64, error) {
	v, n := binary.Uvarint(r.data)
	if n <= 0 {
		return 0, fmt.Errorf("invalid uvarint")
	}
	r.data = r.data[n:]
	return v, nil
}
func (r *compiledReader) ivar() (int, error) {
	v, n := binary.Varint(r.data)
	if n <= 0 {
		return 0, fmt.Errorf("invalid varint")
	}
	r.data = r.data[n:]
	if int64(int(v)) != v {
		return 0, fmt.Errorf("int overflows")
	}
	return int(v), nil
}
func (r *compiledReader) u32() (uint32, error) {
	b, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}
func (r *compiledReader) u64() (uint64, error) {
	b, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b), nil
}
func (r *compiledReader) countMax(label string, max int) (int, error) {
	n, err := r.uvar()
	if err != nil {
		return 0, err
	}
	if n > uint64(maxInt()) {
		return 0, fmt.Errorf("%s count overflows int", label)
	}
	if max < 0 || n > uint64(max) {
		return 0, fmt.Errorf("%s count %d exceeds remaining encoding capacity %d", label, n, max)
	}
	return int(n), nil
}
func (r *compiledReader) countElements(label string, minElemBytes int) (int, error) {
	if minElemBytes <= 0 {
		return 0, fmt.Errorf("%s count has invalid element size %d", label, minElemBytes)
	}
	return r.countMax(label, len(r.data)/minElemBytes)
}
func (r *compiledReader) countBytes(label string) (int, error) {
	return r.countMax(label, len(r.data))
}
func (r *compiledReader) bytes() ([]byte, error) {
	n, err := r.countBytes("byte slice")
	if err != nil {
		return nil, err
	}
	return r.take(n)
}
func (r *compiledReader) str() (string, error) {
	b, err := r.bytes()
	if err != nil {
		return "", err
	}
	return string(b), nil
}
func (r *compiledReader) stringSlice() ([]string, error) {
	n, err := r.countElements("string slice", minStringBytes)
	if err != nil {
		return nil, err
	}
	out := make([]string, n)
	for i := range out {
		out[i], err = r.str()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (r *compiledReader) intSlice() ([]int, error) {
	n, err := r.countElements("int slice", minVarintBytes)
	if err != nil {
		return nil, err
	}
	out := make([]int, n)
	for i := range out {
		out[i], err = r.ivar()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (r *compiledReader) u32Slice() ([]uint32, error) {
	n, err := r.countElements("u32 slice", minU32Bytes)
	if err != nil {
		return nil, err
	}
	out := make([]uint32, n)
	for i := range out {
		out[i], err = r.u32()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (r *compiledReader) stringIntMap() (map[string]int, error) {
	n, err := r.countElements("string-int map", minStringIntMapBytes)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, n)
	for i := 0; i < n; i++ {
		k, err := r.str()
		if err != nil {
			return nil, err
		}
		v, err := r.ivar()
		if err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}
func (r *compiledReader) nameMap() (wasm.NameMap, error) {
	n, err := r.countElements("name map", minNameAssocBytes)
	if err != nil {
		return nil, err
	}
	out := make(wasm.NameMap, n)
	for i := range out {
		out[i].Index, err = r.u32()
		if err != nil {
			return nil, err
		}
		out[i].Name, err = r.str()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (r *compiledReader) indirectNameMap() (wasm.IndirectNameMap, error) {
	n, err := r.countElements("indirect name map", minNameAssocBytes)
	if err != nil {
		return nil, err
	}
	out := make(wasm.IndirectNameMap, n)
	for i := range out {
		out[i].Index, err = r.u32()
		if err != nil {
			return nil, err
		}
		out[i].Names, err = r.nameMap()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (r *compiledReader) nameSec() (*wasm.NameSec, error) {
	has, err := r.bool()
	if err != nil || !has {
		return nil, err
	}
	n := &wasm.NameSec{}
	hasModule, err := r.bool()
	if err != nil {
		return nil, err
	}
	if hasModule {
		s, err := r.str()
		if err != nil {
			return nil, err
		}
		n.ModuleName = &s
	}
	if n.FunctionNames, err = r.nameMap(); err != nil {
		return nil, err
	}
	if n.LocalNames, err = r.indirectNameMap(); err != nil {
		return nil, err
	}
	if n.LabelNames, err = r.indirectNameMap(); err != nil {
		return nil, err
	}
	if n.TypeNames, err = r.nameMap(); err != nil {
		return nil, err
	}
	if n.TableNames, err = r.nameMap(); err != nil {
		return nil, err
	}
	if n.MemoryNames, err = r.nameMap(); err != nil {
		return nil, err
	}
	if n.GlobalNames, err = r.nameMap(); err != nil {
		return nil, err
	}
	if n.ElementNames, err = r.nameMap(); err != nil {
		return nil, err
	}
	if n.DataNames, err = r.nameMap(); err != nil {
		return nil, err
	}
	if n.FieldNames, err = r.indirectNameMap(); err != nil {
		return nil, err
	}
	if n.TagNames, err = r.nameMap(); err != nil {
		return nil, err
	}
	return n, nil
}
func (r *compiledReader) valType() (ValType, error) {
	code, err := r.u8()
	if err != nil {
		return 0, err
	}
	t, ok := valTypeFromCode(code)
	if !ok {
		return 0, fmt.Errorf("unsupported value type code 0x%02x", code)
	}
	return t, nil
}
func (r *compiledReader) funcSigs() ([]FuncSig, error) {
	n, err := r.countElements("function signatures", minFuncSigBytes)
	if err != nil {
		return nil, err
	}
	out := make([]FuncSig, n)
	for i := range out {
		pn, err := r.countElements("function parameters", minVarintBytes)
		if err != nil {
			return nil, err
		}
		out[i].Params = make([]ValType, pn)
		for j := range out[i].Params {
			out[i].Params[j], err = r.valType()
			if err != nil {
				return nil, err
			}
		}
		rn, err := r.countElements("function results", minVarintBytes)
		if err != nil {
			return nil, err
		}
		out[i].Results = make([]ValType, rn)
		for j := range out[i].Results {
			out[i].Results[j], err = r.valType()
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
func (r *compiledReader) offset() (OffsetInit, error) {
	base, err := r.u32()
	if err != nil {
		return OffsetInit{}, err
	}
	has, err := r.bool()
	if err != nil {
		return OffsetInit{}, err
	}
	glob, err := r.ivar()
	if err != nil {
		return OffsetInit{}, err
	}
	return OffsetInit{Base: base, HasGlobal: has, Global: glob}, nil
}
func (r *compiledReader) elems() ([]ElemInit, error) {
	n, err := r.countElements("element segments", minElemInitBytes)
	if err != nil {
		return nil, err
	}
	out := make([]ElemInit, n)
	for i := range out {
		out[i].Offset, err = r.offset()
		if err != nil {
			return nil, err
		}
		fn, err := r.countElements("element functions", minU32Bytes)
		if err != nil {
			return nil, err
		}
		out[i].Funcs = make([]uint32, fn)
		for j := range out[i].Funcs {
			out[i].Funcs[j], err = r.u32()
			if err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
func (r *compiledReader) dataInits() ([]DataInit, error) {
	n, err := r.countElements("data segments", minDataInitBytes)
	if err != nil {
		return nil, err
	}
	out := make([]DataInit, n)
	for i := range out {
		out[i].Offset, err = r.offset()
		if err != nil {
			return nil, err
		}
		out[i].Bytes, err = r.bytes()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (r *compiledReader) globals() ([]GlobalDef, error) {
	n, err := r.countElements("globals", minGlobalBytes)
	if err != nil {
		return nil, err
	}
	out := make([]GlobalDef, n)
	for i := range out {
		out[i].Type, err = r.valType()
		if err != nil {
			return nil, err
		}
		out[i].Mutable, err = r.bool()
		if err != nil {
			return nil, err
		}
		out[i].Bits, err = r.u64()
		if err != nil {
			return nil, err
		}
		out[i].HasInitGlobal, err = r.bool()
		if err != nil {
			return nil, err
		}
		out[i].InitGlobal, err = r.ivar()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (r *compiledReader) globalImports() ([]GlobalImportDef, error) {
	n, err := r.countElements("global imports", minGlobalImportBytes)
	if err != nil {
		return nil, err
	}
	out := make([]GlobalImportDef, n)
	for i := range out {
		out[i].Module, err = r.str()
		if err != nil {
			return nil, err
		}
		out[i].Name, err = r.str()
		if err != nil {
			return nil, err
		}
		out[i].Type, err = r.valType()
		if err != nil {
			return nil, err
		}
		out[i].Mutable, err = r.bool()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (r *compiledReader) gcTypeDescs() ([]gc.TypeDesc, error) {
	n, err := r.countElements("GC type descriptors", minGCDescBytes)
	if err != nil {
		return nil, err
	}
	out := make([]gc.TypeDesc, n)
	for i := range out {
		id, err := r.u32()
		if err != nil {
			return nil, err
		}
		kind, err := r.u8()
		if err != nil {
			return nil, err
		}
		out[i].ID = gc.TypeID(id)
		out[i].Kind = gc.TypeKind(kind)
		fieldsPresent, err := r.bool()
		if err != nil {
			return nil, err
		}
		fieldCount, err := r.countElements("GC type fields", minGCFieldBytes)
		if err != nil {
			return nil, err
		}
		if fieldsPresent {
			if len(r.data) < minGCDescTailBytes {
				return nil, fmt.Errorf("GC type fields missing descriptor tail")
			}
			maxFields := (len(r.data) - minGCDescTailBytes) / minGCFieldBytes
			if fieldCount > maxFields {
				return nil, fmt.Errorf("GC type fields count %d exceeds remaining encoding capacity %d", fieldCount, maxFields)
			}
			out[i].Fields = make([]gc.FieldDesc, fieldCount)
		} else if fieldCount != 0 {
			return nil, fmt.Errorf("nil GC type field list with count %d", fieldCount)
		}
		for j := range out[i].Fields {
			storage, err := r.u8()
			if err != nil {
				return nil, err
			}
			off, err := r.u32()
			if err != nil {
				return nil, err
			}
			out[i].Fields[j] = gc.FieldDesc{Kind: gc.StorageKind(storage), Offset: off}
		}
		elem, err := r.u8()
		if err != nil {
			return nil, err
		}
		out[i].Elem = gc.StorageKind(elem)
		if out[i].Size, err = r.u32(); err != nil {
			return nil, err
		}
		if out[i].ElemSize, err = r.u32(); err != nil {
			return nil, err
		}
		if out[i].Align, err = r.u32(); err != nil {
			return nil, err
		}
		if out[i].HasRefs, err = r.bool(); err != nil {
			return nil, err
		}
		if out[i].Final, err = r.bool(); err != nil {
			return nil, err
		}
		super, err := r.u32()
		if err != nil {
			return nil, err
		}
		out[i].Super = gc.TypeID(super)
		if out[i].HasSuper, err = r.bool(); err != nil {
			return nil, err
		}
	}
	return out, nil
}
