//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var rv64I31CoreCodeDigest = [sha256.Size]byte{0x09, 0x33, 0xe2, 0xad, 0x20, 0x62, 0xed, 0x4a, 0xfa, 0xc8, 0x39, 0x5c, 0x3b, 0xa2, 0x68, 0x32, 0xcd, 0xbc, 0x57, 0xb4, 0xaa, 0xc3, 0xdf, 0x37, 0x4e, 0xf4, 0x74, 0x4f, 0x96, 0x9f, 0x6b, 0x76}
var rv64I31OfficialCoreCodeDigest = [sha256.Size]byte{0x1a, 0x1f, 0xe4, 0x7e, 0x21, 0xfc, 0x84, 0x40, 0x83, 0x14, 0xbb, 0x42, 0x0b, 0xf4, 0x8e, 0xd0, 0x35, 0xff, 0x0b, 0x0b, 0x6a, 0x5c, 0xa1, 0x70, 0x1c, 0xf1, 0x2c, 0x21, 0xac, 0x78, 0xc6, 0x27}
var rv64I31OfficialTableCodeDigest = [sha256.Size]byte{0xf6, 0xf3, 0x62, 0x07, 0xa9, 0x73, 0xb1, 0x78, 0xfd, 0xad, 0xda, 0x46, 0xed, 0xe6, 0xce, 0x23, 0x94, 0xc7, 0x8a, 0x66, 0x58, 0xc7, 0x82, 0xf3, 0xd0, 0x24, 0x04, 0x7e, 0x37, 0xa9, 0x17, 0x1d}
var rv64I31OfficialTableGlobalInitializerCodeDigest = [sha256.Size]byte{0x85, 0x9f, 0xb4, 0x09, 0x53, 0xeb, 0xac, 0x16, 0x55, 0xf4, 0x66, 0xe7, 0x2f, 0xa9, 0xa8, 0x7a, 0x8c, 0x12, 0xf0, 0x28, 0xc7, 0x10, 0x6e, 0x69, 0x46, 0x7d, 0x8c, 0x73, 0x49, 0xc9, 0xcf, 0xb2}
var rv64I31OfficialGlobalInitializerCodeDigest = [sha256.Size]byte{0xaa, 0x0b, 0xe9, 0x11, 0x63, 0x9f, 0xa4, 0x57, 0x90, 0x15, 0x35, 0xfc, 0xb1, 0x6f, 0xd6, 0xde, 0x01, 0x08, 0x5d, 0x8e, 0x56, 0x1f, 0x5b, 0xee, 0x2d, 0x5b, 0x57, 0x0a, 0x20, 0x2e, 0xf6, 0xd5}
var rv64I31OfficialAnyGlobalCodeDigest = [sha256.Size]byte{0xe1, 0xcc, 0x81, 0x73, 0x72, 0x8a, 0xfe, 0xa4, 0xb3, 0x52, 0x20, 0x6c, 0xa0, 0x67, 0x3c, 0xf2, 0xa8, 0xca, 0xc3, 0x70, 0x7f, 0x57, 0x52, 0x6b, 0x87, 0xad, 0x22, 0xb3, 0x06, 0xc6, 0xfc, 0x2d}
var rv64I31OfficialAnyTableCodeDigest = [sha256.Size]byte{0x55, 0xb9, 0xc0, 0x28, 0x2a, 0x13, 0xbf, 0xc3, 0x59, 0xf9, 0xdf, 0x20, 0xf9, 0x6a, 0x2c, 0xb3, 0x77, 0x1d, 0x7a, 0xba, 0xb0, 0x65, 0xdd, 0x47, 0x47, 0x32, 0x36, 0x1a, 0x17, 0xad, 0xa6, 0x81}
var rv64I31OfficialRefTestCodeDigest = [sha256.Size]byte{0x26, 0xe0, 0x8e, 0xf3, 0x90, 0xe5, 0x32, 0x53, 0x68, 0x07, 0xef, 0x9e, 0x58, 0xa2, 0xd8, 0x07, 0xc5, 0xd2, 0x30, 0x88, 0x21, 0xc0, 0x47, 0x5a, 0x10, 0x09, 0xd6, 0x8d, 0x80, 0x56, 0xc5, 0x24}
var rv64I31RefEqPilotCodeDigest = [sha256.Size]byte{0x35, 0xa6, 0xd7, 0xcf, 0x47, 0xfe, 0xec, 0xb8, 0x28, 0x37, 0xbd, 0xfd, 0x50, 0x1c, 0xbd, 0x67, 0xa8, 0x73, 0xe2, 0xab, 0xb3, 0xc6, 0xd0, 0x10, 0xa9, 0xa1, 0xb0, 0xda, 0x3e, 0x33, 0xe9, 0x23}

// rv64I31Core is the retained-artifact counterpart of the nine exact source
// classifiers in the backend. Wasm bodies are not serialized, so callable
// visibility is bound to exact RV64G code images and complete surviving
// metadata.
func rv64I31Core(c *Compiled) bool {
	return rv64I31MinimalCore(c) || rv64I31OfficialCore(c) || rv64I31OfficialTable(c) || rv64I31OfficialTableGlobalInitializer(c) || rv64I31OfficialGlobalInitializer(c) || rv64I31OfficialAnyGlobal(c) || rv64I31OfficialAnyTable(c) || rv64I31OfficialRefTest(c) || rv64I31RefEqPilot(c)
}

// rv64I31CoreExecutionCode recognizes the nine qualified machine-code images
// without trusting the artifact's removable semantic sidecar. The loader uses
// it to reject an i31 image whose feature/product witness was stripped.
func rv64I31CoreExecutionCode(c *Compiled) bool {
	if c == nil {
		return false
	}
	switch len(c.code) {
	case 1432:
		return sha256.Sum256(c.code) == rv64I31CoreCodeDigest
	case 4872:
		return sha256.Sum256(c.code) == rv64I31OfficialCoreCodeDigest
	case 5776:
		return sha256.Sum256(c.code) == rv64I31OfficialTableCodeDigest
	case 840:
		return sha256.Sum256(c.code) == rv64I31OfficialTableGlobalInitializerCodeDigest
	case 688:
		return sha256.Sum256(c.code) == rv64I31OfficialGlobalInitializerCodeDigest
	case 1592:
		return sha256.Sum256(c.code) == rv64I31OfficialAnyGlobalCodeDigest
	case 5856:
		return sha256.Sum256(c.code) == rv64I31OfficialAnyTableCodeDigest
	case 5024:
		return sha256.Sum256(c.code) == rv64I31OfficialRefTestCodeDigest
	case 2436:
		return sha256.Sum256(c.code) == rv64I31RefEqPilotCodeDigest
	default:
		return false
	}
}

// rv64I31RefEqPilot is the retained counterpart of the exact collector-free
// eqref-table source. Artifact loading must recover the same compact table ABI,
// complete metadata, and machine image before callable publication.
func rv64I31RefEqPilot(c *Compiled) bool {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI ||
		!c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 2 || len(c.Types) != 2 || len(c.Exports) != 2 ||
		len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 1 || c.tableCount() != 1 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 ||
		c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	if !c.HasTable || c.TableType != ValAnyRef || !c.TableHasValueType || c.TableValueTypeIndex != 0 || c.TableSize != 5 || c.TableMax != 5 || c.TableHasMax || c.TableAddr64 || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.HasMemory || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	initIndex, hasInit := c.Exports["init"]
	eqIndex, hasEq := c.Exports["eq"]
	if !hasInit || initIndex != 0 || !hasEq || eqIndex != 1 {
		return false
	}
	wantFuncs := []FuncSig{
		{TypeIndex: 0, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValI32}, Results: []ValType{ValI32}, TypeIndex: 1, HasTypeIndex: true},
	}
	typeSigs := []*wasm.CompType{
		{Kind: wasm.CompFunc},
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}},
	}
	if len(c.FuncTypeID) != len(wantFuncs) {
		return false
	}
	for i, want := range wantFuncs {
		got := c.Funcs[i]
		if !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || got.TypeIndex != want.TypeIndex || !got.HasTypeIndex || got.unsafeCrossTail || c.FuncTypeID[i] != wasm.StructuralFuncTypeKey(typeSigs[i]) {
			return false
		}
		typ := c.Types[i]
		if typ.RecGroup != uint32(i) || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) || len(typ.Params) != len(typeSigs[i].Params) || len(typ.Results) != len(typeSigs[i].Results) {
			return false
		}
		for j, value := range typeSigs[i].Params {
			if !rv64I31ValueTypeDescriptor(typ.Params[j], value) {
				return false
			}
		}
		for j, value := range typeSigs[i].Results {
			if !rv64I31ValueTypeDescriptor(typ.Results[j], value) {
				return false
			}
		}
	}
	eqref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapEq}}}
	if c.ValueTypes[0] != eqref || len(c.GCTypeDescs) != len(typeSigs) {
		return false
	}
	for i, desc := range c.GCTypeDescs {
		if desc.ID != nativegc.TypeID(i) || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
			return false
		}
	}
	product, staged := c.stagedGCI31Product(), c.stagedFeatures()
	productWitness := product == stagedGCI31ProductRefEqPilot && staged == CoreFeatureTypedFunctionReferences|CoreFeatureGC || product == stagedGCI31ProductCore && staged == CoreFeatureGC
	wantEntry := []int{0, 1488}
	return c.requiredFeatures == CoreFeatureReferenceTypes|CoreFeatureGC && c.requiredAMD64Features == 0 && productWitness &&
		slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry) && len(c.code) == 2436 && sha256.Sum256(c.code) == rv64I31RefEqPilotCodeDigest
}

// rv64I31OfficialRefTest is the retained counterpart of the pinned
// collector-free ref.test product. The code image and every surviving semantic
// directory are exact because artifacts do not retain Wasm bodies.
func rv64I31OfficialRefTest(c *Compiled) bool {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI ||
		!c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	wantExports := map[string]int{"null-data": 0, "null-i31": 1, "i31": 2, "any": 3, "eq": 4, "struct": 5, "array": 6}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 7 || len(c.Types) != 2 || len(c.Exports) != len(wantExports) ||
		len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 0 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 ||
		c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	for name, index := range wantExports {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	if c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	typeSigs := []*wasm.CompType{
		{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}},
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}},
	}
	bindings := []int{0, 0, 1, 1, 1, 1, 1}
	if len(c.FuncTypeID) != len(bindings) {
		return false
	}
	for i, binding := range bindings {
		fn := c.Funcs[i]
		want := typeSigs[binding]
		if fn.TypeIndex != uint32(binding) || !fn.HasTypeIndex || fn.unsafeCrossTail || len(fn.Params) != len(want.Params) || len(fn.Results) != len(want.Results) || c.FuncTypeID[i] != wasm.StructuralFuncTypeKey(want) {
			return false
		}
		for j := range want.Params {
			if fn.Params[j] != ValI32 {
				return false
			}
		}
		for j := range want.Results {
			if fn.Results[j] != ValI32 {
				return false
			}
		}
	}
	for i, typ := range c.Types {
		want := typeSigs[i]
		if typ.RecGroup != uint32(i) || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) || len(typ.Params) != len(want.Params) || len(typ.Results) != len(want.Results) {
			return false
		}
		for j, value := range want.Params {
			if !rv64I31ValueTypeDescriptor(typ.Params[j], value) {
				return false
			}
		}
		for j, value := range want.Results {
			if !rv64I31ValueTypeDescriptor(typ.Results[j], value) {
				return false
			}
		}
	}
	if len(c.GCTypeDescs) != len(typeSigs) {
		return false
	}
	for i, desc := range c.GCTypeDescs {
		if desc.ID != nativegc.TypeID(i) || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
			return false
		}
	}
	product, staged := c.stagedGCI31Product(), c.stagedFeatures()
	productWitness := product == stagedGCI31ProductRefTest && staged == CoreFeatureTypedFunctionReferences|CoreFeatureGC || product == stagedGCI31ProductCore && staged == CoreFeatureGC
	wantEntry := []int{0, 688, 1376, 2112, 2848, 3584, 4304}
	return c.requiredFeatures == CoreFeatureReferenceTypes|CoreFeatureGC && c.requiredAMD64Features == 0 && productWitness &&
		slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry) && len(c.code) == 5024 && sha256.Sum256(c.code) == rv64I31OfficialRefTestCodeDigest
}

func rv64I31OfficialAnyGlobal(c *Compiled) bool {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI ||
		!c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	wantExports := map[string]int{"get_globals": 0, "set_global": 1}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 2 || len(c.Types) != 2 || len(c.Exports) != len(wantExports) ||
		len(c.Globals) != 2 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 ||
		c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	for name, index := range wantExports {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	if c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	wantFuncs := []FuncSig{
		{Results: []ValType{ValI32, ValI32}, TypeIndex: 0, HasTypeIndex: true},
		{Params: []ValType{ValI32}, TypeIndex: 1, HasTypeIndex: true},
	}
	typeSigs := []*wasm.CompType{
		{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32, wasm.I32}},
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}},
	}
	if len(c.FuncTypeID) != len(typeSigs) {
		return false
	}
	for i, want := range wantFuncs {
		got := c.Funcs[i]
		if !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || got.TypeIndex != want.TypeIndex || !got.HasTypeIndex || got.unsafeCrossTail || c.FuncTypeID[i] != wasm.StructuralFuncTypeKey(typeSigs[i]) {
			return false
		}
		typ := c.Types[i]
		if typ.RecGroup != uint32(i) || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) || len(typ.Params) != len(typeSigs[i].Params) || len(typ.Results) != len(typeSigs[i].Results) {
			return false
		}
		for j, value := range typeSigs[i].Params {
			if !rv64I31ValueTypeDescriptor(typ.Params[j], value) {
				return false
			}
		}
		for j, value := range typeSigs[i].Results {
			if !rv64I31ValueTypeDescriptor(typ.Results[j], value) {
				return false
			}
		}
	}
	anyref := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
	if c.ValueTypes[0] != anyref || len(c.GCTypeDescs) != 2 {
		return false
	}
	for i, desc := range c.GCTypeDescs {
		if desc.ID != nativegc.TypeID(i) || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
			return false
		}
	}
	wantBits := []uint64{2469, 11357}
	for i, global := range c.Globals {
		if global.Type != ValAnyRef || !global.HasValueType || global.ValueTypeIndex != 0 || global.Mutable != (i == 1) || global.Bits != wantBits[i] || global.V128 != (V128{}) || global.HasInitGlobal || global.InitGlobal != 0 || global.HasInitFunc || global.InitFunc != 0 || len(global.InitExpr) != 0 {
			return false
		}
	}
	product, staged := c.stagedGCI31Product(), c.stagedFeatures()
	productWitness := product == stagedGCI31ProductAnyGlobal || product == stagedGCI31ProductCore
	stagedWitness := staged == CoreFeatureGC || staged == CoreFeatureTypedFunctionReferences|CoreFeatureGC
	return c.requiredFeatures == CoreFeatureMultiValue|CoreFeatureReferenceTypes|CoreFeatureGC && c.requiredAMD64Features == 0 && productWitness && stagedWitness &&
		slices.Equal(c.Entry, []int{0, 960}) && slices.Equal(c.InternalEntry, []int{0, 960}) && len(c.code) == 1592 && sha256.Sum256(c.code) == rv64I31OfficialAnyGlobalCodeDigest
}

func rv64I31OfficialGlobalInitializer(c *Compiled) bool {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI ||
		!c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	get, hasGet := c.Exports["get"]
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.Types) != 1 || len(c.Exports) != 1 || !hasGet || get != 0 ||
		len(c.Globals) != 2 || len(c.GlobalImports) != 1 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 2 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 ||
		c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	if c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	fn := c.Funcs[0]
	if len(fn.Params) != 0 || !slices.Equal(fn.Results, []ValType{ValI32}) || fn.TypeIndex != 0 || !fn.HasTypeIndex || fn.unsafeCrossTail {
		return false
	}
	typeSig := &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}
	if len(c.FuncTypeID) != 1 || c.FuncTypeID[0] != wasm.StructuralFuncTypeKey(typeSig) {
		return false
	}
	typ := c.Types[0]
	if typ.RecGroup != 0 || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Params) != 0 || len(typ.Results) != 1 || !rv64I31ValueTypeDescriptor(typ.Results[0], wasm.I32) || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
		return false
	}
	if !rv64I31ValueTypeDescriptor(c.ValueTypes[0], wasm.I32) || !rv64I31ValueTypeDescriptor(c.ValueTypes[1], wasm.I31Ref) || len(c.GCTypeDescs) != 1 {
		return false
	}
	desc := c.GCTypeDescs[0]
	if desc.ID != 0 || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
		return false
	}
	imported, local, im := c.Globals[0], c.Globals[1], c.GlobalImports[0]
	if imported.Type != ValI32 || !imported.HasValueType || imported.ValueTypeIndex != 0 || imported.Mutable || imported.Bits != 0 || imported.V128 != (V128{}) || imported.HasInitGlobal || imported.InitGlobal != 0 || imported.HasInitFunc || imported.InitFunc != 0 || len(imported.InitExpr) != 0 ||
		local.Type != ValI31Ref || !local.HasValueType || local.ValueTypeIndex != 1 || local.Mutable || local.Bits != 0 || local.V128 != (V128{}) || local.HasInitGlobal || local.InitGlobal != 0 || local.HasInitFunc || local.InitFunc != 0 || !slices.Equal(local.InitExpr, []byte{0x23, 0, 0xfb, 0x1c, 0x0b}) ||
		im.Module != "env" || im.Name != "g" || im.Type != ValI32 || !im.HasValueType || im.ValueTypeIndex != 0 || im.Mutable {
		return false
	}
	product, staged := c.stagedGCI31Product(), c.stagedFeatures()
	productWitness := product == stagedGCI31ProductGlobalInitializer || product == stagedGCI31ProductCore
	stagedWitness := staged == CoreFeatureGC || staged == CoreFeatureTypedFunctionReferences|CoreFeatureGC
	return c.requiredFeatures == CoreFeatureReferenceTypes|CoreFeatureGC && c.requiredAMD64Features == 0 && productWitness && stagedWitness &&
		slices.Equal(c.Entry, []int{0}) && slices.Equal(c.InternalEntry, []int{0}) && len(c.code) == 688 && sha256.Sum256(c.code) == rv64I31OfficialGlobalInitializerCodeDigest
}

func rv64I31OfficialTableGlobalInitializer(c *Compiled) bool {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI ||
		!c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	get, hasGet := c.Exports["get"]
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 1 || len(c.Types) != 1 || len(c.Exports) != 1 || !hasGet || get != 0 ||
		len(c.Globals) != 1 || len(c.GlobalImports) != 1 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 2 || c.tableCount() != 1 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 ||
		c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 1 || len(c.passiveElems) != 0 {
		return false
	}
	if !c.HasTable || c.TableType != ValI31Ref || !c.TableHasValueType || c.TableValueTypeIndex != 1 || c.TableSize != 3 || c.TableMax != 3 || !c.TableHasMax || c.TableAddr64 || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.HasMemory || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	d := c.memoryDir
	if d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	fn := c.Funcs[0]
	if !slices.Equal(fn.Params, []ValType{ValI32}) || !slices.Equal(fn.Results, []ValType{ValI32}) || fn.TypeIndex != 0 || !fn.HasTypeIndex || fn.unsafeCrossTail {
		return false
	}
	typeSig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	if len(c.FuncTypeID) != 1 || c.FuncTypeID[0] != wasm.StructuralFuncTypeKey(typeSig) {
		return false
	}
	typ := c.Types[0]
	if typ.RecGroup != 0 || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) ||
		len(typ.Params) != 1 || !rv64I31ValueTypeDescriptor(typ.Params[0], wasm.I32) || len(typ.Results) != 1 || !rv64I31ValueTypeDescriptor(typ.Results[0], wasm.I32) {
		return false
	}
	i31 := wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapI31), false))
	if !rv64I31ValueTypeDescriptor(c.ValueTypes[0], wasm.I32) || !rv64I31ValueTypeDescriptor(c.ValueTypes[1], i31) || len(c.GCTypeDescs) != 1 {
		return false
	}
	desc := c.GCTypeDescs[0]
	if desc.ID != 0 || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
		return false
	}
	global, im := c.Globals[0], c.GlobalImports[0]
	if global.Type != ValI32 || !global.HasValueType || global.ValueTypeIndex != 0 || global.Mutable || global.Bits != 0 || global.V128 != (V128{}) || global.HasInitGlobal || global.InitGlobal != 0 || global.HasInitFunc || global.InitFunc != 0 || len(global.InitExpr) != 0 ||
		im.Module != "env" || im.Name != "g" || im.Type != ValI32 || !im.HasValueType || im.ValueTypeIndex != 0 || im.Mutable {
		return false
	}
	elem := c.Elems[0]
	if elem.TableIndex != 0 || elem.RefType != ValI31Ref || !elem.HasValueType || elem.ValueTypeIndex != 1 || elem.Mode != ElemModeActive || elem.Offset.Base != 0 || elem.Offset.HasGlobal || elem.Offset.Global != 0 || len(elem.Offset.Expr) != 0 || len(elem.Values) != 3 {
		return false
	}
	wantExpr := []byte{0x23, 0, 0xfb, 0x1c, 0x0b}
	for _, value := range elem.Values {
		if value.FuncIndex != 0 || value.GlobalIndex != 0 || !slices.Equal(value.Expr, wantExpr) || value.Null || value.HasGlobal || value.I31Wrap || value.RepeatPrevious {
			return false
		}
	}
	product, staged := c.stagedGCI31Product(), c.stagedFeatures()
	productWitness := product == stagedGCI31ProductTableGlobalInitializer || product == stagedGCI31ProductCore
	return c.requiredFeatures == CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC && c.requiredAMD64Features == 0 && productWitness &&
		staged == CoreFeatureTypedFunctionReferences|CoreFeatureGC &&
		slices.Equal(c.Entry, []int{0}) && slices.Equal(c.InternalEntry, []int{0}) && len(c.code) == 840 && sha256.Sum256(c.code) == rv64I31OfficialTableGlobalInitializerCodeDigest
}

func rv64I31OfficialTable(c *Compiled) bool {
	return rv64I31OfficialTableShape(c, false)
}

func rv64I31OfficialAnyTable(c *Compiled) bool {
	return rv64I31OfficialTableShape(c, true)
}

func rv64I31OfficialTableShape(c *Compiled, anyref bool) bool {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI ||
		!c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	wantValueTypes := 1
	if anyref {
		wantValueTypes = 2
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 6 || len(c.Types) != 4 || len(c.Exports) != 6 ||
		len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != wantValueTypes || c.tableCount() != 1 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 ||
		c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 1 || len(c.passiveElems) != 2 {
		return false
	}
	wantTableType := ValI31Ref
	if anyref {
		wantTableType = ValAnyRef
	}
	if !c.HasTable || c.TableType != wantTableType || !c.TableHasValueType || c.TableValueTypeIndex != 0 || c.TableSize != 3 || c.TableMax != 10 || !c.TableHasMax || c.TableAddr64 || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.HasMemory || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	wantExports := map[string]int{"size": 0, "get": 1, "grow": 2, "fill": 3, "copy": 4, "init": 5}
	for name, index := range wantExports {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	wantFuncs := []FuncSig{
		{Results: []ValType{ValI32}, TypeIndex: 0, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 1, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValI32}, Results: []ValType{ValI32}, TypeIndex: 2, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValI32, ValI32}, TypeIndex: 3, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValI32, ValI32}, TypeIndex: 3, HasTypeIndex: true},
		{Params: []ValType{ValI32, ValI32, ValI32}, TypeIndex: 3, HasTypeIndex: true},
	}
	for i := range wantFuncs {
		got, want := c.Funcs[i], wantFuncs[i]
		if !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || got.TypeIndex != want.TypeIndex || got.HasTypeIndex != want.HasTypeIndex || got.unsafeCrossTail {
			return false
		}
	}
	typeSigs := []*wasm.CompType{
		{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}},
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}},
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}},
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}},
	}
	bindings := []int{0, 1, 2, 3, 3, 3}
	if len(c.FuncTypeID) != len(bindings) {
		return false
	}
	for i, binding := range bindings {
		if c.FuncTypeID[i] != wasm.StructuralFuncTypeKey(typeSigs[binding]) {
			return false
		}
	}
	for i, typ := range c.Types {
		if typ.RecGroup != uint32(i) || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
			return false
		}
		want := typeSigs[i]
		if len(typ.Params) != len(want.Params) || len(typ.Results) != len(want.Results) {
			return false
		}
		for j, value := range want.Params {
			if !rv64I31ValueTypeDescriptor(typ.Params[j], value) {
				return false
			}
		}
		for j, value := range want.Results {
			if !rv64I31ValueTypeDescriptor(typ.Results[j], value) {
				return false
			}
		}
	}
	i31ValueTypeIndex := uint32(0)
	if anyref {
		anyrefType := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
		if c.ValueTypes[0] != anyrefType {
			return false
		}
		i31ValueTypeIndex = 1
	}
	if !rv64I31ValueTypeDescriptor(c.ValueTypes[i31ValueTypeIndex], wasm.I31Ref) || len(c.GCTypeDescs) != len(typeSigs) {
		return false
	}
	for i, desc := range c.GCTypeDescs {
		if desc.ID != nativegc.TypeID(i) || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
			return false
		}
	}
	active := c.Elems[0]
	if !rv64I31TableElement(active, i31ValueTypeIndex, ElemModeActive, []uint32{1999, 1777, 1555}) || !rv64I31TableElement(c.passiveElems[0], i31ValueTypeIndex, ElemModeActive, nil) || !rv64I31TableElement(c.passiveElems[1], i31ValueTypeIndex, ElemModePassive, []uint32{247, 913, 1579}) {
		return false
	}
	wantEntry := []int{0, 608, 1456, 2384, 3360, 4608}
	wantCodeLen, wantCodeDigest, wantProduct := 5776, rv64I31OfficialTableCodeDigest, stagedGCI31ProductTable
	if anyref {
		wantEntry = []int{0, 608, 1536, 2464, 3440, 4688}
		wantCodeLen, wantCodeDigest, wantProduct = 5856, rv64I31OfficialAnyTableCodeDigest, stagedGCI31ProductAnyTable
	}
	product, staged := c.stagedGCI31Product(), c.stagedFeatures()
	productWitness := product == wantProduct && staged == CoreFeatureTypedFunctionReferences|CoreFeatureGC || product == stagedGCI31ProductCore && staged == CoreFeatureGC
	return c.requiredFeatures == CoreFeatureBulkMemoryOperations|CoreFeatureReferenceTypes|CoreFeatureGC && c.requiredAMD64Features == 0 && productWitness &&
		slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry) && len(c.code) == wantCodeLen && sha256.Sum256(c.code) == wantCodeDigest
}

func rv64I31TableElement(got ElemInit, valueTypeIndex uint32, mode ElemMode, values []uint32) bool {
	if got.TableIndex != 0 || got.RefType != ValI31Ref || !got.HasValueType || got.ValueTypeIndex != valueTypeIndex || got.Mode != mode || got.Offset.Base != 0 || got.Offset.HasGlobal || got.Offset.Global != 0 || len(got.Offset.Expr) != 0 || len(got.Values) != len(values) {
		return false
	}
	for i, value := range got.Values {
		if value.FuncIndex != values[i] || value.GlobalIndex != 0 || len(value.Expr) != 0 || value.Null || value.HasGlobal || value.I31Wrap || value.RepeatPrevious {
			return false
		}
	}
	return true
}

func rv64I31MinimalCore(c *Compiled) bool {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI ||
		!c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape ||
		c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 ||
		len(c.Funcs) != 2 || len(c.Types) != 1 || len(c.Exports) != 2 || c.Exports["signed"] != 0 || c.Exports["unsigned"] != 1 ||
		len(c.ValueTypes) != 0 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 ||
		c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 ||
		c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	if c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	for _, fn := range c.Funcs {
		if len(fn.Params) != 1 || fn.Params[0] != ValI32 || len(fn.Results) != 1 || fn.Results[0] != ValI32 || !fn.HasTypeIndex || fn.TypeIndex != 0 || fn.unsafeCrossTail {
			return false
		}
	}
	key := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}})
	if len(c.FuncTypeID) != 2 || c.FuncTypeID[0] != key || c.FuncTypeID[1] != key {
		return false
	}
	typ := c.Types[0]
	if typ.RecGroup != 0 || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction ||
		len(typ.Params) != 1 || typ.Params[0].Kind != ValueTypeI32 || len(typ.Results) != 1 || typ.Results[0].Kind != ValueTypeI32 || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
		return false
	}
	if len(c.GCTypeDescs) != 1 {
		return false
	}
	desc := c.GCTypeDescs[0]
	if desc.ID != 0 || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
		return false
	}
	staged := c.stagedFeatures()
	return c.requiredFeatures == CoreFeatureReferenceTypes|CoreFeatureGC && c.requiredAMD64Features == 0 && c.stagedGCI31Product() == stagedGCI31ProductCore && (staged == CoreFeatureGC || staged == CoreFeatureTypedFunctionReferences|CoreFeatureGC) &&
		len(c.Entry) == 2 && c.Entry[0] == 0 && c.Entry[1] == 720 && len(c.InternalEntry) == 2 && c.InternalEntry[0] == 0 && c.InternalEntry[1] == 720 &&
		len(c.code) == 1432 && sha256.Sum256(c.code) == rv64I31CoreCodeDigest
}

// rv64I31OfficialCore is the retained counterpart of the pinned 252-byte
// spec-v3 gc/i31 core module. Wasm bodies are absent from artifacts, so every
// surviving semantic directory and the complete RV64G image are pinned.
func rv64I31OfficialCore(c *Compiled) bool {
	if c == nil || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI ||
		!c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 7 || len(c.Types) != 5 || len(c.Exports) != 7 ||
		len(c.Globals) != 2 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || c.tagImportCount() != 0 ||
		c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 || c.Names != nil || len(c.customInstructions) != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	if c.HasTable || c.HasMemory || c.HasTableInitFunc || c.TableInitFunc != 0 || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	wantExports := map[string]int{"new": 0, "get_u": 1, "get_s": 2, "get_u-null": 3, "get_s-null": 4, "get_globals": 5, "set_global": 6}
	if len(c.Exports) != len(wantExports) {
		return false
	}
	for name, index := range wantExports {
		if c.Exports[name] != index {
			return false
		}
	}
	wantFuncs := []FuncSig{
		{Params: []ValType{ValI32}, Results: []ValType{ValI31Ref}, TypeIndex: 0, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 1, HasTypeIndex: true},
		{Params: []ValType{ValI32}, Results: []ValType{ValI32}, TypeIndex: 1, HasTypeIndex: true},
		{Results: []ValType{ValI32}, TypeIndex: 2, HasTypeIndex: true},
		{Results: []ValType{ValI32}, TypeIndex: 2, HasTypeIndex: true},
		{Results: []ValType{ValI32, ValI32}, TypeIndex: 3, HasTypeIndex: true},
		{Params: []ValType{ValI32}, TypeIndex: 4, HasTypeIndex: true},
	}
	for i := range wantFuncs {
		got, want := c.Funcs[i], wantFuncs[i]
		if !slices.Equal(got.Params, want.Params) || !slices.Equal(got.Results, want.Results) || got.TypeIndex != want.TypeIndex || got.HasTypeIndex != want.HasTypeIndex || got.unsafeCrossTail {
			return false
		}
	}
	i31 := wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapI31), false))
	typeSigs := []*wasm.CompType{
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{i31}},
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}},
		{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}},
		{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32, wasm.I32}},
		{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}},
	}
	bindings := []int{0, 1, 1, 2, 2, 3, 4}
	if len(c.FuncTypeID) != len(bindings) {
		return false
	}
	for i, binding := range bindings {
		if c.FuncTypeID[i] != wasm.StructuralFuncTypeKey(typeSigs[binding]) {
			return false
		}
	}
	for i, typ := range c.Types {
		if typ.RecGroup != uint32(i) || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
			return false
		}
		want := typeSigs[i]
		if len(typ.Params) != len(want.Params) || len(typ.Results) != len(want.Results) {
			return false
		}
		for j, value := range want.Params {
			if !rv64I31ValueTypeDescriptor(typ.Params[j], value) {
				return false
			}
		}
		for j, value := range want.Results {
			if !rv64I31ValueTypeDescriptor(typ.Results[j], value) {
				return false
			}
		}
	}
	if !rv64I31ValueTypeDescriptor(c.ValueTypes[0], i31) || len(c.GCTypeDescs) != len(typeSigs) {
		return false
	}
	for i, desc := range c.GCTypeDescs {
		if desc.ID != nativegc.TypeID(i) || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
			return false
		}
	}
	for i, global := range c.Globals {
		if global.Type != ValI31Ref || !global.HasValueType || global.ValueTypeIndex != 0 || global.Mutable != (i == 1) || global.Bits != uint64(5+2*i) || global.V128 != (V128{}) || global.HasInitGlobal || global.InitGlobal != 0 || global.HasInitFunc || global.InitFunc != 0 || len(global.InitExpr) != 0 {
			return false
		}
	}
	wantEntry := []int{0, 640, 1360, 2080, 2768, 3456, 4240}
	return c.requiredFeatures == CoreFeatureMultiValue|CoreFeatureReferenceTypes|CoreFeatureTypedFunctionReferences|CoreFeatureGC && c.requiredAMD64Features == 0 && c.stagedGCI31Product() == stagedGCI31ProductCore && c.stagedFeatures() == CoreFeatureTypedFunctionReferences|CoreFeatureGC &&
		slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry) && len(c.code) == 4872 && sha256.Sum256(c.code) == rv64I31OfficialCoreCodeDigest
}

func rv64I31ValueTypeDescriptor(got ValueTypeDescriptor, want wasm.ValType) bool {
	if wasm.EqualValType(want, wasm.I32) {
		return got.Kind == ValueTypeI32 && got.Ref == (ReferenceTypeDescriptor{})
	}
	return isI31RefType(want) && got.Kind == ValueTypeReference && got.Ref.Nullable == want.Ref().Nullable() && !got.Ref.Exact && !got.Ref.Heap.Defined && got.Ref.Heap.Abstract == AbstractHeapI31 && got.Ref.Heap.TypeIndex == 0
}
