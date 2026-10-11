//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

var rv64I31CoreCodeDigest = [sha256.Size]byte{0x09, 0x33, 0xe2, 0xad, 0x20, 0x62, 0xed, 0x4a, 0xfa, 0xc8, 0x39, 0x5c, 0x3b, 0xa2, 0x68, 0x32, 0xcd, 0xbc, 0x57, 0xb4, 0xaa, 0xc3, 0xdf, 0x37, 0x4e, 0xf4, 0x74, 0x4f, 0x96, 0x9f, 0x6b, 0x76}

// rv64I31Core is the retained-artifact counterpart of the exact 65-byte source
// classifier in the backend. Wasm bodies are not serialized, so callable
// visibility is bound to the exact RV64G code image and complete surviving
// numeric metadata. This product owns no heap, roots, tables, globals, or GC
// initializer state.
func rv64I31Core(c *Compiled) bool {
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
	return c.requiredFeatures == CoreFeatureReferenceTypes|CoreFeatureGC && c.requiredAMD64Features == 0 && c.stagedGCI31Product() == stagedGCI31ProductCore && c.stagedFeatures() == CoreFeatureGC &&
		len(c.Entry) == 2 && c.Entry[0] == 0 && c.Entry[1] == 720 && len(c.InternalEntry) == 2 && c.InternalEntry[0] == 0 && c.InternalEntry[1] == 720 &&
		len(c.code) == 1432 && sha256.Sum256(c.code) == rv64I31CoreCodeDigest
}
