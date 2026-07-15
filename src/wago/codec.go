package wago

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

func compiledMetadataUsesSIMD(c *Compiled) bool {
	if c == nil {
		return false
	}
	for _, sig := range c.importFuncSigs {
		if valTypesUseSIMD(sig.Params) || valTypesUseSIMD(sig.Results) {
			return true
		}
	}
	for _, sig := range c.Funcs {
		if valTypesUseSIMD(sig.Params) || valTypesUseSIMD(sig.Results) {
			return true
		}
	}
	for _, g := range c.GlobalImports {
		if g.Type == ValV128 {
			return true
		}
	}
	for _, g := range c.Globals {
		if g.Type == ValV128 {
			return true
		}
	}
	return false
}

func valTypesUseSIMD(ts []ValType) bool {
	for _, t := range ts {
		if t == ValV128 {
			return true
		}
	}
	return false
}

func marshalCompiled(c *Compiled) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("compiled module is nil")
	}
	w := compiledWriter{buf: make([]byte, 0, len(c.Code)+256)}
	w.buf = append(w.buf, wagoMagic...)
	w.u8(wagoVersion)
	w.bytes(c.Code)
	w.intSlice(c.Entry)
	w.intSlice(c.InternalEntry)
	w.uvar(uint64(c.NumImports))
	w.stringSlice(c.Imports)
	if err := w.typeDescriptors(c.Types); err != nil {
		return nil, err
	}
	if err := w.funcSigs(c.importFuncSigs, c.Types); err != nil {
		return nil, err
	}
	if err := w.funcSigs(c.Funcs, c.Types); err != nil {
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
	if err := w.tables(c); err != nil {
		return nil, err
	}
	w.stringIntMap(c.tableExports)
	w.u32Slice(c.FuncTypeID)
	w.bool(c.NeedsFuncRefDescs)
	if err := w.elems(c.Elems); err != nil {
		return nil, err
	}
	if err := w.elems(c.passiveElems); err != nil {
		return nil, err
	}
	w.data(c.Data)
	w.passiveData(c.PassiveData)
	w.str(c.memoryImport)
	w.bool(c.dynamicImports)
	w.u64(uint64(compiledStructuralRequiredFeatures(c)))
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
	code, ok := t.code()
	if !ok {
		return fmt.Errorf("unsupported value type %s in compiled metadata", t)
	}
	w.u8(code)
	return nil
}
func (w *compiledWriter) valueType(t ValueTypeDescriptor) {
	w.u8(byte(t.Kind))
	if t.Kind != ValueTypeReference {
		return
	}
	w.bool(t.Ref.Nullable)
	w.bool(t.Ref.Exact)
	w.bool(t.Ref.Heap.Defined)
	if t.Ref.Heap.Defined {
		w.u32(t.Ref.Heap.TypeIndex)
	} else {
		w.u8(byte(t.Ref.Heap.Abstract))
	}
}

func (w *compiledWriter) valueTypes(v []ValueTypeDescriptor) {
	w.uvar(uint64(len(v)))
	for _, t := range v {
		w.valueType(t)
	}
}

func (w *compiledWriter) fieldType(f FieldTypeDescriptor) {
	w.bool(f.Storage.Packed)
	if f.Storage.Packed {
		w.u8(byte(f.Storage.PackedType))
	} else {
		w.valueType(f.Storage.Value)
	}
	w.bool(f.Mutable)
}

func (w *compiledWriter) typeDescriptors(v []DefinedTypeDescriptor) error {
	if err := validateDefinedTypeDescriptors(v); err != nil {
		return err
	}
	w.uvar(uint64(len(v)))
	for _, d := range v {
		w.u32(d.RecGroup)
		w.bool(d.Final)
		w.uvar(uint64(len(d.Supers)))
		for _, x := range d.Supers {
			w.u32(x)
		}
		w.bool(d.HasDescribes)
		if d.HasDescribes {
			w.u32(d.Describes)
		}
		w.bool(d.HasDescriptor)
		if d.HasDescriptor {
			w.u32(d.Descriptor)
		}
		w.u8(byte(d.Kind))
		switch d.Kind {
		case CompositeTypeFunction:
			w.valueTypes(d.Params)
			w.valueTypes(d.Results)
		case CompositeTypeStruct:
			w.uvar(uint64(len(d.Fields)))
			for _, f := range d.Fields {
				w.fieldType(f)
			}
		case CompositeTypeArray:
			w.fieldType(d.Array)
		}
	}
	return nil
}

func (w *compiledWriter) funcSigs(v []FuncSig, types []DefinedTypeDescriptor) error {
	w.uvar(uint64(len(v)))
	for i, sig := range v {
		params, results, err := exactFuncSignature(sig, types)
		if err != nil {
			return fmt.Errorf("function signature %d: %w", i, err)
		}
		w.bool(sig.HasTypeIndex)
		if sig.HasTypeIndex {
			w.u32(sig.TypeIndex)
			continue
		}
		w.valueTypes(params)
		w.valueTypes(results)
	}
	return nil
}
func (w *compiledWriter) offset(o OffsetInit) {
	w.u32(o.Base)
	w.bool(o.HasGlobal)
	w.ivar(o.Global)
	w.bytes(o.Expr)
}
func (w *compiledWriter) elems(v []ElemInit) error {
	w.uvar(uint64(len(v)))
	for _, e := range v {
		w.u32(e.TableIndex)
		if err := w.valType(normalizedElemRefType(e.RefType)); err != nil {
			return err
		}
		w.u8(byte(e.Mode))
		w.offset(e.Offset)
		w.uvar(uint64(len(e.Values)))
		for _, value := range e.Values {
			if value.Null {
				w.u8(0)
			} else {
				w.u8(1)
				w.u32(value.FuncIndex)
			}
		}
	}
	return nil
}
func (w *compiledWriter) data(v []DataInit) {
	w.uvar(uint64(len(v)))
	for _, d := range v {
		w.offset(d.Offset)
		w.bytes(d.Bytes)
	}
}
func (w *compiledWriter) passiveData(v []PassiveDataInit) {
	w.uvar(uint64(len(v)))
	for _, d := range v {
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
		switch {
		case g.HasInitGlobal:
			w.u8(1)
			w.ivar(g.InitGlobal)
		case g.HasInitFunc:
			w.u8(2)
			w.u32(g.InitFunc)
		case len(g.InitExpr) != 0:
			w.u8(3)
			w.bytes(g.InitExpr)
		default:
			w.u8(0)
			w.u64(g.Bits)
			if g.Type == ValV128 {
				w.buf = append(w.buf, g.V128[:]...)
			}
		}
	}
	return nil
}

func (w *compiledWriter) tables(c *Compiled) error {
	count := c.tableCount()
	w.uvar(uint64(count))
	for i := 0; i < count; i++ {
		if err := w.valType(c.tableElementType(i)); err != nil {
			return err
		}
		if imp, ok := c.tableImportAt(i); ok {
			w.u8(1)
			w.str(imp.Key)
			w.uvar(uint64(imp.Min))
			w.uvar(uint64(imp.Max))
			w.bool(imp.HasMax)
			continue
		}
		def := c.tableDef(i)
		w.u8(0)
		w.uvar(uint64(def.Size))
		w.uvar(uint64(def.Max))
		w.bool(def.HasMax)
		w.bool(def.HasInitFunc)
		if def.HasInitFunc {
			w.u32(def.InitFunc)
		}
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
	c.InternalEntry, err = r.intSlice()
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
	c.Types, err = r.typeDescriptors()
	if err != nil {
		return err
	}
	c.importFuncSigs, err = r.funcSigs(c.Types)
	if err != nil {
		return err
	}
	c.Funcs, err = r.funcSigs(c.Types)
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
	if err := r.tables(c); err != nil {
		return err
	}
	c.tableExports, err = r.stringIntMap()
	if err != nil {
		return err
	}
	c.FuncTypeID, err = r.u32Slice()
	if err != nil {
		return err
	}
	c.NeedsFuncRefDescs, err = r.bool()
	if err != nil {
		return err
	}
	c.Elems, err = r.elems()
	if err != nil {
		return err
	}
	c.passiveElems, err = r.elems()
	if err != nil {
		return err
	}
	c.Data, err = r.dataInits()
	if err != nil {
		return err
	}
	c.PassiveData, err = r.passiveDataInits()
	if err != nil {
		return err
	}
	c.memoryImport, err = r.str()
	if err != nil {
		return err
	}
	c.dynamicImports, err = r.bool()
	if err != nil {
		return err
	}
	required, err := r.u64()
	if err != nil {
		return err
	}
	c.requiredFeatures = CoreFeatures(required)
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
	minFuncSigBytes      = 1
	minDefinedTypeBytes  = minU32Bytes + 1 + minVarintBytes + 1 + 1 + 1
	minFieldTypeBytes    = 3
	minOffsetInitBytes   = minU32Bytes + 1 + minVarintBytes + minStringBytes
	minElemInitBytes     = minU32Bytes + 1 + 1 + minOffsetInitBytes + minVarintBytes
	minDataInitBytes     = minOffsetInitBytes + minStringBytes
	minPassiveDataBytes  = minStringBytes
	minGlobalBytes       = 1 + 1 + 1
	minTableBytes        = 1 + 1 + minVarintBytes + minVarintBytes + 1
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
	return r.bytesLabel("byte slice")
}
func (r *compiledReader) bytesLabel(label string) ([]byte, error) {
	n, err := r.countBytes(label)
	if err != nil {
		return nil, err
	}
	return r.take(n)
}
func (r *compiledReader) str() (string, error) {
	return r.strLabel("string")
}
func (r *compiledReader) strLabel(label string) (string, error) {
	b, err := r.bytesLabel(label)
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
func (r *compiledReader) nameMap(label string) (wasm.NameMap, error) {
	n, err := r.countElements(label, minNameAssocBytes)
	if err != nil {
		return nil, err
	}
	out := make(wasm.NameMap, n)
	for i := range out {
		out[i].Index, err = r.u32()
		if err != nil {
			return nil, err
		}
		out[i].Name, err = r.strLabel(label + " name")
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (r *compiledReader) indirectNameMap(label, nestedLabel string) (wasm.IndirectNameMap, error) {
	n, err := r.countElements(label, minNameAssocBytes)
	if err != nil {
		return nil, err
	}
	out := make(wasm.IndirectNameMap, n)
	for i := range out {
		out[i].Index, err = r.u32()
		if err != nil {
			return nil, err
		}
		out[i].Names, err = r.nameMap(nestedLabel)
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
		s, err := r.strLabel("module name")
		if err != nil {
			return nil, err
		}
		n.ModuleName = &s
	}
	if n.FunctionNames, err = r.nameMap("function name map"); err != nil {
		return nil, err
	}
	if n.LocalNames, err = r.indirectNameMap("local indirect name map", "local name map"); err != nil {
		return nil, err
	}
	if n.LabelNames, err = r.indirectNameMap("label indirect name map", "label name map"); err != nil {
		return nil, err
	}
	if n.TypeNames, err = r.nameMap("type name map"); err != nil {
		return nil, err
	}
	if n.TableNames, err = r.nameMap("table name map"); err != nil {
		return nil, err
	}
	if n.MemoryNames, err = r.nameMap("memory name map"); err != nil {
		return nil, err
	}
	if n.GlobalNames, err = r.nameMap("global name map"); err != nil {
		return nil, err
	}
	if n.ElementNames, err = r.nameMap("element name map"); err != nil {
		return nil, err
	}
	if n.DataNames, err = r.nameMap("data name map"); err != nil {
		return nil, err
	}
	if n.FieldNames, err = r.indirectNameMap("field indirect name map", "field name map"); err != nil {
		return nil, err
	}
	if n.TagNames, err = r.nameMap("tag name map"); err != nil {
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
func (r *compiledReader) valueType() (ValueTypeDescriptor, error) {
	kind, err := r.u8()
	if err != nil {
		return ValueTypeDescriptor{}, err
	}
	t := ValueTypeDescriptor{Kind: ValueTypeKind(kind)}
	if t.Kind > ValueTypeReference {
		return t, fmt.Errorf("invalid structural value type kind %d", kind)
	}
	if t.Kind != ValueTypeReference {
		return t, nil
	}
	if t.Ref.Nullable, err = r.bool(); err != nil {
		return t, err
	}
	if t.Ref.Exact, err = r.bool(); err != nil {
		return t, err
	}
	if t.Ref.Heap.Defined, err = r.bool(); err != nil {
		return t, err
	}
	if t.Ref.Heap.Defined {
		t.Ref.Heap.TypeIndex, err = r.u32()
		return t, err
	}
	abs, err := r.u8()
	if err != nil {
		return t, err
	}
	t.Ref.Heap.Abstract = AbstractHeapType(abs)
	if t.Ref.Heap.Abstract > AbstractHeapNoExn {
		return t, fmt.Errorf("invalid abstract heap type %d", abs)
	}
	return t, nil
}

func (r *compiledReader) valueTypes(label string) ([]ValueTypeDescriptor, error) {
	n, err := r.countElements(label, minVarintBytes)
	if err != nil {
		return nil, err
	}
	out := make([]ValueTypeDescriptor, n)
	for i := range out {
		out[i], err = r.valueType()
		if err != nil {
			return nil, fmt.Errorf("%s %d: %w", label, i, err)
		}
	}
	return out, nil
}

func (r *compiledReader) fieldType() (FieldTypeDescriptor, error) {
	var f FieldTypeDescriptor
	var err error
	if f.Storage.Packed, err = r.bool(); err != nil {
		return f, err
	}
	if f.Storage.Packed {
		pack, err := r.u8()
		if err != nil {
			return f, err
		}
		f.Storage.PackedType = PackedType(pack)
		if f.Storage.PackedType > PackedTypeI16 {
			return f, fmt.Errorf("invalid packed type %d", pack)
		}
	} else if f.Storage.Value, err = r.valueType(); err != nil {
		return f, err
	}
	f.Mutable, err = r.bool()
	return f, err
}

func (r *compiledReader) typeDescriptors() ([]DefinedTypeDescriptor, error) {
	n, err := r.countElements("defined types", minDefinedTypeBytes)
	if err != nil {
		return nil, err
	}
	out := make([]DefinedTypeDescriptor, n)
	for i := range out {
		d := &out[i]
		if d.RecGroup, err = r.u32(); err != nil {
			return nil, err
		}
		if d.Final, err = r.bool(); err != nil {
			return nil, err
		}
		sn, err := r.countElements("supertypes", minU32Bytes)
		if err != nil {
			return nil, err
		}
		if sn != 0 {
			d.Supers = make([]uint32, sn)
		}
		for j := range d.Supers {
			if d.Supers[j], err = r.u32(); err != nil {
				return nil, err
			}
		}
		if d.HasDescribes, err = r.bool(); err != nil {
			return nil, err
		}
		if d.HasDescribes {
			if d.Describes, err = r.u32(); err != nil {
				return nil, err
			}
		}
		if d.HasDescriptor, err = r.bool(); err != nil {
			return nil, err
		}
		if d.HasDescriptor {
			if d.Descriptor, err = r.u32(); err != nil {
				return nil, err
			}
		}
		kind, err := r.u8()
		if err != nil {
			return nil, err
		}
		d.Kind = CompositeTypeKind(kind)
		switch d.Kind {
		case CompositeTypeFunction:
			if d.Params, err = r.valueTypes("function parameters"); err != nil {
				return nil, err
			}
			if d.Results, err = r.valueTypes("function results"); err != nil {
				return nil, err
			}
		case CompositeTypeStruct:
			fn, err := r.countElements("struct fields", minFieldTypeBytes)
			if err != nil {
				return nil, err
			}
			d.Fields = make([]FieldTypeDescriptor, fn)
			for j := range d.Fields {
				if d.Fields[j], err = r.fieldType(); err != nil {
					return nil, err
				}
			}
		case CompositeTypeArray:
			if d.Array, err = r.fieldType(); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("invalid composite type kind %d", kind)
		}
	}
	if err := validateDefinedTypeDescriptors(out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *compiledReader) funcSigs(types []DefinedTypeDescriptor) ([]FuncSig, error) {
	n, err := r.countElements("function signatures", minFuncSigBytes)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	out := make([]FuncSig, n)
	for i := range out {
		out[i].HasTypeIndex, err = r.bool()
		if err != nil {
			return nil, err
		}
		if out[i].HasTypeIndex {
			out[i].TypeIndex, err = r.u32()
			if err != nil {
				return nil, err
			}
			if int(out[i].TypeIndex) >= len(types) || types[out[i].TypeIndex].Kind != CompositeTypeFunction {
				return nil, fmt.Errorf("function signature %d type index %d is not a function", i, out[i].TypeIndex)
			}
			params, results := types[out[i].TypeIndex].Params, types[out[i].TypeIndex].Results
			out[i].Params, err = valTypesFromDescriptors(params, types)
			if err != nil {
				return nil, fmt.Errorf("function signature %d params: %w", i, err)
			}
			out[i].Results, err = valTypesFromDescriptors(results, types)
			if err != nil {
				return nil, fmt.Errorf("function signature %d results: %w", i, err)
			}
			continue
		}
		params, err := r.valueTypes("function parameters")
		if err != nil {
			return nil, err
		}
		out[i].Params, err = valTypesFromDescriptors(params, types)
		if err != nil {
			return nil, fmt.Errorf("function signature %d params: %w", i, err)
		}
		results, err := r.valueTypes("function results")
		if err != nil {
			return nil, err
		}
		out[i].Results, err = valTypesFromDescriptors(results, types)
		if err != nil {
			return nil, fmt.Errorf("function signature %d results: %w", i, err)
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
	expr, err := r.bytes()
	if err != nil {
		return OffsetInit{}, err
	}
	if len(expr) == 0 {
		expr = nil
	}
	return OffsetInit{Base: base, HasGlobal: has, Global: glob, Expr: expr}, nil
}
func (r *compiledReader) elems() ([]ElemInit, error) {
	n, err := r.countElements("element segments", minElemInitBytes)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	out := make([]ElemInit, n)
	for i := range out {
		out[i].TableIndex, err = r.u32()
		if err != nil {
			return nil, err
		}
		out[i].RefType, err = r.valType()
		if err != nil {
			return nil, err
		}
		mode, err := r.u8()
		if err != nil {
			return nil, err
		}
		out[i].Mode = ElemMode(mode)
		out[i].Offset, err = r.offset()
		if err != nil {
			return nil, err
		}
		vn, err := r.countElements("element values", 1)
		if err != nil {
			return nil, err
		}
		if vn != 0 {
			out[i].Values = make([]RefInit, vn)
		}
		for j := range out[i].Values {
			tag, err := r.u8()
			if err != nil {
				return nil, err
			}
			switch tag {
			case 0:
				out[i].Values[j].Null = true
			case 1:
				out[i].Values[j].FuncIndex, err = r.u32()
				if err != nil {
					return nil, err
				}
			default:
				return nil, fmt.Errorf("invalid element initializer tag %d", tag)
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
func (r *compiledReader) passiveDataInits() ([]PassiveDataInit, error) {
	n, err := r.countElements("passive data segments", minPassiveDataBytes)
	if err != nil {
		return nil, err
	}
	out := make([]PassiveDataInit, n)
	for i := range out {
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
		kind, err := r.u8()
		if err != nil {
			return nil, err
		}
		switch kind {
		case 0:
			out[i].Bits, err = r.u64()
			if err != nil {
				return nil, err
			}
			if out[i].Type == ValV128 {
				vec, err := r.take(16)
				if err != nil {
					return nil, err
				}
				copy(out[i].V128[:], vec)
			}
		case 1:
			out[i].HasInitGlobal = true
			out[i].InitGlobal, err = r.ivar()
			if err != nil {
				return nil, err
			}
		case 2:
			out[i].HasInitFunc = true
			out[i].InitFunc, err = r.u32()
			if err != nil {
				return nil, err
			}
		case 3:
			out[i].InitExpr, err = r.bytes()
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("invalid global initializer kind %d", kind)
		}
	}
	return out, nil
}
func (r *compiledReader) tables(c *Compiled) error {
	n, err := r.countElements("tables", minTableBytes)
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	c.HasTable = true
	if n > 1 {
		c.extraTables = make([]tableDef, n-1)
	}
	for i := 0; i < n; i++ {
		typ, err := r.valType()
		if err != nil {
			return err
		}
		kind, err := r.u8()
		if err != nil {
			return err
		}
		var def tableDef
		def.Type = typ
		switch kind {
		case 0:
			size, err := r.uvar()
			if err != nil {
				return err
			}
			max, err := r.uvar()
			if err != nil {
				return err
			}
			if size > uint64(maxInt()) || max > uint64(maxInt()) {
				return fmt.Errorf("table %d limits overflow int", i)
			}
			def.Size, def.Max = int(size), int(max)
			def.HasMax, err = r.bool()
			if err != nil {
				return err
			}
			def.HasInitFunc, err = r.bool()
			if err != nil {
				return err
			}
			if def.HasInitFunc {
				def.InitFunc, err = r.u32()
				if err != nil {
					return err
				}
			}
		case 1:
			def.ImportKey, err = r.strLabel("table import key")
			if err != nil {
				return err
			}
			min, err := r.uvar()
			if err != nil {
				return err
			}
			max, err := r.uvar()
			if err != nil {
				return err
			}
			if min > uint64(maxInt()) || max > uint64(maxInt()) {
				return fmt.Errorf("table import %d limits overflow int", i)
			}
			def.Size, def.Max = int(min), int(max)
			def.ImportHasMax, err = r.bool()
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("invalid table %d kind %d", i, kind)
		}
		if i == 0 {
			c.TableType = def.Type
			if def.ImportKey != "" {
				c.tableImport = def.ImportKey
				c.tableImportMin = def.Size
				c.tableImportMax = def.Max
				c.tableImportHasMax = def.ImportHasMax
			} else {
				c.TableSize = def.Size
				c.TableMax = def.Max
				c.TableHasMax = def.HasMax
				c.HasTableInitFunc = def.HasInitFunc
				c.TableInitFunc = def.InitFunc
			}
		} else {
			c.extraTables[i-1] = def
		}
	}
	return nil
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
