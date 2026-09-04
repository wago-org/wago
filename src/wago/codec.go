package wago

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"
	"sort"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

const (
	compiledSectionCode     = 1
	compiledSectionMetadata = 2
	compiledSectionCount    = 2

	// Internal CPU/execution bits share the persisted u64 requirement word but
	// are stripped before exposing CoreFeatures. Public feature bits occupy the
	// low range; reserving the top nine bits avoids growing artifacts.
	compiledGCExecutionI31Product         uint64 = 1 << 55
	compiledFuncRefContextHeader          uint64 = 1 << 56
	compiledDynamicFuncrefEscape          uint64 = 1 << 57
	compiledRegisterABIDisabled           uint64 = 1 << 58
	compiledAtomicWaitExecution           uint64 = 1 << 59
	compiledCPUFeatureBMI2                uint64 = 1 << 60
	compiledGCExecutionDynamicFuncRefTest uint64 = 1 << 61
	compiledGCExecutionGenericStruct      uint64 = 1 << 62
	compiledGCExecutionGenericArray       uint64 = 1 << 63
	compiledGCExecutionMask                      = compiledGCExecutionI31Product | compiledGCExecutionDynamicFuncRefTest | compiledGCExecutionGenericStruct | compiledGCExecutionGenericArray

	// Import names are attacker-controlled artifact metadata. Bound the decoded
	// string headers plus exact-name sidecar independently of the encoded section
	// size so compact empty strings cannot amplify into multi-gigabyte allocations.
	maxImportDirectoryAllocationBytes = 64 << 20
)

func compiledUvarintLen(v uint64) int {
	var buf [binary.MaxVarintLen64]byte
	return binary.PutUvarint(buf[:], v)
}

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
	for _, typ := range c.Types {
		switch typ.Kind {
		case CompositeTypeStruct:
			for _, field := range typ.Fields {
				if !field.Storage.Packed && field.Storage.Value.Kind == ValueTypeV128 {
					return true
				}
			}
		case CompositeTypeArray:
			if !typ.Array.Storage.Packed && typ.Array.Storage.Value.Kind == ValueTypeV128 {
				return true
			}
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
	metadata, err := marshalCompiledMetadata(c)
	if err != nil {
		return nil, err
	}
	w := compiledWriter{buf: make([]byte, 0, len(c.code)+len(metadata)+32)}
	w.buf = append(w.buf, wagoMagic...)
	w.u8(wagoVersion)
	w.u8(compiledSectionCount)
	w.section(compiledSectionCode, c.code)
	w.section(compiledSectionMetadata, metadata)
	return w.buf, nil
}

func writeCompiled(w io.Writer, c *Compiled) (int64, error) {
	if c == nil {
		return 0, fmt.Errorf("compiled module is nil")
	}
	metadata, err := marshalCompiledMetadata(c)
	if err != nil {
		return 0, err
	}
	written := int64(0)
	write := func(p []byte) error {
		n, err := w.Write(p)
		written += int64(n)
		if err != nil {
			return err
		}
		if n != len(p) {
			return io.ErrShortWrite
		}
		return nil
	}
	if err := write([]byte{wagoMagic[0], wagoMagic[1], wagoMagic[2], wagoMagic[3], wagoVersion, compiledSectionCount}); err != nil {
		return written, err
	}
	var header [1 + binary.MaxVarintLen64]byte
	writeSection := func(id byte, payload []byte) error {
		header[0] = id
		n := binary.PutUvarint(header[1:], uint64(len(payload)))
		if err := write(header[:1+n]); err != nil {
			return err
		}
		return write(payload)
	}
	if err := writeSection(compiledSectionCode, c.code); err != nil {
		return written, err
	}
	if err := writeSection(compiledSectionMetadata, metadata); err != nil {
		return written, err
	}
	return written, nil
}

type artifactCountingReader struct {
	r io.Reader
	n int64
}

func (r *artifactCountingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}

func readArtifactUvar(r *artifactCountingReader) (uint64, error) {
	var encoded [binary.MaxVarintLen64]byte
	for i := range encoded {
		if _, err := io.ReadFull(r, encoded[i:i+1]); err != nil {
			return 0, err
		}
		if encoded[i]&0x80 == 0 {
			value, n := binary.Uvarint(encoded[:i+1])
			if n != i+1 {
				return 0, fmt.Errorf("invalid section length")
			}
			var canonical [binary.MaxVarintLen64]byte
			if binary.PutUvarint(canonical[:], value) != n {
				return 0, fmt.Errorf("non-canonical section length")
			}
			return value, nil
		}
	}
	return 0, fmt.Errorf("section length overflows u64")
}

func readCompiledFrom(source io.Reader, limits ArtifactLimits) (decoded Compiled, image *coreruntime.CodeBuffer, read int64, err error) {
	if source == nil {
		return decoded, nil, 0, fmt.Errorf("wago: compiled artifact reader is nil")
	}
	if limits.MaxCodeBytes < 0 || limits.MaxMetadataBytes < 0 {
		return decoded, nil, 0, fmt.Errorf("wago: compiled artifact limits must be non-negative")
	}
	r := &artifactCountingReader{r: source}
	defer func() { read = r.n }()
	var header [6]byte
	if _, err = io.ReadFull(r, header[:]); err != nil {
		return decoded, nil, 0, fmt.Errorf("compiled artifact header: %w", err)
	}
	if string(header[:4]) != wagoMagic {
		return decoded, nil, 0, fmt.Errorf("not a wago module")
	}
	if header[4] != wagoVersion {
		return decoded, nil, 0, fmt.Errorf("wago module version %d unsupported (want %d)", header[4], wagoVersion)
	}
	if header[5] != compiledSectionCount {
		return decoded, nil, 0, fmt.Errorf("compiled section count %d unsupported (want %d)", header[5], compiledSectionCount)
	}
	readSectionHeader := func(want byte, label string, limit int64) (int, error) {
		var id [1]byte
		if _, err := io.ReadFull(r, id[:]); err != nil {
			return 0, fmt.Errorf("%s section id: %w", label, err)
		}
		if id[0] != want {
			return 0, fmt.Errorf("compiled section %d out of order (want %s section %d)", id[0], label, want)
		}
		size, err := readArtifactUvar(r)
		if err != nil {
			return 0, fmt.Errorf("%s section length: %w", label, err)
		}
		if size > uint64(limit) {
			return 0, fmt.Errorf("%s section length %d exceeds limit %d", label, size, limit)
		}
		if size > uint64(maxInt()) {
			return 0, fmt.Errorf("%s section length %d overflows int", label, size)
		}
		return int(size), nil
	}
	codeLen, err := readSectionHeader(compiledSectionCode, "code", limits.MaxCodeBytes)
	if err != nil {
		return decoded, nil, 0, err
	}
	image, err = coreruntime.NewCodeBuffer(codeLen)
	if err != nil {
		return decoded, nil, 0, fmt.Errorf("allocate compiled code section: %w", err)
	}
	code, err := image.AppendSpace(codeLen)
	if err != nil {
		_ = image.Close()
		return decoded, nil, 0, fmt.Errorf("size compiled code section: %w", err)
	}
	if _, err := io.ReadFull(r, code); err != nil {
		_ = image.Close()
		return decoded, nil, 0, fmt.Errorf("truncated code section: %w", err)
	}
	metadataLen, err := readSectionHeader(compiledSectionMetadata, "metadata", limits.MaxMetadataBytes)
	if err != nil {
		_ = image.Close()
		return decoded, nil, 0, err
	}
	metadata := make([]byte, metadataLen)
	if _, err := io.ReadFull(r, metadata); err != nil {
		_ = image.Close()
		return decoded, nil, 0, fmt.Errorf("truncated metadata section: %w", err)
	}
	decoded.code = code
	if err := unmarshalCompiledMetadata(&decoded, metadata); err != nil {
		_ = image.Close()
		return decoded, nil, 0, err
	}
	return decoded, image, 0, nil
}

func marshalCompiledMetadata(c *Compiled) ([]byte, error) {
	metadata, _, err := marshalCompiledMetadataMeasured(c)
	return metadata, err
}

func marshalCompiledMetadataMeasured(c *Compiled) ([]byte, ArtifactSectionSizes, error) {
	w := compiledWriter{buf: make([]byte, 0, 256)}
	var sizes ArtifactSectionSizes
	start := 0
	mark := func(dst *int64) {
		*dst = int64(len(w.buf) - start)
		start = len(w.buf)
	}
	w.intSlice(c.Entry)
	w.internalEntrySlice(c.InternalEntry)
	mark(&sizes.Entries)
	w.uvar(uint64(c.NumImports))
	w.stringSlice(c.Imports)
	funcModuleEnds, _, _, _, exactImportNames := c.importModuleEndSections()
	for i := range c.Imports {
		moduleEnd := uint64(0)
		if exactImportNames {
			moduleEnd = funcModuleEnds[i]
		}
		w.uvar(moduleEnd)
	}
	mark(&sizes.Imports)
	if err := w.typeDescriptors(c.Types); err != nil {
		return nil, sizes, err
	}
	if err := validateValueTypeDescriptors(c.Types, c.ValueTypes); err != nil {
		return nil, sizes, err
	}
	w.valueTypes(c.ValueTypes)
	mark(&sizes.Types)
	if err := w.funcSigs(c.importFuncSigs, c.Types); err != nil {
		return nil, sizes, err
	}
	if err := w.funcSigs(c.Funcs, c.Types); err != nil {
		return nil, sizes, err
	}
	mark(&sizes.Functions)
	w.stringIntMap(c.Exports)
	w.nameSec(c.Names)
	mark(&sizes.ExportsAndNames)
	if err := w.globalImports(c.GlobalImports, c); err != nil {
		return nil, sizes, err
	}
	if err := w.globals(c.Globals, c); err != nil {
		return nil, sizes, err
	}
	w.stringIntMap(c.GlobalExports)
	mark(&sizes.Globals)
	if err := w.tables(c); err != nil {
		return nil, sizes, err
	}
	w.stringIntMap(c.tableExports)
	w.u64Slice(c.FuncTypeID)
	w.bool(c.NeedsFuncRefDescs)
	mark(&sizes.Tables)
	if err := w.elems(c.Elems, c); err != nil {
		return nil, sizes, err
	}
	if err := w.elems(c.passiveElems, c); err != nil {
		return nil, sizes, err
	}
	mark(&sizes.Elements)
	w.data(c.Data)
	w.passiveData(c.PassiveData)
	mark(&sizes.Data)
	w.memories(c)
	w.stringIntMap(c.memoryExportMap())
	mark(&sizes.Memories)
	w.bool(c.dynamicImports)
	sizes.Features = int64(len(w.buf) - start)
	start = len(w.buf)
	w.tags(c)
	mark(&sizes.Tags)
	required := uint64(compiledStructuralRequiredFeatures(c))
	if c.stagedGCI31Product() != 0 {
		required |= compiledGCExecutionI31Product
	}
	if c.stagedGCStructProduct() == stagedGCStructGeneric {
		required |= compiledGCExecutionGenericStruct
	}
	if c.stagedGCArrayProduct() == stagedGCArrayProductGeneric {
		required |= compiledGCExecutionGenericArray
	}
	if c.usesDynamicFuncRefTest() {
		required |= compiledGCExecutionDynamicFuncRefTest
	}
	if c.usesAtomicWaitHelpers() {
		required |= compiledAtomicWaitExecution
	}
	if c.requiresBMI2 {
		required |= compiledCPUFeatureBMI2
	}
	if c.needsFuncRefContextHeader {
		required |= compiledFuncRefContextHeader
	}
	if c.dynamicFuncrefEscape {
		required |= compiledDynamicFuncrefEscape
	}
	if c.registerABIDisabled {
		required |= compiledRegisterABIDisabled
	}
	w.u64(required)
	sizes.Features += int64(len(w.buf) - start)
	start = len(w.buf)
	if required&(compiledGCExecutionGenericStruct|compiledGCExecutionGenericArray) != 0 || c.hasCollectorReferenceCallBoundary() {
		w.u32(c.nativeGCABIRequirement())
	}
	w.gcTypeDescs(c.GCTypeDescs)
	if err := validateCompiledGCFrameRoots(c, c.genericGCFrameRoots()); err != nil {
		return nil, sizes, err
	}
	if rootMap := c.genericGCFrameRoots(); rootMap != nil {
		w.gcFrameRoots(rootMap)
	}
	mark(&sizes.GC)
	return w.buf, sizes, nil
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
func (w *compiledWriter) section(id byte, payload []byte) {
	w.u8(id)
	w.uvar(uint64(len(payload)))
	w.buf = append(w.buf, payload...)
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
func (w *compiledWriter) internalEntrySlice(v []int) {
	w.uvar(uint64(len(v)))
	for _, x := range v {
		w.ivar(internalEntryOffset(x))
	}
}
func (w *compiledWriter) u64Slice(v []uint64) {
	w.uvar(uint64(len(v)))
	for _, x := range v {
		w.u64(x)
	}
}
func (w *compiledWriter) tags(c *Compiled) {
	if c.memoryDir == nil {
		w.uvar(0)
		w.stringIntMap(nil)
		return
	}
	_, _, _, tagModuleEnds, exactImportNames := c.importModuleEndSections()
	w.uvar(uint64(len(c.memoryDir.ehTags)))
	for i, tag := range c.memoryDir.ehTags {
		w.str(tag.ImportKey)
		moduleEnd := uint64(0)
		if exactImportNames && tag.ImportKey != "" {
			moduleEnd = tagModuleEnds[i]
		}
		w.uvar(moduleEnd)
		w.u32(tag.TypeIndex)
	}
	w.stringIntMap(c.memoryDir.ehTagExports)
}

func (w *compiledWriter) memories(c *Compiled) {
	_, _, memoryModuleEnds, _, exactImportNames := c.importModuleEndSections()
	w.uvar(uint64(c.memoryCount()))
	for i := 0; i < c.memoryCount(); i++ {
		def := c.memoryDef(i)
		w.str(def.ImportKey)
		moduleEnd := uint64(0)
		if exactImportNames && def.ImportKey != "" {
			moduleEnd = memoryModuleEnds[i]
		}
		w.uvar(moduleEnd)
		w.uvar(def.Min)
		w.uvar(def.Max)
		w.bool(def.HasMax)
		w.bool(def.Addr64)
		w.bool(def.Shared)
	}
	w.bool(c.HasMemory)
	w.u32(c.MemMinPages)
	w.u32(c.MemMaxPages)
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
func (w *compiledWriter) valueTypeRef(legacy ValType, has bool, index uint32, pool []ValueTypeDescriptor, types []DefinedTypeDescriptor) error {
	if _, err := exactValueType(legacy, has, index, pool, types); err != nil {
		return err
	}
	w.bool(has)
	if has {
		w.u32(index)
		return nil
	}
	return w.valType(legacy)
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
func (w *compiledWriter) elems(v []ElemInit, c *Compiled) error {
	w.uvar(uint64(len(v)))
	for _, e := range v {
		w.u32(e.TableIndex)
		if err := w.valueTypeRef(normalizedElemRefType(e.RefType), e.HasValueType, e.ValueTypeIndex, c.ValueTypes, c.Types); err != nil {
			return err
		}
		w.u8(byte(e.Mode))
		w.offset(e.Offset)
		w.uvar(uint64(len(e.Values)))
		for _, value := range e.Values {
			switch {
			case value.Null:
				w.u8(0)
			case len(value.Expr) != 0:
				w.u8(4)
				w.bytes(value.Expr)
			case value.HasGlobal && value.I31Wrap:
				w.u8(3)
				w.u32(value.GlobalIndex)
			case value.HasGlobal:
				w.u8(2)
				w.u32(value.GlobalIndex)
			default:
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
		w.u32(d.MemoryIndex)
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
func (w *compiledWriter) globals(v []GlobalDef, c *Compiled) error {
	w.uvar(uint64(len(v)))
	for _, g := range v {
		if err := w.valueTypeRef(g.Type, g.HasValueType, g.ValueTypeIndex, c.ValueTypes, c.Types); err != nil {
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
	_, tableModuleEnds, _, _, exactImportNames := c.importModuleEndSections()
	w.uvar(uint64(count))
	for i := 0; i < count; i++ {
		def := c.tableDef(i)
		if err := w.valueTypeRef(c.tableElementType(i), def.HasValueType, def.ValueTypeIndex, c.ValueTypes, c.Types); err != nil {
			return err
		}
		w.bool(def.Addr64)
		if imp, ok := c.tableImportAt(i); ok {
			w.u8(1)
			w.str(imp.Key)
			moduleEnd := uint64(0)
			if exactImportNames {
				moduleEnd = tableModuleEnds[i]
			}
			w.uvar(moduleEnd)
			w.uvar(uint64(imp.Min))
			w.uvar(uint64(imp.Max))
			w.bool(imp.HasMax)
			continue
		}
		w.u8(0)
		w.uvar(uint64(def.Size))
		w.uvar(def.Max)
		w.bool(def.HasMax)
		w.bool(def.HasInitFunc)
		if def.HasInitFunc {
			w.u32(def.InitFunc)
		}
	}
	return nil
}
func (w *compiledWriter) globalImports(v []GlobalImportDef, c *Compiled) error {
	w.uvar(uint64(len(v)))
	for _, g := range v {
		w.str(g.Module)
		w.str(g.Name)
		if err := w.valueTypeRef(g.Type, g.HasValueType, g.ValueTypeIndex, c.ValueTypes, c.Types); err != nil {
			return err
		}
		w.bool(g.Mutable)
	}
	return nil
}
func (w *compiledWriter) gcFrameRoots(rootMap *compiledGCFrameRoots) {
	w.uvar(uint64(len(rootMap.adapterReturnOffsets)))
	for _, off := range rootMap.adapterReturnOffsets {
		w.u32(off)
	}
	w.uvar(uint64(len(rootMap.safepoints)))
	for i := range rootMap.safepoints {
		w.u32(rootMap.safepoints[i].id)
		w.u32(rootMap.safepoints[i].frameBytes)
		w.uvar(uint64(len(rootMap.safepoints[i].offsets)))
		for _, off := range rootMap.safepoints[i].offsets {
			w.u32(off)
		}
	}
	w.uvar(uint64(len(rootMap.callsites)))
	for i := range rootMap.callsites {
		w.u32(rootMap.callsites[i].returnOffset)
		w.u32(rootMap.callsites[i].frameBytes)
		w.u32(rootMap.callsites[i].stackAdjust)
		w.uvar(uint64(len(rootMap.callsites[i].offsets)))
		for _, off := range rootMap.callsites[i].offsets {
			w.u32(off)
		}
	}
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
	count, err := r.u8()
	if err != nil {
		return fmt.Errorf("compiled section count: %w", err)
	}
	if count != compiledSectionCount {
		return fmt.Errorf("compiled section count %d unsupported (want %d)", count, compiledSectionCount)
	}
	code, err := r.requiredSection(compiledSectionCode, "code")
	if err != nil {
		return err
	}
	metadata, err := r.requiredSection(compiledSectionMetadata, "metadata")
	if err != nil {
		return err
	}
	if len(r.data) != 0 {
		return fmt.Errorf("trailing %d byte(s) after compiled sections", len(r.data))
	}
	c.code = code
	return unmarshalCompiledMetadata(c, metadata)
}

func unmarshalCompiledMetadata(c *Compiled, data []byte) error {
	r := compiledReader{data: data}
	var err error
	c.Entry, err = r.intSlice()
	if err != nil {
		return err
	}
	c.InternalEntry, err = r.intSlice()
	if err != nil {
		return err
	}
	if err := c.validateInternalEntries(true); err != nil {
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
	var functionModuleEnds []uint64
	c.Imports, functionModuleEnds, err = r.importDirectory(c.NumImports)
	if err != nil {
		return err
	}
	if len(functionModuleEnds) != 0 {
		c.validateMemo = &validateMemo{importModuleEnds: functionModuleEnds}
	}
	c.Types, err = r.typeDescriptors()
	if err != nil {
		return err
	}
	c.ValueTypes, err = r.valueTypes("value type pool")
	if err != nil {
		return err
	}
	if err := validateValueTypeDescriptors(c.Types, c.ValueTypes); err != nil {
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
	c.GlobalImports, err = r.globalImports(c.ValueTypes, c.Types)
	if err != nil {
		return err
	}
	c.Globals, err = r.globals(c.ValueTypes, c.Types)
	if err != nil {
		return err
	}
	c.GlobalExports, err = r.stringIntMap()
	if err != nil {
		return err
	}
	if err := r.tables(c, c.ValueTypes, c.Types); err != nil {
		return err
	}
	c.tableExports, err = r.stringIntMap()
	if err != nil {
		return err
	}
	c.FuncTypeID, err = r.u64Slice()
	if err != nil {
		return err
	}
	c.NeedsFuncRefDescs, err = r.bool()
	if err != nil {
		return err
	}
	c.Elems, err = r.elems(c.ValueTypes, c.Types)
	if err != nil {
		return err
	}
	c.passiveElems, err = r.elems(c.ValueTypes, c.Types)
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
	if err := r.memories(c); err != nil {
		return err
	}
	if c.memoryDir == nil {
		c.memoryDir = &compiledMemoryDirectory{}
	}
	c.memoryDir.exports, err = r.stringIntMap()
	if err != nil {
		return err
	}
	c.dynamicImports, err = r.bool()
	if err != nil {
		return err
	}
	if err := r.tags(c); err != nil {
		return err
	}
	required, err := r.u64()
	if err != nil {
		return err
	}
	gcExecution := required & compiledGCExecutionMask
	c.requiresBMI2 = required&compiledCPUFeatureBMI2 != 0
	c.needsFuncRefContextHeader = required&compiledFuncRefContextHeader != 0
	c.dynamicFuncrefEscape = required&compiledDynamicFuncrefEscape != 0
	c.registerABIDisabled = required&compiledRegisterABIDisabled != 0
	c.requiredFeatures = CoreFeatures(required &^ (compiledFuncRefContextHeader | compiledDynamicFuncrefEscape | compiledRegisterABIDisabled | compiledAtomicWaitExecution | compiledGCExecutionMask | compiledCPUFeatureBMI2))
	genericNativeGC := gcExecution&(compiledGCExecutionGenericStruct|compiledGCExecutionGenericArray) != 0
	if genericNativeGC || c.hasCollectorReferenceCallBoundary() {
		label := "native GC call-boundary"
		if genericNativeGC {
			label = "generic GC native"
		}
		nativeGCABIVersion, readErr := r.u32()
		if readErr != nil {
			return fmt.Errorf("%s ABI version: %w", label, readErr)
		}
		if nativeGCABIVersion != gc.NativeABIVersion {
			return fmt.Errorf("%s ABI version %d unsupported (want %d)", label, nativeGCABIVersion, gc.NativeABIVersion)
		}
		c.ensureCodeCache()
		c.codeCache.setNativeGCABIVersion(nativeGCABIVersion)
	}
	c.GCTypeDescs, err = r.gcTypeDescs()
	if err != nil {
		return err
	}
	var gcFrameRoots *compiledGCFrameRoots
	if len(r.data) != 0 {
		gcFrameRoots, err = r.gcFrameRoots()
		if err != nil {
			return err
		}
	}
	if gcExecution&compiledGCExecutionDynamicFuncRefTest != 0 {
		if !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) || len(c.FuncTypeID) == 0 || !c.needsFuncRefDescs() {
			return fmt.Errorf("dynamic indexed-function ref.test execution flag requires typed function descriptor metadata")
		}
		c.ensureCodeCache()
		c.codeCache.stagedFeatures |= CoreFeatureTypedFunctionReferences
		c.codeCache.flags |= compiledCacheDynamicFuncRefTest
	}
	if gcExecution&compiledGCExecutionI31Product != 0 {
		if !c.requiredFeatures.IsEnabled(CoreFeatureGC) {
			return fmt.Errorf("i31 execution product flag requires the recorded GC feature")
		}
		c.ensureCodeCache()
		c.codeCache.stagedFeatures |= c.requiredFeatures & (CoreFeatureGC | CoreFeatureTypedFunctionReferences)
		c.codeCache.gcI31Product = stagedGCI31ProductCore
	}
	if required&compiledAtomicWaitExecution != 0 {
		if !c.requiredFeatures.IsEnabled(CoreFeatureThreads) {
			return fmt.Errorf("atomic wait execution flag requires recorded threads feature")
		}
		c.ensureCodeCache()
		c.codeCache.flags |= compiledCacheAtomicWaitHelpers
	}
	genericGCExecution := gcExecution & (compiledGCExecutionGenericStruct | compiledGCExecutionGenericArray)
	if genericGCExecution != 0 {
		if !c.requiredFeatures.IsEnabled(CoreFeatureGC) || !gc.HasHeapObjectTypes(c.GCTypeDescs) {
			return fmt.Errorf("generic GC execution flags %#x require recorded GC heap metadata", genericGCExecution)
		}
		hasStruct, hasArray := false, false
		for _, desc := range c.GCTypeDescs {
			hasStruct = hasStruct || desc.Kind == gc.KindStruct
			hasArray = hasArray || desc.Kind == gc.KindArray
		}
		if genericGCExecution&compiledGCExecutionGenericStruct != 0 && !hasStruct && !hasArray {
			return fmt.Errorf("generic GC reference-helper execution flag has no struct or array descriptor")
		}
		if genericGCExecution&compiledGCExecutionGenericArray != 0 && !hasArray {
			return fmt.Errorf("generic GC array execution flag has no array descriptor")
		}
		c.ensureCodeCache()
		c.codeCache.stagedFeatures |= CoreFeatureGC | CoreFeatureTypedFunctionReferences
		if genericGCExecution&compiledGCExecutionGenericStruct != 0 {
			c.codeCache.gcStructProduct = stagedGCStructGeneric
		}
		if genericGCExecution&compiledGCExecutionGenericArray != 0 {
			c.codeCache.gcArrayProduct = stagedGCArrayProductGeneric
		}
	}
	if gcFrameRoots != nil {
		if c.validateMemo == nil {
			c.validateMemo = &validateMemo{}
		}
		c.validateMemo.gcFrameRoots = gcFrameRoots
		if err := validateCompiledGCFrameRoots(c, gcFrameRoots); err != nil {
			return err
		}
	}
	if len(r.data) != 0 {
		return fmt.Errorf("trailing %d byte(s)", len(r.data))
	}
	return nil
}

type compiledReader struct{ data []byte }

func (r *compiledReader) requiredSection(want byte, label string) ([]byte, error) {
	id, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("%s section id: %w", label, err)
	}
	if id != want {
		return nil, fmt.Errorf("compiled section %d out of order (want %s section %d)", id, label, want)
	}
	size, err := r.canonicalUvar()
	if err != nil {
		return nil, fmt.Errorf("%s section length: %w", label, err)
	}
	if size > uint64(maxInt()) {
		return nil, fmt.Errorf("%s section length overflows int", label)
	}
	payload, err := r.take(int(size))
	if err != nil {
		return nil, fmt.Errorf("truncated %s section: %w", label, err)
	}
	return payload, nil
}

func (r *compiledReader) canonicalUvar() (uint64, error) {
	return r.uvar()
}

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
	minDataInitBytes     = minU32Bytes + minOffsetInitBytes + minStringBytes
	minPassiveDataBytes  = minStringBytes
	minGlobalBytes       = 1 + 1 + 1
	minTableBytes        = 1 + 1 + minVarintBytes + minVarintBytes + 1
	minGlobalImportBytes = minStringBytes + minStringBytes + 1 + 1
	minTagBytes          = minStringBytes + minVarintBytes + minU32Bytes
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
	var canonical [binary.MaxVarintLen64]byte
	if binary.PutUvarint(canonical[:], v) != n {
		return 0, fmt.Errorf("non-canonical uvarint")
	}
	r.data = r.data[n:]
	return v, nil
}

func (r *compiledReader) importModuleEnd(key string) (uint64, error) {
	moduleEnd, err := r.uvar()
	if err != nil {
		return 0, err
	}
	if err := validateImportModuleEnd(key, moduleEnd); err != nil {
		return 0, err
	}
	return moduleEnd, nil
}
func (r *compiledReader) ivar() (int, error) {
	v, n := binary.Varint(r.data)
	if n <= 0 {
		return 0, fmt.Errorf("invalid varint")
	}
	var canonical [binary.MaxVarintLen64]byte
	if binary.PutVarint(canonical[:], v) != n {
		return 0, fmt.Errorf("non-canonical varint")
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

func (r *compiledReader) importDirectory(want int) ([]string, []uint64, error) {
	return r.importDirectoryWithAllocationLimit(want, maxImportDirectoryAllocationBytes)
}

func (r *compiledReader) importDirectoryWithAllocationLimit(want, allocationLimit int) ([]string, []uint64, error) {
	// Each entry needs at least an empty string length and one module-boundary
	// varint in the remainder. This also rejects an impossible count before any
	// decoded directory allocation.
	n, err := r.countElements("function imports", minStringBytes+minVarintBytes)
	if err != nil {
		return nil, nil, err
	}
	if n != want {
		return nil, nil, fmt.Errorf("function import directory count %d != NumImports %d", n, want)
	}
	const moduleEndBytes = 8
	decodedBytesPerImport := bits.UintSize/4 + moduleEndBytes // string header + uint64
	if allocationLimit < 0 || n > allocationLimit/decodedBytesPerImport {
		return nil, nil, fmt.Errorf("function import count %d exceeds decoded directory allocation limit %d bytes", n, allocationLimit)
	}
	keys := make([]string, n)
	for i := range keys {
		keys[i], err = r.str()
		if err != nil {
			return nil, nil, fmt.Errorf("function import %d key: %w", i, err)
		}
	}
	ends := make([]uint64, n)
	for i, key := range keys {
		ends[i], err = r.importModuleEnd(key)
		if err != nil {
			return nil, nil, fmt.Errorf("function import %d name: %w", i, err)
		}
	}
	return keys, ends, nil
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
func (r *compiledReader) u64Slice() ([]uint64, error) {
	n, err := r.countElements("u64 slice", 8)
	if err != nil {
		return nil, err
	}
	out := make([]uint64, n)
	for i := range out {
		out[i], err = r.u64()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (r *compiledReader) tags(c *Compiled) error {
	n, err := r.countElements("exception tags", minTagBytes)
	if err != nil {
		return err
	}
	if c.memoryDir == nil {
		c.memoryDir = &compiledMemoryDirectory{}
	}
	if n != 0 {
		c.memoryDir.ehTags = make([]compiledTagDef, n)
	}
	importEnded := false
	for i := range c.memoryDir.ehTags {
		tag := &c.memoryDir.ehTags[i]
		tag.ImportKey, err = r.str()
		if err != nil {
			return fmt.Errorf("exception tag %d import: %w", i, err)
		}
		moduleEnd, readErr := r.importModuleEnd(tag.ImportKey)
		if readErr != nil {
			return fmt.Errorf("exception tag %d import name: %w", i, readErr)
		}
		if tag.ImportKey != "" {
			c.appendImportModuleEnd(moduleEnd)
		}
		tag.TypeIndex, err = r.u32()
		if err != nil {
			return fmt.Errorf("exception tag %d type: %w", i, err)
		}
		if tag.ImportKey == "" {
			importEnded = true
		} else if importEnded {
			return fmt.Errorf("exception tag %d import follows local declaration", i)
		}
	}
	c.memoryDir.ehTagExports, err = r.stringIntMap()
	if err != nil {
		return fmt.Errorf("exception tag exports: %w", err)
	}
	return nil
}

func (r *compiledReader) memories(c *Compiled) error {
	n, err := r.countElements("memories", 7)
	if err != nil {
		return err
	}
	if c.memoryDir == nil {
		c.memoryDir = &compiledMemoryDirectory{}
	}
	if n != 0 {
		c.memoryDir.defs = make([]memoryDef, n)
	}
	for i := range c.memoryDir.defs {
		def := &c.memoryDir.defs[i]
		def.ImportKey, err = r.str()
		if err != nil {
			return fmt.Errorf("memory %d import: %w", i, err)
		}
		moduleEnd, readErr := r.importModuleEnd(def.ImportKey)
		if readErr != nil {
			return fmt.Errorf("memory %d import name: %w", i, readErr)
		}
		if def.ImportKey != "" {
			c.appendImportModuleEnd(moduleEnd)
		}
		def.Min, err = r.uvar()
		if err != nil {
			return fmt.Errorf("memory %d minimum: %w", i, err)
		}
		def.Max, err = r.uvar()
		if err != nil {
			return fmt.Errorf("memory %d maximum: %w", i, err)
		}
		def.HasMax, err = r.bool()
		if err != nil {
			return fmt.Errorf("memory %d has-max: %w", i, err)
		}
		def.Addr64, err = r.bool()
		if err != nil {
			return fmt.Errorf("memory %d address type: %w", i, err)
		}
		def.Shared, err = r.bool()
		if err != nil {
			return fmt.Errorf("memory %d shared flag: %w", i, err)
		}
		if i == 0 && def.ImportKey != "" {
			c.memoryImport = def.ImportKey
		}
	}
	c.HasMemory, err = r.bool()
	if err != nil {
		return err
	}
	c.MemMinPages, err = r.u32()
	if err != nil {
		return err
	}
	c.MemMaxPages, err = r.u32()
	if err == nil && len(c.memoryDir.defs) != 0 {
		c.MemHasMax = c.memoryDir.defs[0].HasMax
	}
	return err
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
func (r *compiledReader) valueTypeRef(pool []ValueTypeDescriptor, types []DefinedTypeDescriptor) (legacy ValType, index uint32, has bool, err error) {
	has, err = r.bool()
	if err != nil {
		return 0, 0, false, err
	}
	if has {
		index, err = r.u32()
		if err != nil {
			return 0, 0, false, err
		}
		if int(index) >= len(pool) {
			return 0, 0, false, fmt.Errorf("value type index %d out of range", index)
		}
		legacy, ok := pool[index].ABIType(types)
		if !ok {
			return 0, 0, false, fmt.Errorf("value type index %d is outside the current ABI", index)
		}
		return legacy, index, true, nil
	}
	legacy, err = r.valType()
	return legacy, 0, false, err
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
func (r *compiledReader) elems(pool []ValueTypeDescriptor, types []DefinedTypeDescriptor) ([]ElemInit, error) {
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
		out[i].RefType, out[i].ValueTypeIndex, out[i].HasValueType, err = r.valueTypeRef(pool, types)
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
			case 2, 3:
				out[i].Values[j].GlobalIndex, err = r.u32()
				if err != nil {
					return nil, err
				}
				out[i].Values[j].HasGlobal = true
				out[i].Values[j].I31Wrap = tag == 3
			case 4:
				out[i].Values[j].Expr, err = r.bytes()
				if err != nil {
					return nil, err
				}
				if len(out[i].Values[j].Expr) == 0 {
					return nil, fmt.Errorf("empty GC element initializer expression")
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
		out[i].MemoryIndex, err = r.u32()
		if err != nil {
			return nil, err
		}
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
func (r *compiledReader) globals(pool []ValueTypeDescriptor, types []DefinedTypeDescriptor) ([]GlobalDef, error) {
	n, err := r.countElements("globals", minGlobalBytes)
	if err != nil {
		return nil, err
	}
	out := make([]GlobalDef, n)
	for i := range out {
		out[i].Type, out[i].ValueTypeIndex, out[i].HasValueType, err = r.valueTypeRef(pool, types)
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
func (r *compiledReader) tables(c *Compiled, pool []ValueTypeDescriptor, types []DefinedTypeDescriptor) error {
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
		typ, valueTypeIndex, hasValueType, err := r.valueTypeRef(pool, types)
		if err != nil {
			return err
		}
		addr64, err := r.bool()
		if err != nil {
			return err
		}
		kind, err := r.u8()
		if err != nil {
			return err
		}
		var def tableDef
		def.Type, def.ValueTypeIndex, def.HasValueType, def.Addr64 = typ, valueTypeIndex, hasValueType, addr64
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
			def.HasMax, err = r.bool()
			if err != nil {
				return err
			}
			if size > uint64(maxInt()) || (max > uint64(maxInt()) && (!addr64 || !def.HasMax)) {
				return fmt.Errorf("table %d limits overflow executable capacity", i)
			}
			def.Size, def.Max = int(size), max
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
			moduleEnd, readErr := r.importModuleEnd(def.ImportKey)
			if readErr != nil {
				return fmt.Errorf("table import %d name: %w", i, readErr)
			}
			c.appendImportModuleEnd(moduleEnd)
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
			def.Size, def.Max = int(min), max
			def.ImportHasMax, err = r.bool()
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("invalid table %d kind %d", i, kind)
		}
		if i == 0 {
			c.TableType, c.TableValueTypeIndex, c.TableHasValueType, c.TableAddr64 = def.Type, def.ValueTypeIndex, def.HasValueType, def.Addr64
			if def.ImportKey != "" {
				c.tableImport = def.ImportKey
				c.tableImportMin = def.Size
				c.tableImportMax = int(def.Max)
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

func (r *compiledReader) globalImports(pool []ValueTypeDescriptor, types []DefinedTypeDescriptor) ([]GlobalImportDef, error) {
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
		out[i].Type, out[i].ValueTypeIndex, out[i].HasValueType, err = r.valueTypeRef(pool, types)
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

func (r *compiledReader) gcFrameRoots() (*compiledGCFrameRoots, error) {
	adapterCount, err := r.countElements("GC frame adapter returns", 4)
	if err != nil {
		return nil, err
	}
	rootMap := &compiledGCFrameRoots{adapterReturnOffsets: make([]uint32, adapterCount)}
	var offsetInterner gcFrameOffsetInterner
	for i := range rootMap.adapterReturnOffsets {
		rootMap.adapterReturnOffsets[i], err = r.u32()
		if err != nil {
			return nil, err
		}
	}
	n, err := r.countElements("GC frame safepoints", 9)
	if err != nil {
		return nil, err
	}
	if uint64(n) > uint64(shared.GCSafepointIDMax) {
		return nil, fmt.Errorf("GC frame safepoint count %d is invalid", n)
	}
	rootMap.safepoints = make([]compiledGCFrameSafepoint, n)
	for i := range rootMap.safepoints {
		rootMap.safepoints[i].id, err = r.u32()
		if err != nil {
			return nil, err
		}
		rootMap.safepoints[i].frameBytes, err = r.u32()
		if err != nil {
			return nil, err
		}
		count, err := r.countElements("GC frame root offsets", 4)
		if err != nil {
			return nil, err
		}
		rootMap.safepoints[i].offsets = make([]uint32, count)
		for j := range rootMap.safepoints[i].offsets {
			rootMap.safepoints[i].offsets[j], err = r.u32()
			if err != nil {
				return nil, err
			}
		}
		rootMap.safepoints[i].offsets = offsetInterner.intern(rootMap.safepoints[i].offsets, false)
	}
	callCount, err := r.countElements("GC frame callsites", 13)
	if err != nil {
		return nil, err
	}
	if n == 0 && callCount == 0 {
		return nil, fmt.Errorf("GC frame metadata has no safepoints or callsites")
	}
	rootMap.callsites = make([]compiledGCFrameCallsite, callCount)
	for i := range rootMap.callsites {
		rootMap.callsites[i].returnOffset, err = r.u32()
		if err != nil {
			return nil, err
		}
		rootMap.callsites[i].frameBytes, err = r.u32()
		if err != nil {
			return nil, err
		}
		rootMap.callsites[i].stackAdjust, err = r.u32()
		if err != nil {
			return nil, err
		}
		count, err := r.countElements("GC callsite root offsets", 4)
		if err != nil {
			return nil, err
		}
		rootMap.callsites[i].offsets = make([]uint32, count)
		for j := range rootMap.callsites[i].offsets {
			rootMap.callsites[i].offsets[j], err = r.u32()
			if err != nil {
				return nil, err
			}
		}
		rootMap.callsites[i].offsets = offsetInterner.intern(rootMap.callsites[i].offsets, false)
	}
	return rootMap, nil
}
