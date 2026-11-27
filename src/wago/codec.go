// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

// This internal encoder binds source-compiled RV64 code to its metadata for
// mutation detection. It has no file framing, loader, or public serialization API.
import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/wago-org/wago/internal/buildfeatures"
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

func compiledMetadataBytes(c *Compiled, checkRoots bool) ([]byte, error) {
	rootMap := c.genericGCFrameRoots()
	if err := validateMetadataProfile(c); err != nil {
		return nil, err
	}
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
	if functionCodeForImage {
		w.intSlice(c.Entry)
		w.internalEntrySlice(c.InternalEntry)
	} else {
		w.uvar(0) // public entries
		w.uvar(0) // internal entries
	}
	if hostFunctionImportsBuilt {
		w.functionImports(c)
	} else {
		w.uvar(0) // import count
		w.uvar(0) // import names
	}
	if err := w.typeDescriptors(c.Types); err != nil {
		return nil, err
	}
	if err := validateValueTypeDescriptors(c.Types, c.ValueTypes); err != nil {
		return nil, err
	}
	w.valueTypes(c.ValueTypes)
	if hostFunctionImportsBuilt {
		if err := w.funcSigs(c.importFuncSigs, c.Types); err != nil {
			return nil, err
		}
	} else {
		w.uvar(0) // imported signatures
	}
	if functionCodeForImage {
		if err := w.funcSigs(c.Funcs, c.Types); err != nil {
			return nil, err
		}
		w.stringIntMap(c.Exports)
	} else {
		w.uvar(0) // local signatures
		w.uvar(0) // function exports
	}
	// Skipped writers still emit the canonical empty section. The profile
	// check rejects metadata that would otherwise disappear from the digest.
	if nameSectionForImage {
		w.nameSec(c.Names)
	} else {
		w.bool(false)
	}
	if globalImportsForImage {
		if err := w.globalImports(c.GlobalImports, c); err != nil {
			return nil, err
		}
	} else {
		w.uvar(0)
	}
	if globalsForImage {
		if err := w.globals(c.Globals, c); err != nil {
			return nil, err
		}
		w.stringIntMap(c.GlobalExports)
	} else {
		w.uvar(0) // globals
		w.uvar(0) // global exports
	}
	if tablesForImage {
		if err := w.tables(c); err != nil {
			return nil, err
		}
		w.stringIntMap(c.tableExports)
	} else {
		w.uvar(0) // tables
		w.uvar(0) // table exports
	}
	if functionCodeForImage {
		w.u64Slice(c.FuncTypeID)
	} else {
		w.uvar(0) // function type IDs
	}
	w.bool(c.NeedsFuncRefDescs)
	if activeElementsForImage {
		if err := w.elems(c.Elems, c); err != nil {
			return nil, err
		}
	} else {
		w.uvar(0)
	}
	if passiveElementsForImage {
		if err := w.elems(c.passiveElems, c); err != nil {
			return nil, err
		}
	} else {
		w.uvar(0)
	}
	if activeDataForImage {
		if err := w.compiledData(c); err != nil {
			return nil, err
		}
	} else {
		w.uvar(0)
	}
	if passiveDataForImage {
		w.passiveData(c.PassiveData)
	} else {
		w.uvar(0)
	}
	if memoriesForImage {
		if err := w.memories(c); err != nil {
			return nil, err
		}
	} else {
		w.uvar(0)
	}
	w.bool(c.HasMemory)
	w.u32(c.MemMinPages)
	w.u32(c.MemMaxPages)
	if memoriesForImage {
		w.stringIntMap(c.memoryExportMap())
	} else {
		w.uvar(0) // memory exports
	}
	w.bool(c.dynamicImports)
	if !standaloneBareImage || exceptionTagsForImage {
		w.tags(c)
	} else {
		w.uvar(0)
		w.uvar(0)
	}
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
	if legacyTable32InstructionsForImage || legacyTable64ForImage {
		bits, err := compiledLegacyTableWitness(c.nativeTable64ABI)
		if err != nil {
			return nil, err
		}
		required |= bits
	}
	if i31ExecutionForImage && c.stagedGCI31Product() != 0 {
		required |= compiledGCExecutionI31Product
	}
	if wasmGCBuilt {
		if c.stagedGCStructProduct() == stagedGCStructGeneric || rv64LegacyGCProductsForImage && (rv64OfficialGCRefEq(c) || rv64GCRefTestTable(c) || rv64GCRefTestConcrete(c) || rv64GCRefCastConcrete(c) || rv64GCBrOnCastConcrete(c) || rv64GCBrOnCastFailConcrete(c) || rv64GCBrOnCastNullability(c) || rv64GCBrOnCastFailNullability(c) || rv64GCRefTestAbstract(c) || rv64GCExtern(c) || rv64GCRefCastAbstract(c) || rv64GCBrOnCastAbstract(c) || rv64GCBrOnCastFailAbstract(c) || rv64GCStructGet(c) || rv64GCStructMutation(c) || rv64GCStructNull(c) || rv64GCStructPacked(c) || rv64GCStructBasic(c)) {
			required |= compiledGCExecutionGenericStruct
		}
		if c.stagedGCArrayProduct() == stagedGCArrayProductGeneric || rv64LegacyGCProductsForImage && (rv64OfficialGCRefEq(c) || rv64GCRefTestAbstract(c) || rv64GCExtern(c) || rv64GCRefCastAbstract(c) || rv64GCBrOnCastAbstract(c) || rv64GCBrOnCastFailAbstract(c) || rv64GCArrayLocal(c) || rv64GCArrayUniform(c) || rv64GCArrayFixedLocal(c) || rv64GCArrayFixedOfficial(c) || rv64GCArrayDefaultOfficial(c) || rv64GCArrayPackedData(c) || rv64GCArrayReference(c) || rv64GCArrayInitElem(c) || rv64GCArrayInitData(c) || rv64GCArrayFill(c) || rv64GCArrayCopy(c)) {
			required |= compiledGCExecutionGenericArray
		}
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
	if required&(compiledGCExecutionGenericStruct|compiledGCExecutionGenericArray) != 0 || c.needsOwnedExceptionReferences() || c.hasOpaqueExternFrames() || c.hasFunctionEnvironmentFrames() ||
		nativeCollectorBuilt && (c.hasCollectorReferenceCallBoundary() || c.hasInternalCollectorReferenceFunctions()) {
		w.u32(c.nativeGCABIRequirement())
	}
	if detailedGCTypeDescsForImage {
		w.gcTypeDescs(c.GCTypeDescs)
	} else if err := w.gcFunctionTypeDescs(c.GCTypeDescs); err != nil {
		return nil, err
	}
	if nativeCollectorFrameRootsBuilt && rootMap != nil {
		if checkRoots {
			if err := validateCompiledGCFrameRoots(c, rootMap); err != nil {
				return nil, err
			}
		}
		w.gcFrameRoots(rootMap)
	}
	return w.buf, nil
}

// Operation-specific legacy table witnesses share the requirement word with
// architecture flags. Both legacy indexed Table32 and Table64 use these bits.
func compiledLegacyTableWitness(abi uint8) (uint64, error) {
	var bits uint64
	var operation string
	switch abi {
	case nativeTable64NoABI:
		return 0, nil
	case nativeTable64SizeABI:
		bits, operation = compiledRV64Table64SizeABI, "table.size"
	case nativeTable64GrowABI:
		bits, operation = compiledRV64Table64GrowABI, "table64.grow"
	case nativeTable64GetSetABI:
		bits, operation = compiledRV64Table64GetSetABI, "table64.get/set"
	case nativeTable64IndirectABI:
		bits, operation = compiledRV64Table64IndirectABI, "table64 indirect-call"
	case nativeTable64FillABI:
		bits, operation = compiledRV64Table64FillABI, "table64.fill"
	case nativeTable64CopyABI:
		bits, operation = compiledRV64Table64CopyABI, "table64.copy"
	case nativeTable64InitDropABI:
		bits, operation = compiledRV64Table64InitDropABI, "table64.init/drop"
	case nativeTable64ImportedSizeABI:
		bits, operation = compiledRV64Table64ImportedSizeABI, "imported table64.size"
	case nativeTable64ImportedGetSetABI:
		bits, operation = compiledRV64Table64ImportedGetSetABI, "imported table64.get/set"
	case nativeTable64ImportedGrowABI:
		bits, operation = compiledRV64Table64ImportedGrowABI, "imported table64.grow"
	case nativeTable64ImportedFillABI:
		bits, operation = compiledRV64Table64ImportedFillABI, "imported table64.fill"
	case nativeTable64ImportedCopyABI:
		bits, operation = compiledRV64Table64ImportedCopyABI, "imported table64.copy"
	case nativeTable64ImportedInitDropABI:
		bits, operation = compiledRV64Table64ImportedInitDropABI, "imported table64.init/drop"
	default:
		return 0, fmt.Errorf("unknown RV64 table64 compiler witness")
	}
	if compiledArchitecture != 4 {
		return 0, fmt.Errorf("RV64 %s compiler witness on a foreign architecture", operation)
	}
	return bits, nil
}

func validateMetadataProfile(c *Compiled) error {
	var section string
	switch {
	case !functionCodeForImage && (!c.hasNoFunctionCode() || len(c.Exports) != 0 || len(c.FuncTypeID) != 0):
		section = "function entries and declarations"
	case !hostFunctionImportsBuilt && (c.NumImports != 0 || len(c.Imports) != 0 || len(c.importFuncSigs) != 0):
		section = "function imports"
	case !legacyTable32InstructionsForImage && !legacyTable64ForImage && c.nativeTable64ABI != nativeTable64NoABI:
		section = "legacy table instruction witness"
	case !nameSectionForImage && c.Names != nil:
		section = "name section"
	case !globalImportsForImage && len(c.GlobalImports) != 0:
		section = "global imports"
	case !globalsForImage && (len(c.Globals) != 0 || len(c.GlobalExports) != 0):
		section = "global declarations"
	case !tablesForImage && (c.tableCount() != 0 || len(c.tableExports) != 0):
		section = "table declarations"
	case !activeElementsForImage && len(c.Elems) != 0:
		section = "active elements"
	case !passiveElementsForImage && len(c.passiveElems) != 0:
		section = "passive elements"
	case !activeDataForImage && (len(c.Data) != 0 || c.activeDataCount() != 0):
		section = "active data"
	case !passiveDataForImage && len(c.PassiveData) != 0:
		section = "passive data"
	case !memoriesForImage && (c.memoryCount() != 0 || len(c.memoryExportMap()) != 0):
		section = "memory declarations"
	case standaloneBareImage && !exceptionTagsForImage && c.memoryDir != nil && c.memoryDir.hasExceptionTagMetadata():
		section = "exception tags"
	default:
		return nil
	}
	return fmt.Errorf("compiled metadata %s exceed the standalone runtime profile", section)
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
func (w *compiledWriter) functionImports(c *Compiled) {
	w.uvar(uint64(c.NumImports))
	w.uvar(uint64(len(c.Imports)))
	for _, name := range c.Imports {
		w.str(name)
	}
	funcModuleEnds, _, _, _, exactImportNames := c.importModuleEndSections()
	for i := range c.Imports {
		moduleEnd := uint64(0)
		if exactImportNames {
			moduleEnd = funcModuleEnds[i]
		}
		w.uvar(moduleEnd)
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
func (w *compiledWriter) memories(c *Compiled) error {
	var memoryModuleEnds []uint64
	var exactImportNames bool
	if memoryImportsForImage {
		_, _, memoryModuleEnds, _, exactImportNames = c.importModuleEndSections()
	}
	count := c.memoryCount()
	w.uvar(uint64(count))
	for i := 0; i < count; i++ {
		def := c.memoryDef(i)
		if memoryImportsForImage {
			w.str(def.ImportKey)
			moduleEnd := uint64(0)
			if exactImportNames && def.ImportKey != "" {
				moduleEnd = memoryModuleEnds[i]
			}
			w.uvar(moduleEnd)
		} else {
			if def.ImportKey != "" {
				return fmt.Errorf("compiled metadata uses a memory import outside the standalone runtime profile")
			}
			w.uvar(0)
			w.uvar(0)
		}
		w.uvar(def.Min)
		w.uvar(def.Max)
		w.bool(def.HasMax)
		w.bool(def.Addr64)
		w.bool(def.Shared)
	}
	return nil
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
	if !referenceTypeDescriptorsForImage || t.Kind != ValueTypeReference {
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

func (w *compiledWriter) typeDescriptors(v []DefinedTypeDescriptor) error {
	if err := validateDefinedTypeDescriptors(v); err != nil {
		return err
	}
	w.uvar(uint64(len(v)))
	for _, d := range v {
		w.u32(d.RecGroup)
		w.bool(d.Final)
		if typeLinksForImage {
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
		} else {
			w.uvar(0)     // supertypes
			w.bool(false) // describes
			w.bool(false) // descriptor
		}
		w.u8(byte(d.Kind))
		switch d.Kind {
		case CompositeTypeFunction:
			w.valueTypes(d.Params)
			w.valueTypes(d.Results)
		case CompositeTypeStruct, CompositeTypeArray:
			if buildfeatures.GC != 0 && aggregateTypesForImage {
				w.aggregateTypeFields(&d)
			}
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
func (w *compiledWriter) offset(o OffsetInit) error {
	if !segmentGlobalOffsetsForImage && o.HasGlobal {
		return fmt.Errorf("compiled metadata uses a global segment offset outside the standalone runtime profile")
	}
	if !segmentOffsetExpressionsForImage && len(o.Expr) != 0 {
		return fmt.Errorf("compiled metadata uses a segment offset expression outside the standalone runtime profile")
	}
	w.u32(o.Base)
	w.bool(o.HasGlobal)
	w.ivar(o.Global)
	if segmentOffsetExpressionsForImage {
		w.bytes(o.Expr)
	} else {
		w.uvar(0)
	}
	return nil
}
func (w *compiledWriter) elems(v []ElemInit, c *Compiled) error {
	w.uvar(uint64(len(v)))
	for _, e := range v {
		w.u32(e.TableIndex)
		if err := w.valueTypeRef(normalizedElemRefType(e.RefType), e.HasValueType, e.ValueTypeIndex, c.ValueTypes, c.Types); err != nil {
			return err
		}
		w.u8(byte(e.Mode))
		if err := w.offset(e.Offset); err != nil {
			return err
		}
		w.uvar(uint64(len(e.Values)))
		for _, value := range e.Values {
			switch {
			case value.Null:
				w.u8(0)
			case value.RepeatPrevious:
				if !elementRepeatsForImage {
					return fmt.Errorf("compiled metadata uses an element repeat outside the standalone runtime profile")
				}
				w.u8(5)
			case len(value.Expr) != 0:
				if !elementExpressionsForImage {
					return fmt.Errorf("compiled metadata uses an element expression outside the standalone runtime profile")
				}
				w.u8(4)
				w.bytes(value.Expr)
			case value.HasGlobal:
				if !elementGlobalsForImage {
					return fmt.Errorf("compiled metadata uses an element global outside the standalone runtime profile")
				}
				if value.I31Wrap {
					w.u8(3)
				} else {
					w.u8(2)
				}
				w.u32(value.GlobalIndex)
			default:
				w.u8(1)
				w.u32(value.FuncIndex)
			}
		}
	}
	return nil
}
func (w *compiledWriter) compiledData(c *Compiled) error {
	w.uvar(uint64(c.activeDataCount()))
	for i := 0; i < c.activeDataCount(); i++ {
		d := c.activeDataAt(i)
		w.u32(d.MemoryIndex)
		if err := w.offset(d.Offset); err != nil {
			return err
		}
		w.bytes(d.Bytes)
	}
	return nil
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
			if !globalInitExpressionsForImage {
				return fmt.Errorf("compiled metadata uses a global initializer expression outside the standalone runtime profile")
			}
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
	var tableModuleEnds []uint64
	var exactImportNames bool
	if tableImportsForImage {
		_, tableModuleEnds, _, _, exactImportNames = c.importModuleEndSections()
	}
	w.uvar(uint64(count))
	for i := 0; i < count; i++ {
		def := c.tableDef(i)
		if err := w.valueTypeRef(c.tableElementType(i), def.HasValueType, def.ValueTypeIndex, c.ValueTypes, c.Types); err != nil {
			return err
		}
		w.bool(def.Addr64)
		if tableImportsForImage {
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
		} else if i == 0 && c.tableImport != "" || def.ImportKey != "" {
			return fmt.Errorf("compiled metadata uses a table import outside the standalone runtime profile")
		}
		w.u8(0)
		w.uvar(uint64(def.Size))
		w.uvar(def.Max)
		w.bool(def.HasMax)
		w.bool(def.HasInitFunc)
		if def.HasInitFunc {
			if !tableInitializersForImage {
				return fmt.Errorf("compiled metadata uses a table function initializer outside the standalone runtime profile")
			}
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

func gcTypeDescNeedsDetails(d gc.TypeDesc) bool {
	return d.Kind != gc.KindFunc || d.Fields != nil || d.Elem != 0 || d.Size != 0 ||
		d.ElemSize != 0 || d.Align != 0 || d.HasRefs || d.Super != 0 || d.HasSuper
}

// gcFunctionTypeDescs writes the existing wire format for function sentinels.
// The layout and supertype fields must all have their canonical empty values.
func (w *compiledWriter) gcFunctionTypeDescs(descs []gc.TypeDesc) error {
	// ID, kind, field presence/count, element kind, three u32 layout fields,
	// HasRefs, Final, supertype ID and HasSuper. Zero fields remain zero.
	const kindOffset = 4
	const finalOffset = kindOffset + 1 + 2 + 1 + 3*4 + 1
	var row [finalOffset + 1 + 4 + 1]byte
	row[kindOffset] = byte(gc.KindFunc)
	w.uvar(uint64(len(descs)))
	for i, d := range descs {
		if gcTypeDescNeedsDetails(d) {
			return fmt.Errorf("compiled metadata GC type descriptor %d exceeds the standalone runtime profile", i)
		}
		binary.LittleEndian.PutUint32(row[:kindOffset], uint32(d.ID))
		row[finalOffset] = 0
		if d.Final {
			row[finalOffset] = 1
		}
		w.appendBytes(row[:])
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
