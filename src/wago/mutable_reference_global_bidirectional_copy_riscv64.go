//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

var rv64MutableFuncrefBidirectionalCopyCodeDigest = [sha256.Size]byte{0x7d, 0x14, 0x2f, 0x18, 0x41, 0x31, 0x11, 0xfb, 0xac, 0x14, 0x8f, 0xaf, 0x29, 0x3d, 0xc9, 0x99, 0xee, 0x23, 0xef, 0x2e, 0xc7, 0xf2, 0xf0, 0xc6, 0x84, 0xcb, 0x61, 0x96, 0xa8, 0x89, 0x7e, 0x07}

func rv64MutableFuncrefGlobalBidirectionalCopyExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 1580 && sha256.Sum256(c.code) == rv64MutableFuncrefBidirectionalCopyCodeDigest
}

// rv64MutableFuncrefGlobalBidirectionalCopy authenticates two exact, independent
// one-write functions. The indexed parked bridge receives the destination cell
// explicitly, but arbitrary bodies and multi-write lifetime gaps remain closed.
func rv64MutableFuncrefGlobalBidirectionalCopy(c *Compiled) bool {
	if c == nil || c.hasNoFunctionCode() || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 2 || len(c.FuncTypeID) != 2 || len(c.Exports) != 2 || len(c.Types) != 1 || len(c.GCTypeDescs) != 1 {
		return false
	}
	if index, ok := c.Exports["copy_to_target"]; !ok || index != 0 {
		return false
	}
	if index, ok := c.Exports["copy_to_source"]; !ok || index != 1 {
		return false
	}
	if len(c.GlobalImports) != 2 || len(c.Globals) != 2 || len(c.GlobalExports) != 0 || len(c.ValueTypes) != 1 || c.tableCount() != 0 || len(c.extraTables) != 0 || len(c.tableExports) != 0 || c.memoryCount() != 0 || len(c.memoryExportMap()) != 0 || c.tagImportCount() != 0 || c.HasStart || c.StartIsImport || c.StartLocalFunc != 0 || c.StartImportIdx != 0 {
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
	for i, name := range []string{"source", "target"} {
		imp, global := c.GlobalImports[i], c.Globals[i]
		exact, err := c.globalExactType(i)
		if err != nil || exact != plain || imp.Module != "env" || imp.Name != name || !imp.HasValueType || imp.ValueTypeIndex != 0 || imp.Type != ValFuncRef || !imp.Mutable || global.Type != ValFuncRef || !global.Mutable || !global.HasValueType || global.ValueTypeIndex != 0 {
			return false
		}
		if global.HasInitFunc || global.InitFunc != 0 || global.HasInitGlobal || global.InitGlobal != 0 || len(global.InitExpr) != 0 || global.Bits != 0 || global.V128 != (V128{}) {
			return false
		}
	}
	required := CoreFeatureReferenceTypes | CoreFeatureMutableGlobal
	return c.requiredFeatures == required && c.requiredAMD64Features == 0 && compiledStructuralRequiredFeatures(c) == required && slices.Equal(c.Entry, []int{0, 672}) && slices.Equal(c.InternalEntry, []int{0, 672}) && rv64MutableFuncrefGlobalBidirectionalCopyExecutionCode(c)
}
