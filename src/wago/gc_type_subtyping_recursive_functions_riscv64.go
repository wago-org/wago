//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64GCTypeSubtypingRecursiveFunctionCode = []struct {
	length int
	digest [sha256.Size]byte
}{
	{2140, [sha256.Size]byte{0xf8, 0x39, 0x33, 0xa0, 0xc8, 0xcb, 0xe1, 0x65, 0x70, 0xd8, 0xbd, 0x36, 0x15, 0x19, 0x8b, 0xef, 0x8a, 0xd0, 0xea, 0x15, 0x01, 0x65, 0xf6, 0x96, 0x3c, 0x20, 0x2e, 0xfc, 0xc2, 0x6c, 0x27, 0xa7}},
	{2084, [sha256.Size]byte{0x66, 0x38, 0x41, 0x7c, 0x82, 0x30, 0x02, 0x53, 0xa6, 0x08, 0xf7, 0x45, 0xfe, 0xe7, 0xbc, 0x8a, 0xdf, 0x88, 0x68, 0x74, 0xb3, 0xa4, 0x28, 0xec, 0xbb, 0xbc, 0x90, 0x46, 0x55, 0x2f, 0x2d, 0xf3}},
}

func rv64GCTypeSubtypingRecursiveFunctionsProfile(c *Compiled, allowUnrestored bool) uint8 {
	return rv64GCTypeSubtypingRecursiveFunctionsProfileWithCode(c, allowUnrestored, true)
}

func rv64GCTypeSubtypingRecursiveFunctionsProfileWithCode(c *Compiled, allowUnrestored, requireCode bool) uint8 {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled ||
		c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		!c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) || c.usesGCStructHelpers() || c.usesGCArrayHelpers() ||
		c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 3 || len(c.FuncTypeID) != 3 ||
		len(c.ValueTypes) != 0 || len(c.Exports) != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 ||
		c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 ||
		c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 ||
		c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.HasTable || c.HasMemory ||
		c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 ||
		c.stagedGCStructProduct() != 0 || c.stagedGCArrayProduct() != 0 || c.genericGCFrameRoots() != nil || c.nativeGCABIRequirement() != 0 {
		return 0
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 ||
		len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return 0
	}
	product := c.stagedGCTypeSubtypingProduct()
	if product != stagedGCTypeSubtypingRecursiveFunctions && (!allowUnrestored || product != 0) {
		return 0
	}
	staged := c.stagedFeatures()
	if staged != CoreFeatureTypedFunctionReferences|CoreFeatureGC && (!allowUnrestored || staged != 0) {
		return 0
	}
	if !slices.Equal(c.Entry, []int{0, 656, 1376}) || !slices.Equal(c.InternalEntry, c.Entry) {
		return 0
	}

	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	f32 := ValueTypeDescriptor{Kind: ValueTypeF32}
	refType := func(index uint32) ValueTypeDescriptor {
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: index}}}
	}
	profiles := []struct {
		types     []DefinedTypeDescriptor
		descs     []nativegc.TypeDesc
		funcTypes []uint32
		typeIDs   []uint64
		features  CoreFeatures
	}{
		{
			types: []DefinedTypeDescriptor{
				{RecGroup: 0, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, refType(2)}},
				{RecGroup: 0, Supers: []uint32{0}, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, refType(1)}},
				{RecGroup: 0, Supers: []uint32{1}, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{i32, refType(0)}},
				{RecGroup: 1, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{refType(0)}},
				{RecGroup: 2, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{refType(1)}},
				{RecGroup: 3, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{refType(2)}},
			},
			descs: []nativegc.TypeDesc{
				{ID: 0, Kind: corergc.KindFunc},
				{ID: 1, Kind: corergc.KindFunc, Super: 0, HasSuper: true},
				{ID: 2, Kind: corergc.KindFunc, Super: 1, HasSuper: true},
				{ID: 3, Kind: corergc.KindFunc, Final: true},
				{ID: 4, Kind: corergc.KindFunc, Final: true},
				{ID: 5, Kind: corergc.KindFunc, Final: true},
			},
			funcTypes: []uint32{3, 4, 5},
			typeIDs:   []uint64{6314554692585206006, 18324930624647618919, 10961341822235731289},
			features:  CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences | CoreFeatureGC,
		},
		{
			types: []DefinedTypeDescriptor{
				{RecGroup: 0, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{i32, refType(1)}},
				{RecGroup: 0, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{f32, refType(0)}},
				{RecGroup: 1, Supers: []uint32{0}, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{i32, refType(5)}},
				{RecGroup: 1, Supers: []uint32{1}, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{f32, refType(4)}},
				{RecGroup: 1, Supers: []uint32{0}, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{i32, refType(3)}},
				{RecGroup: 1, Supers: []uint32{1}, Kind: CompositeTypeFunction, Results: []ValueTypeDescriptor{f32, refType(2)}},
				{RecGroup: 2, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{refType(0)}},
				{RecGroup: 3, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{refType(2)}},
				{RecGroup: 4, Final: true, Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{refType(4)}},
			},
			descs: []nativegc.TypeDesc{
				{ID: 0, Kind: corergc.KindFunc},
				{ID: 1, Kind: corergc.KindFunc},
				{ID: 2, Kind: corergc.KindFunc, Super: 0, HasSuper: true},
				{ID: 3, Kind: corergc.KindFunc, Super: 1, HasSuper: true},
				{ID: 4, Kind: corergc.KindFunc, Super: 0, HasSuper: true},
				{ID: 5, Kind: corergc.KindFunc, Super: 1, HasSuper: true},
				{ID: 6, Kind: corergc.KindFunc, Final: true},
				{ID: 7, Kind: corergc.KindFunc, Final: true},
				{ID: 8, Kind: corergc.KindFunc, Final: true},
			},
			funcTypes: []uint32{6, 7, 8},
			typeIDs:   []uint64{600684411259324299, 18184688688654093362, 11016139991689737643},
			features:  CoreFeatureMultiValue | CoreFeatureReferenceTypes | CoreFeatureTypedFunctionReferences | CoreFeatureGC,
		},
	}
	for i, profile := range profiles {
		code := rv64GCTypeSubtypingRecursiveFunctionCode[i]
		if len(c.Types) != len(profile.types) || len(c.GCTypeDescs) != len(profile.descs) || c.requiredFeatures != profile.features || c.requiredAMD64Features != 0 ||
			requireCode && (len(c.code) != code.length || sha256.Sum256(c.code) != code.digest) || !slices.Equal(c.FuncTypeID, profile.typeIDs) {
			continue
		}
		matched := true
		for typeIndex, want := range profile.types {
			got := c.Types[typeIndex]
			if got.RecGroup != want.RecGroup || got.Final != want.Final || !slices.Equal(got.Supers, want.Supers) || got.HasDescribes || got.Describes != 0 ||
				got.HasDescriptor || got.Descriptor != 0 || got.Kind != want.Kind || !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) ||
				len(got.Fields) != 0 || got.Array != (FieldTypeDescriptor{}) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		for descIndex, want := range profile.descs {
			got := c.GCTypeDescs[descIndex]
			if got.ID != want.ID || got.Kind != want.Kind || len(got.Fields) != 0 || got.Elem != 0 || got.Size != 0 || got.ElemSize != 0 || got.Align != 0 ||
				got.HasRefs || got.Final != want.Final || got.Super != want.Super || got.HasSuper != want.HasSuper {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		for functionIndex, typeIndex := range profile.funcTypes {
			got := c.Funcs[functionIndex]
			if !slices.Equal(got.Params, []ValType{ValFuncRef}) || len(got.Results) != 0 || got.TypeIndex != typeIndex || !got.HasTypeIndex || got.unsafeCrossTail {
				matched = false
				break
			}
		}
		if matched {
			return uint8(i + 1)
		}
	}
	return 0
}

func rv64GCTypeSubtypingRecursiveFunctions(c *Compiled) bool {
	return rv64GCTypeSubtypingRecursiveFunctionsProfile(c, false) != 0
}

func rv64GCTypeSubtypingRecursiveFunctionsExecutionCode(c *Compiled) bool {
	if c == nil {
		return false
	}
	for _, code := range rv64GCTypeSubtypingRecursiveFunctionCode {
		if len(c.code) == code.length && sha256.Sum256(c.code) == code.digest {
			return true
		}
	}
	return false
}
