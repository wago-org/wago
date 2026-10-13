//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

var rv64MutableFuncrefLocalExportCodeDigest = [sha256.Size]byte{0x43, 0xe5, 0xa7, 0xf5, 0xb0, 0x84, 0xca, 0x26, 0xfc, 0x98, 0x7a, 0x08, 0x17, 0x13, 0x2d, 0xfc, 0x87, 0xaa, 0x47, 0x0a, 0x69, 0x8d, 0xec, 0xd9, 0x9b, 0x81, 0x44, 0x36, 0xd6, 0xc8, 0xaa, 0xa1}

// The checked compiler emits a separate exact image. Both images retain the
// same metadata and ownership proof, including their own entry offsets.
var rv64MutableFuncrefLocalExportCheckedCodeDigest = [sha256.Size]byte{0xde, 0x82, 0xbb, 0x79, 0xdc, 0x8c, 0xb4, 0x21, 0x27, 0x9b, 0xbe, 0x38, 0xd2, 0x96, 0xe0, 0x73, 0xaf, 0xb5, 0x9a, 0x48, 0x53, 0x18, 0x03, 0x33, 0xb0, 0x7a, 0x77, 0xa8, 0xdf, 0xaf, 0x5a, 0x2f}

func rv64MutableFuncrefGlobalLocalExportExecutionCode(c *Compiled) bool {
	if c == nil {
		return false
	}
	switch len(c.code) {
	case 1580:
		return sha256.Sum256(c.code) == rv64MutableFuncrefLocalExportCodeDigest
	case 3468:
		return sha256.Sum256(c.code) == rv64MutableFuncrefLocalExportCheckedCodeDigest
	default:
		return false
	}
}

// rv64MutableFuncrefGlobalLocalExport authenticates one host-visible mutable
// funcref container. Guest writers publish only null or one of two local
// descriptors through the parked bridge, so a foreign value installed by the
// host is retired through the same ownership transaction.
func rv64MutableFuncrefGlobalLocalExport(c *Compiled) bool {
	if c == nil || c.hasNoFunctionCode() || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 5 || len(c.FuncTypeID) != 5 || len(c.Exports) != 5 || len(c.Types) != 1 || len(c.GCTypeDescs) != 1 {
		return false
	}
	for name, index := range map[string]int{"target_0": 0, "target_1": 1, "set_0": 2, "set_1": 3, "clear": 4} {
		if got, ok := c.Exports[name]; !ok || got != index {
			return false
		}
	}
	cellIndex, cellExported := c.GlobalExports["cell"]
	if len(c.GlobalImports) != 0 || len(c.Globals) != 1 || len(c.GlobalExports) != 1 || !cellExported || cellIndex != 0 || len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || len(c.memoryExportMap()) != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || len(c.customInstructions) != 0 || c.Names != nil || c.HasTable || c.HasMemory || c.tableImport != "" || c.memoryImport != "" || c.compactData != nil || len(c.Data) != 0 {
		return false
	}
	if d := c.memoryDir; d == nil || len(d.defs) != 0 || len(d.exports) != 0 || !d.exactExports || d.staged || d.stagedMemory64 || len(d.gcStructGlobals) != 0 || len(d.gcArrayGlobals) != 0 || d.gcArrayElement != nil || d.gcI31TableInit != nil || len(d.ehTags) != 0 || len(d.ehTagExports) != 0 {
		return false
	}
	funcType := &wasm.CompType{Kind: wasm.CompFunc}
	wantKey := wasm.StructuralFuncTypeKey(funcType)
	for i, sig := range c.Funcs {
		if len(sig.Params) != 0 || len(sig.Results) != 0 || !sig.HasTypeIndex || sig.TypeIndex != 0 || sig.unsafeCrossTail || c.FuncTypeID[i] != wantKey {
			return false
		}
	}
	typ := c.Types[0]
	if typ.RecGroup != 0 || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Params) != 0 || len(typ.Results) != 0 || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
		return false
	}
	desc := c.GCTypeDescs[0]
	if desc.ID != 0 || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	global := c.Globals[0]
	exact, err := c.globalExactType(0)
	if err != nil || exact != plain || global.Type != ValFuncRef || !global.Mutable || !global.HasValueType || global.ValueTypeIndex != 0 || global.HasInitFunc || global.InitFunc != 0 || global.HasInitGlobal || global.InitGlobal != 0 || len(global.InitExpr) != 0 || global.Bits != 0 || global.V128 != (V128{}) {
		return false
	}
	required := CoreFeatureReferenceTypes | CoreFeatureMutableGlobal
	wantEntry := []int{0, 208, 416, 752, 1088}
	if len(c.code) == 3468 {
		wantEntry = []int{0, 576, 1152, 1872, 2592}
	}
	return c.requiredFeatures == required && c.requiredAMD64Features == 0 && compiledStructuralRequiredFeatures(c) == required && slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry) && rv64MutableFuncrefGlobalLocalExportExecutionCode(c)
}
