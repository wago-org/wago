//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

var rv64MutableFuncrefLocalCallCodeDigest = [sha256.Size]byte{0x68, 0x0e, 0xb8, 0xec, 0x42, 0xa6, 0xf8, 0xb2, 0x5d, 0x83, 0x13, 0x7f, 0xfe, 0xb0, 0xd9, 0x85, 0xc8, 0x20, 0x89, 0x77, 0x66, 0xf9, 0x85, 0xcb, 0x5a, 0x0d, 0x02, 0x32, 0xf7, 0x91, 0xc5, 0x51}

func rv64MutableFuncrefGlobalLocalCallExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 6812 && sha256.Sum256(c.code) == rv64MutableFuncrefLocalCallCodeDigest
}

// rv64MutableFuncrefGlobalLocalCall authenticates one host-visible nullable
// typed cell and its sole call_ref consumer. Host ingress must match type zero;
// guest writers can install only the two local targets or null.
func rv64MutableFuncrefGlobalLocalCall(c *Compiled) bool {
	if c == nil || c.hasNoFunctionCode() || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || !c.nativeReferenceFunctionABI || !c.nativeReferenceCallABI || !c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 6 || len(c.FuncTypeID) != 6 || len(c.Exports) != 6 || len(c.Types) != 2 || len(c.GCTypeDescs) != 2 {
		return false
	}
	for name, index := range map[string]int{"target_0": 0, "target_1": 1, "set_0": 2, "set_1": 3, "clear": 4, "call": 5} {
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
	i32Type := &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}
	voidType := &wasm.CompType{Kind: wasm.CompFunc}
	i32Key, voidKey := wasm.StructuralFuncTypeKey(i32Type), wasm.StructuralFuncTypeKey(voidType)
	wantTypes := []uint32{0, 0, 1, 1, 1, 0}
	for i, sig := range c.Funcs {
		wantKey := i32Key
		validResults := len(sig.Results) == 1 && sig.Results[0] == ValI32
		if wantTypes[i] == 1 {
			wantKey = voidKey
			validResults = len(sig.Results) == 0
		}
		if len(sig.Params) != 0 || !validResults || !sig.HasTypeIndex || sig.TypeIndex != wantTypes[i] || sig.unsafeCrossTail || c.FuncTypeID[i] != wantKey {
			return false
		}
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	for i, typ := range c.Types {
		validResults := slices.Equal(typ.Results, []ValueTypeDescriptor{i32})
		if i == 1 {
			validResults = len(typ.Results) == 0
		}
		if typ.RecGroup != uint32(i) || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || typ.Kind != CompositeTypeFunction || len(typ.Params) != 0 || !validResults || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
			return false
		}
	}
	for i, desc := range c.GCTypeDescs {
		if desc.ID != corergc.TypeID(i) || desc.Kind != corergc.KindFunc || len(desc.Fields) != 0 || desc.Elem != 0 || desc.Size != 0 || desc.ElemSize != 0 || desc.Align != 0 || desc.HasRefs || !desc.Final || desc.Super != 0 || desc.HasSuper {
			return false
		}
	}
	exact := c.ValueTypes[0]
	globalExact, err := c.globalExactType(0)
	global := c.Globals[0]
	if err != nil || globalExact != exact || exact.Kind != ValueTypeReference || !exact.Ref.Nullable || exact.Ref.Exact || !exact.Ref.Heap.Defined || exact.Ref.Heap.Abstract != 0 || exact.Ref.Heap.TypeIndex != 0 || global.Type != ValFuncRef || !global.Mutable || !global.HasValueType || global.ValueTypeIndex != 0 || global.HasInitFunc || global.InitFunc != 0 || global.HasInitGlobal || global.InitGlobal != 0 || len(global.InitExpr) != 0 || global.Bits != 0 || global.V128 != (V128{}) {
		return false
	}
	required := CoreFeatureReferenceTypes | CoreFeatureMutableGlobal | CoreFeatureTypedFunctionReferences
	wantEntry := []int{0, 608, 1216, 1936, 2656, 3312}
	return c.requiredFeatures == required && c.requiredAMD64Features == 0 && compiledStructuralRequiredFeatures(c) == required && slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry) && rv64MutableFuncrefGlobalLocalCallExecutionCode(c)
}
