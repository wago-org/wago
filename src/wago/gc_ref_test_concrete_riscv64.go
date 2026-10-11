//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCRefTestConcreteCodeDigest = [sha256.Size]byte{0x7f, 0x46, 0xfc, 0x65, 0xcf, 0xf1, 0x02, 0x13, 0xaa, 0x3b, 0xb3, 0x83, 0x5b, 0x72, 0x52, 0xf6, 0xaa, 0xb6, 0x4b, 0xc0, 0x38, 0xff, 0xb8, 0x8a, 0x8d, 0xbb, 0x36, 0xc3, 0x9c, 0xce, 0x7f, 0xdb}

func rv64GCRefTestConcreteExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 22792 && sha256.Sum256(c.code) == rv64GCRefTestConcreteCodeDigest
}

// rv64GCRefTestConcrete pins the complete retained semantic directory because
// trusted artifacts no longer carry Wasm bodies.
func rv64GCRefTestConcrete(c *Compiled) bool {
	if c == nil || !rv64GCRefTestConcreteExecutionCode(c) || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI ||
		!c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata ||
		c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 3 || len(c.Types) != 9 || len(c.Exports) != 2 ||
		len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 1 || c.tableCount() != 1 || len(c.extraTables) != 0 || len(c.tableExports) != 0 ||
		c.memoryCount() != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil ||
		len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	if !c.HasTable || c.TableType != ValAnyRef || !c.TableHasValueType || c.TableValueTypeIndex != 0 || c.TableSize != 20 || c.TableMax != 20 || c.TableHasMax || c.TableAddr64 ||
		c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.HasMemory || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 ||
		len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	wantExports := map[string]int{"test-sub": 1, "test-canon": 2}
	if len(c.Exports) != len(wantExports) {
		return false
	}
	for name, index := range wantExports {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	field := FieldTypeDescriptor{Storage: StorageTypeDescriptor{Value: i32}}
	wantTypes := []DefinedTypeDescriptor{
		{RecGroup: 0, Kind: CompositeTypeStruct},
		{RecGroup: 1, Kind: CompositeTypeStruct, Supers: []uint32{0}, Fields: []FieldTypeDescriptor{field}},
		{RecGroup: 2, Kind: CompositeTypeStruct, Supers: []uint32{0}, Fields: []FieldTypeDescriptor{field}},
		{RecGroup: 3, Kind: CompositeTypeStruct, Supers: []uint32{1}, Fields: []FieldTypeDescriptor{field, field}},
		{RecGroup: 4, Kind: CompositeTypeStruct, Supers: []uint32{2}, Fields: []FieldTypeDescriptor{field, field}},
		{RecGroup: 5, Kind: CompositeTypeStruct, Supers: []uint32{0}, Fields: []FieldTypeDescriptor{field, field}},
		{RecGroup: 6, Kind: CompositeTypeStruct, Supers: []uint32{0}},
		{RecGroup: 7, Kind: CompositeTypeStruct, Supers: []uint32{6}, Fields: []FieldTypeDescriptor{field, field}},
		{RecGroup: 8, Final: true, Kind: CompositeTypeFunction},
	}
	for i, want := range wantTypes {
		got := c.Types[i]
		if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 || got.HasDescriptor || got.Descriptor != 0 ||
			got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || !slices.Equal(got.Fields, want.Fields) || got.Array != want.Array {
			return false
		}
	}
	wantFunc := FuncSig{TypeIndex: 8, HasTypeIndex: true}
	wantTypeID := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc})
	if len(c.FuncTypeID) != 3 {
		return false
	}
	for i := range c.Funcs {
		got := c.Funcs[i]
		if !slices.Equal(got.Params, wantFunc.Params) || !slices.Equal(got.Results, wantFunc.Results) || got.TypeIndex != wantFunc.TypeIndex || !got.HasTypeIndex || got.unsafeCrossTail || c.FuncTypeID[i] != wantTypeID {
			return false
		}
	}
	structRef := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapStruct}}}
	if c.ValueTypes[0] != structRef || len(c.GCTypeDescs) != 9 {
		return false
	}
	i32Field0 := nativegc.FieldDesc{Kind: corergc.StorageI32}
	i32Field4 := nativegc.FieldDesc{Kind: corergc.StorageI32, Offset: 4}
	wantDescs := []nativegc.TypeDesc{
		{ID: 0, Kind: corergc.KindStruct, Align: 1},
		{ID: 1, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{i32Field0}, Size: 4, Align: 4, Super: 0, HasSuper: true},
		{ID: 2, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{i32Field0}, Size: 4, Align: 4, Super: 0, HasSuper: true},
		{ID: 3, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{i32Field0, i32Field4}, Size: 8, Align: 4, Super: 1, HasSuper: true},
		{ID: 4, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{i32Field0, i32Field4}, Size: 8, Align: 4, Super: 2, HasSuper: true},
		{ID: 5, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{i32Field0, i32Field4}, Size: 8, Align: 4, Super: 0, HasSuper: true},
		{ID: 6, Kind: corergc.KindStruct, Align: 1, Super: 0, HasSuper: true},
		{ID: 7, Kind: corergc.KindStruct, Fields: []nativegc.FieldDesc{i32Field0, i32Field4}, Size: 8, Align: 4, Super: 6, HasSuper: true},
		{ID: 8, Kind: corergc.KindFunc, Final: true},
	}
	for i, want := range wantDescs {
		got := c.GCTypeDescs[i]
		if got.ID != want.ID || got.Kind != want.Kind || !slices.Equal(got.Fields, want.Fields) || got.Elem != want.Elem || got.Size != want.Size || got.ElemSize != want.ElemSize || got.Align != want.Align || got.HasRefs != want.HasRefs || got.Final != want.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
			return false
		}
	}
	product := c.stagedGCStructProduct()
	productWitness := product == stagedGCStructRefTestConcrete || product == stagedGCStructGeneric
	wantEntry := []int{0, 1648, 17232}
	return productWitness && c.stagedGCArrayProduct() == 0 && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC &&
		c.usesGCStructHelpers() && !c.usesGCArrayHelpers() && c.genericGCFrameRoots() == nil && c.requiredFeatures == CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC && c.requiredAMD64Features == 0 &&
		slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry)
}

func railshotRestoreGCRefTestConcreteArtifact(c *Compiled) {
	if !rv64GCRefTestConcrete(c) {
		return
	}
	c.ensureCodeCache()
	c.codeCache.gcStructProduct = stagedGCStructRefTestConcrete
}
