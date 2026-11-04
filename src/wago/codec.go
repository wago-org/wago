package wago

// This internal encoder binds source-compiled RV64 code to its metadata for
// mutation detection. It has no file framing, loader, or public serialization API.
import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const (
	compiledStartFunction         uint64 = 1 << 29
	compiledCore1SegmentPreflight uint64 = 1 << 31

	// Internal CPU/execution bits share the persisted u64 requirement word but
	// are stripped before exposing CoreFeatures. Public feature bits occupy the
	// low range; reserving the top fifteen bits avoids growing artifacts.
	// Bits 36-48 are architecture-scoped to attest exact RV64 table
	// instruction profiles. The size witness also covers the exact indexed
	// two-table32 slice. Bits 36-42 overlap AMD64 CPU features in the shared word, but
	// the artifact architecture header disambiguates it and RV64 encoding rejects
	// any CPU-requirement alias. Bit 50
	// originally witnessed ref.as_non_null. Branches share its retained
	// internal-carrier ABI; old non-null artifacts keep the same bit and layout.
	compiledRV64FunctionEnvironmentFrames  uint64 = 1 << 33
	compiledRV64OpaqueExternFrames         uint64 = 1 << 34
	compiledRV64OwnedExceptionReferences   uint64 = 1 << 35
	compiledRV64Table64ImportedInitDropABI uint64 = 1 << 36
	compiledRV64Table64ImportedCopyABI     uint64 = 1 << 37
	compiledRV64Table64ImportedFillABI     uint64 = 1 << 38
	compiledRV64Table64ImportedGrowABI     uint64 = 1 << 39
	compiledRV64Table64ImportedGetSetABI   uint64 = 1 << 40
	compiledRV64Table64ImportedSizeABI     uint64 = 1 << 41
	compiledRV64Table64InitDropABI         uint64 = 1 << 42
	compiledRV64Table64CopyABI             uint64 = 1 << 43
	compiledRV64Table64FillABI             uint64 = 1 << 44
	compiledRV64Table64IndirectABI         uint64 = 1 << 45
	compiledRV64Table64GetSetABI           uint64 = 1 << 46
	compiledRV64Table64GrowABI             uint64 = 1 << 47
	compiledRV64Table64SizeABI             uint64 = 1 << 48
	compiledRV64ReferenceFunctionABI       uint64 = 1 << 49
	compiledRV64ReferenceControlABI        uint64 = 1 << 50
	compiledRV64ReferenceCallABI           uint64 = 1 << 51
	compiledRV64TableReferenceABI          uint64 = 1 << 52
	compiledRV64TableContextABI            uint64 = 1 << 53
	compiledRV64ContextTrapABI             uint64 = 1 << 54
	compiledGCExecutionI31Product          uint64 = 1 << 55
	compiledFuncRefContextHeader           uint64 = 1 << 56
	compiledDynamicFuncrefEscape           uint64 = 1 << 57
	compiledRegisterABIDisabled            uint64 = 1 << 58
	compiledAtomicWaitExecution            uint64 = 1 << 59
	compiledNotifyPendingExecution         uint64 = 1 << 60
	compiledCPUFeatureBMI2                 uint64 = uint64(shared.AMD64BMI2) << 32
	compiledCPUFeatureLZCNT                uint64 = uint64(shared.AMD64LZCNT) << 32
	compiledCPUFeatureTZCNT                uint64 = uint64(shared.AMD64BMI1) << 32
	compiledCPUFeaturePOPCNT               uint64 = uint64(shared.AMD64POPCNT) << 32
	compiledCPUFeatureBitCount                    = compiledCPUFeatureLZCNT | compiledCPUFeatureTZCNT | compiledCPUFeaturePOPCNT
	compiledGCExecutionDynamicFuncRefTest  uint64 = 1 << 61
	compiledGCExecutionGenericStruct       uint64 = 1 << 62
	compiledGCExecutionGenericArray        uint64 = 1 << 63
	compiledCPUFeatureARM64LSE             uint64 = 1 << 52
	compiledCPUFeatures                    uint64 = 0xfffff << 32
	compiledGCExecutionMask                       = compiledGCExecutionI31Product | compiledGCExecutionDynamicFuncRefTest | compiledGCExecutionGenericStruct | compiledGCExecutionGenericArray
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

func compiledMetadataBytes(c *Compiled, checkRoots bool) ([]byte, error) {
	if err := c.validateCompactData(); err != nil {
		return nil, err
	}
	// CPU requirements must never alias the architecture execution markers.
	if c.nativeTable64ABI > nativeTable64ImportedInitDropABI {
		return nil, fmt.Errorf("unknown RV64 table64 compiler witness")
	}
	if uint64(c.requiredAMD64Features)&^(compiledCPUFeatures>>32) != 0 {
		return nil, fmt.Errorf("AMD64 CPU requirements exceed the encoded feature field")
	}
	if compiledArchitecture == 4 && uint64(c.requiredAMD64Features)&((compiledRV64OwnedExceptionReferences|compiledRV64Table64ImportedInitDropABI|compiledRV64Table64ImportedCopyABI|compiledRV64Table64ImportedFillABI|compiledRV64Table64ImportedGrowABI|compiledRV64Table64ImportedGetSetABI|compiledRV64Table64ImportedSizeABI|compiledRV64Table64InitDropABI|compiledRV64Table64CopyABI|compiledRV64Table64FillABI|compiledRV64Table64IndirectABI|compiledRV64Table64GetSetABI|compiledRV64Table64GrowABI|compiledRV64Table64SizeABI|compiledRV64ReferenceFunctionABI|compiledRV64ReferenceControlABI|compiledRV64ReferenceCallABI)>>32) != 0 {
		return nil, fmt.Errorf("AMD64 CPU requirement aliases an RV64 execution marker")
	}
	w := compiledWriter{buf: make([]byte, 0, 256)}
	w.intSlice(c.Entry)
	w.internalEntrySlice(c.InternalEntry)
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
	if err := w.typeDescriptors(c.Types); err != nil {
		return nil, err
	}
	if err := validateValueTypeDescriptors(c.Types, c.ValueTypes); err != nil {
		return nil, err
	}
	w.valueTypes(c.ValueTypes)
	if err := w.funcSigs(c.importFuncSigs, c.Types); err != nil {
		return nil, err
	}
	if err := w.funcSigs(c.Funcs, c.Types); err != nil {
		return nil, err
	}
	w.stringIntMap(c.Exports)
	w.nameSec(c.Names)
	if err := w.globalImports(c.GlobalImports, c); err != nil {
		return nil, err
	}
	if err := w.globals(c.Globals, c); err != nil {
		return nil, err
	}
	w.stringIntMap(c.GlobalExports)
	if err := w.tables(c); err != nil {
		return nil, err
	}
	w.stringIntMap(c.tableExports)
	w.u64Slice(c.FuncTypeID)
	w.bool(c.NeedsFuncRefDescs)
	if err := w.elems(c.Elems, c); err != nil {
		return nil, err
	}
	if err := w.elems(c.passiveElems, c); err != nil {
		return nil, err
	}
	w.compiledData(c)
	w.passiveData(c.PassiveData)
	w.memories(c)
	w.stringIntMap(c.memoryExportMap())
	w.bool(c.dynamicImports)
	w.tags(c)
	required := uint64(compiledStructuralRequiredFeatures(c))
	if c.needsOwnedExceptionReferences() {
		required |= compiledRV64OwnedExceptionReferences
	}
	if c.core1SegmentPreflight {
		required |= compiledCore1SegmentPreflight
	}
	if c.nativeReferenceFunctionABI {
		required |= compiledRV64ReferenceFunctionABI
	}
	if c.nativeReferenceControlABI {
		required |= compiledRV64ReferenceControlABI
	}
	if c.nativeReferenceCallABI {
		required |= compiledRV64ReferenceCallABI
	}
	if c.nativeTableReferenceABI {
		required |= compiledRV64TableReferenceABI
	}
	if c.nativeTableContextABI {
		required |= compiledRV64TableContextABI
	}
	if c.nativeContextTrapABI {
		required |= compiledRV64ContextTrapABI
	}
	if c.nativeTable64ABI == nativeTable64SizeABI {
		if compiledArchitecture != 4 {
			return nil, fmt.Errorf("RV64 table.size compiler witness on a foreign architecture")
		}
		required |= compiledRV64Table64SizeABI
	}
	if c.nativeTable64ABI == nativeTable64GrowABI {
		if compiledArchitecture != 4 {
			return nil, fmt.Errorf("RV64 table64.grow compiler witness on a foreign architecture")
		}
		required |= compiledRV64Table64GrowABI
	}
	if c.nativeTable64ABI == nativeTable64GetSetABI {
		if compiledArchitecture != 4 {
			return nil, fmt.Errorf("RV64 table64.get/set compiler witness on a foreign architecture")
		}
		required |= compiledRV64Table64GetSetABI
	}
	if c.nativeTable64ABI == nativeTable64IndirectABI {
		if compiledArchitecture != 4 {
			return nil, fmt.Errorf("RV64 table64 indirect-call compiler witness on a foreign architecture")
		}
		required |= compiledRV64Table64IndirectABI
	}
	if c.nativeTable64ABI == nativeTable64FillABI {
		if compiledArchitecture != 4 {
			return nil, fmt.Errorf("RV64 table64.fill compiler witness on a foreign architecture")
		}
		required |= compiledRV64Table64FillABI
	}
	if c.nativeTable64ABI == nativeTable64CopyABI {
		if compiledArchitecture != 4 {
			return nil, fmt.Errorf("RV64 table64.copy compiler witness on a foreign architecture")
		}
		required |= compiledRV64Table64CopyABI
	}
	if c.nativeTable64ABI == nativeTable64InitDropABI {
		if compiledArchitecture != 4 {
			return nil, fmt.Errorf("RV64 table64.init/drop compiler witness on a foreign architecture")
		}
		required |= compiledRV64Table64InitDropABI
	}
	if c.nativeTable64ABI == nativeTable64ImportedSizeABI {
		if compiledArchitecture != 4 {
			return nil, fmt.Errorf("RV64 imported table64.size compiler witness on a foreign architecture")
		}
		required |= compiledRV64Table64ImportedSizeABI
	}
	if c.nativeTable64ABI == nativeTable64ImportedGetSetABI {
		if compiledArchitecture != 4 {
			return nil, fmt.Errorf("RV64 imported table64.get/set compiler witness on a foreign architecture")
		}
		required |= compiledRV64Table64ImportedGetSetABI
	}
	if c.nativeTable64ABI == nativeTable64ImportedGrowABI {
		if compiledArchitecture != 4 {
			return nil, fmt.Errorf("RV64 imported table64.grow compiler witness on a foreign architecture")
		}
		required |= compiledRV64Table64ImportedGrowABI
	}
	if c.nativeTable64ABI == nativeTable64ImportedFillABI {
		if compiledArchitecture != 4 {
			return nil, fmt.Errorf("RV64 imported table64.fill compiler witness on a foreign architecture")
		}
		required |= compiledRV64Table64ImportedFillABI
	}
	if c.nativeTable64ABI == nativeTable64ImportedCopyABI {
		if compiledArchitecture != 4 {
			return nil, fmt.Errorf("RV64 imported table64.copy compiler witness on a foreign architecture")
		}
		required |= compiledRV64Table64ImportedCopyABI
	}
	if c.nativeTable64ABI == nativeTable64ImportedInitDropABI {
		if compiledArchitecture != 4 {
			return nil, fmt.Errorf("RV64 imported table64.init/drop compiler witness on a foreign architecture")
		}
		required |= compiledRV64Table64ImportedInitDropABI
	}
	if c.stagedGCI31Product() != 0 {
		required |= compiledGCExecutionI31Product
	}
	if c.stagedGCStructProduct() == stagedGCStructGeneric || rv64OfficialGCRefEq(c) || rv64GCRefTestTable(c) || rv64GCRefTestConcrete(c) || rv64GCRefCastConcrete(c) || rv64GCBrOnCastConcrete(c) || rv64GCBrOnCastFailConcrete(c) || rv64GCBrOnCastNullability(c) || rv64GCBrOnCastFailNullability(c) || rv64GCRefTestAbstract(c) || rv64GCExtern(c) || rv64GCRefCastAbstract(c) || rv64GCBrOnCastAbstract(c) || rv64GCBrOnCastFailAbstract(c) || rv64GCStructGet(c) || rv64GCStructMutation(c) || rv64GCStructNull(c) || rv64GCStructPacked(c) || rv64GCStructBasic(c) {
		required |= compiledGCExecutionGenericStruct
	}
	if c.stagedGCArrayProduct() == stagedGCArrayProductGeneric || rv64OfficialGCRefEq(c) || rv64GCRefTestAbstract(c) || rv64GCExtern(c) || rv64GCRefCastAbstract(c) || rv64GCBrOnCastAbstract(c) || rv64GCBrOnCastFailAbstract(c) || rv64GCArrayLocal(c) || rv64GCArrayUniform(c) || rv64GCArrayFixedLocal(c) || rv64GCArrayFixedOfficial(c) || rv64GCArrayDefaultOfficial(c) || rv64GCArrayPackedData(c) || rv64GCArrayReference(c) || rv64GCArrayInitElem(c) || rv64GCArrayInitData(c) || rv64GCArrayFill(c) || rv64GCArrayCopy(c) {
		required |= compiledGCExecutionGenericArray
	}
	if c.usesDynamicFuncRefTest() {
		required |= compiledGCExecutionDynamicFuncRefTest
	}
	if c.usesAtomicWaitHelpers() {
		required |= compiledAtomicWaitExecution
	}
	if c.requiresNotifyPending {
		required |= compiledNotifyPendingExecution
	}
	if c.requiresARM64LSE {
		required |= compiledCPUFeatureARM64LSE
	}
	required |= uint64(c.requiredAMD64Features) << 32
	if c.needsFuncRefContextHeader {
		required |= compiledFuncRefContextHeader
	}
	if c.dynamicFuncrefEscape {
		required |= compiledDynamicFuncrefEscape
	}
	if c.registerABIDisabled {
		required |= compiledRegisterABIDisabled
	}
	var startIndex uint32
	if c.HasStart {
		var err error
		startIndex, err = compiledStartIndex(c)
		if err != nil {
			return nil, err
		}
		required |= compiledStartFunction
	}
	if c.hasOpaqueExternFrames() {
		required |= compiledRV64OpaqueExternFrames
	}
	if c.hasFunctionEnvironmentFrames() {
		required |= compiledRV64FunctionEnvironmentFrames
	}
	w.u64(required)
	if c.HasStart {
		w.u32(startIndex)
	}
	if err := railshotWriteNativeTail(&w, c); err != nil {
		return nil, err
	}
	if required&(compiledGCExecutionGenericStruct|compiledGCExecutionGenericArray) != 0 || c.hasCollectorReferenceCallBoundary() || c.needsOwnedExceptionReferences() || c.hasInternalCollectorReferenceFunctions() || c.hasOpaqueExternFrames() || c.hasFunctionEnvironmentFrames() {
		w.u32(c.nativeGCABIRequirement())
	}
	w.gcTypeDescs(c.GCTypeDescs)
	if checkRoots {
		if err := validateCompiledGCFrameRoots(c, c.genericGCFrameRoots()); err != nil {
			return nil, err
		}
	}
	if rootMap := c.genericGCFrameRoots(); rootMap != nil {
		w.gcFrameRoots(rootMap)
	}
	return w.buf, nil
}

type compiledWriter struct {
	buf []byte
	tmp [binary.MaxVarintLen64]byte
}

func (w *compiledWriter) appendBytes(p []byte)  { w.buf = append(w.buf, p...) }
func (w *compiledWriter) appendString(s string) { w.buf = append(w.buf, s...) }
func (w *compiledWriter) u8(v byte) {
	w.tmp[0] = v
	w.appendBytes(w.tmp[:1])
}

// Share scalar metadata encoding for mutation fingerprints, with buffer
// appends inline inside each encoder.
//
//go:noinline
func (w *compiledWriter) bool(v bool) {
	if v {
		w.u8(1)
	} else {
		w.u8(0)
	}
}

//go:noinline
func (w *compiledWriter) uvar(v uint64) {
	n := binary.PutUvarint(w.tmp[:], v)
	w.appendBytes(w.tmp[:n])
}
func (w *compiledWriter) ivar(v int) {
	n := binary.PutVarint(w.tmp[:], int64(v))
	w.appendBytes(w.tmp[:n])
}

//go:noinline
func (w *compiledWriter) u32(v uint32) {
	binary.LittleEndian.PutUint32(w.tmp[:4], v)
	w.appendBytes(w.tmp[:4])
}

//go:noinline
func (w *compiledWriter) u64(v uint64) {
	binary.LittleEndian.PutUint64(w.tmp[:8], v)
	w.appendBytes(w.tmp[:8])
}
func (w *compiledWriter) bytes(b []byte) {
	w.uvar(uint64(len(b)))
	w.appendBytes(b)
}

func (w *compiledWriter) str(s string) {
	w.uvar(uint64(len(s)))
	w.appendString(s)
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
	w.uvar(uint64(len(m)))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
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
		if err := validateFuncSignature(sig, types); err != nil {
			return fmt.Errorf("function signature %d: %w", i, err)
		}
		w.bool(sig.HasTypeIndex)
		if sig.HasTypeIndex {
			w.u32(sig.TypeIndex)
			continue
		}
		for _, values := range [][]ValType{sig.Params, sig.Results} {
			w.uvar(uint64(len(values)))
			for _, value := range values {
				descriptor, _ := valueTypeDescriptorFromValType(value) // validated above
				w.valueType(descriptor)
			}
		}
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
			case value.RepeatPrevious:
				w.u8(5)
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
func (w *compiledWriter) compiledData(c *Compiled) {
	if c.compactData == nil {
		w.data(c.Data)
		return
	}
	w.uvar(uint64(c.activeDataCount()))
	for i := 0; i < c.activeDataCount(); i++ {
		d := c.activeDataAt(i)
		w.u32(d.MemoryIndex)
		w.offset(d.Offset)
		w.bytes(d.Bytes)
	}
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
				w.appendBytes(g.V128[:])
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
