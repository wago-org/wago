//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var (
	rv64GCBrOnCastNullabilityCodeDigest     = [sha256.Size]byte{0x73, 0xee, 0x12, 0x85, 0x73, 0x5d, 0x9f, 0x41, 0x41, 0x6e, 0xff, 0x0f, 0x1b, 0x1b, 0xfd, 0x79, 0xe6, 0xf3, 0x02, 0x93, 0x6a, 0xd0, 0x29, 0xbe, 0xb6, 0x22, 0xc6, 0x80, 0xc2, 0xfb, 0xd2, 0x4d}
	rv64GCBrOnCastFailNullabilityCodeDigest = [sha256.Size]byte{0x1d, 0x48, 0x18, 0x0e, 0x3b, 0xe3, 0xe2, 0xb0, 0x3f, 0x79, 0xcb, 0x9e, 0x82, 0xa2, 0xb1, 0xde, 0x5b, 0x2c, 0x30, 0x43, 0xdf, 0xf5, 0x27, 0x99, 0xa4, 0xea, 0x54, 0x2e, 0x38, 0x9a, 0x08, 0x53}
)

func rv64GCBrOnCastNullabilityExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 2636 && sha256.Sum256(c.code) == rv64GCBrOnCastNullabilityCodeDigest
}

func rv64GCBrOnCastFailNullabilityExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 2460 && sha256.Sum256(c.code) == rv64GCBrOnCastFailNullabilityCodeDigest
}

func rv64GCBrOnCastNullability(c *Compiled) bool {
	return rv64GCBrOnCastNullabilityProduct(c, false)
}

func rv64GCBrOnCastFailNullability(c *Compiled) bool {
	return rv64GCBrOnCastNullabilityProduct(c, true)
}

func rv64GCBrOnCastNullabilityProduct(c *Compiled, fail bool) bool {
	codeOK, wantTypes, wantProduct := rv64GCBrOnCastNullabilityExecutionCode(c), 4, stagedGCStructBrOnCastNullability
	wantEntry := []int{0, 784, 1568}
	if fail {
		codeOK, wantTypes, wantProduct = rv64GCBrOnCastFailNullabilityExecutionCode(c), 3, stagedGCStructBrOnCastFailNullability
		wantEntry = []int{0, 720, 1440}
	}
	if c == nil || !codeOK || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || !slices.Equal(c.Entry, wantEntry) || !slices.Equal(c.InternalEntry, wantEntry) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 3 || len(c.FuncTypeID) != 3 || len(c.Types) != wantTypes || len(c.GCTypeDescs) != wantTypes || len(c.ValueTypes) != 0 || len(c.Exports) != 0 ||
		len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 ||
		c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 ||
		c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 ||
		d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	nonNullAny := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
	nullableAny := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
	nonNullT := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 0}}}
	nullableT := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 0}}}
	wantDefined := []DefinedTypeDescriptor{
		{RecGroup: 0, Final: true, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{}},
		{RecGroup: 1, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{nonNullAny}, Results: []ValueTypeDescriptor{nonNullT}},
		{RecGroup: 2, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{nullableAny}, Results: []ValueTypeDescriptor{nonNullT}},
		{RecGroup: 3, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{nullableAny}, Results: []ValueTypeDescriptor{nullableT}},
	}
	wantKeys := []uint64{128867140982450263, 15123330844342055272, 4421367316276785587}
	wantFuncTypes := []uint32{1, 2, 3}
	if fail {
		wantDefined = []DefinedTypeDescriptor{
			{RecGroup: 0, Final: true, Kind: CompositeTypeStruct, Fields: []FieldTypeDescriptor{}},
			{RecGroup: 1, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{nonNullAny}, Results: []ValueTypeDescriptor{nonNullAny}},
			{RecGroup: 2, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{nullableAny}, Results: []ValueTypeDescriptor{nullableAny}},
		}
		wantKeys = []uint64{13446880275171981969, 9344568337142825904, 9344568337142825904}
		wantFuncTypes = []uint32{1, 2, 2}
	}
	for i, want := range wantDefined {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 ||
			got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	for i, got := range c.Funcs {
		wantType := wantFuncTypes[i]
		if !slices.Equal(got.Params, []ValType{ValAnyRef}) || !slices.Equal(got.Results, []ValType{ValAnyRef}) || got.TypeIndex != wantType || !got.HasTypeIndex || got.unsafeCrossTail || c.FuncTypeID[i] != wantKeys[i] {
			return false
		}
	}
	wantDescs := make([]nativegc.TypeDesc, wantTypes)
	wantDescs[0] = nativegc.TypeDesc{ID: 0, Kind: corergc.KindStruct, Align: 1, Final: true}
	for i := 1; i < wantTypes; i++ {
		wantDescs[i] = nativegc.TypeDesc{ID: corergc.TypeID(i), Kind: corergc.KindFunc, Final: true}
	}
	for i, want := range wantDescs {
		got := c.GCTypeDescs[i]
		if got.ID != want.ID || got.Kind != want.Kind || !slices.Equal(got.Fields, want.Fields) || got.Elem != want.Elem || got.Size != want.Size || got.ElemSize != want.ElemSize || got.Align != want.Align || got.HasRefs != want.HasRefs || got.Final != want.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
			return false
		}
	}
	product := c.stagedGCStructProduct()
	return (product == wantProduct || product == stagedGCStructGeneric) && c.stagedGCArrayProduct() == 0 && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC &&
		c.usesGCStructHelpers() && !c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil && c.requiredFeatures == CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC && c.requiredAMD64Features == 0
}

func railshotRestoreGCBrOnCastNullabilityArtifact(c *Compiled) {
	if !rv64GCBrOnCastNullability(c) {
		return
	}
	c.ensureCodeCache()
	c.codeCache.gcStructProduct = stagedGCStructBrOnCastNullability
}

func railshotRestoreGCBrOnCastFailNullabilityArtifact(c *Compiled) {
	if !rv64GCBrOnCastFailNullability(c) {
		return
	}
	c.ensureCodeCache()
	c.codeCache.gcStructProduct = stagedGCStructBrOnCastFailNullability
}
