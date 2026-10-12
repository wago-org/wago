//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"crypto/sha256"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
)

var rv64MutableFuncrefLocalObserveCodeDigest = [sha256.Size]byte{0x1c, 0xe0, 0xb9, 0x89, 0xdc, 0x57, 0xc1, 0xfe, 0xaa, 0x22, 0x5f, 0x7a, 0x01, 0xd1, 0x10, 0x11, 0xd6, 0x8c, 0xa9, 0x2f, 0x08, 0x9e, 0x5e, 0x11, 0xff, 0xcc, 0x25, 0xbb, 0xc3, 0x12, 0x18, 0x7d}

func rv64MutableFuncrefGlobalLocalObserveExecutionCode(c *Compiled) bool {
	return c != nil && len(c.code) == 1836 && sha256.Sum256(c.code) == rv64MutableFuncrefLocalObserveCodeDigest
}

// rv64MutableFuncrefGlobalLocalObserve authenticates the first host-visible
// local mutable funcref cell that guest code can both mutate and observe. The
// sole read reduces the retained descriptor to nullness, so it cannot leak an
// unrooted raw carrier across the public function boundary.
func rv64MutableFuncrefGlobalLocalObserve(c *Compiled) bool {
	if c == nil || c.hasNoFunctionCode() || !c.nativeContextTrapABI || !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled || c.nativeTable64ABI != nativeTable64NoABI || !c.needsFuncRefContextHeader || !c.hasTableExportMetadata || !c.NeedsFuncRefDescs || c.dynamicFuncrefEscape || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || rv64HasReferenceTail(c) {
		return false
	}
	if c.NumImports != 0 || c.dynamicImports || len(c.Imports) != 0 || len(c.importFuncSigs) != 0 || len(c.Funcs) != 6 || len(c.FuncTypeID) != 6 || len(c.Exports) != 6 || len(c.Types) != 2 || len(c.GCTypeDescs) != 2 {
		return false
	}
	for name, index := range map[string]int{"target_0": 0, "target_1": 1, "set_0": 2, "set_1": 3, "clear": 4, "is_null": 5} {
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
	voidType := &wasm.CompType{Kind: wasm.CompFunc}
	i32Type := &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}
	voidKey, i32Key := wasm.StructuralFuncTypeKey(voidType), wasm.StructuralFuncTypeKey(i32Type)
	for i, sig := range c.Funcs {
		wantType, wantKey := uint32(0), voidKey
		validResults := len(sig.Results) == 0
		if i == 5 {
			wantType, wantKey = 1, i32Key
			validResults = len(sig.Results) == 1 && sig.Results[0] == ValI32
		}
		if len(sig.Params) != 0 || !validResults || !sig.HasTypeIndex || sig.TypeIndex != wantType || sig.unsafeCrossTail || c.FuncTypeID[i] != wantKey {
			return false
		}
	}
	i32 := ValueTypeDescriptor{Kind: ValueTypeI32}
	for i, typ := range c.Types {
		validResults := len(typ.Results) == 0
		if i == 1 {
			validResults = slices.Equal(typ.Results, []ValueTypeDescriptor{i32})
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
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	global := c.Globals[0]
	exact, err := c.globalExactType(0)
	if err != nil || exact != plain || c.ValueTypes[0] != plain || global.Type != ValFuncRef || !global.Mutable || !global.HasValueType || global.ValueTypeIndex != 0 || global.HasInitFunc || global.InitFunc != 0 || global.HasInitGlobal || global.InitGlobal != 0 || len(global.InitExpr) != 0 || global.Bits != 0 || global.V128 != (V128{}) {
		return false
	}
	required := CoreFeatureReferenceTypes | CoreFeatureMutableGlobal
	wantEntry := []int{0, 208, 416, 752, 1088, 1360}
	return c.requiredFeatures == required && c.requiredAMD64Features == 0 && compiledStructuralRequiredFeatures(c) == required && slices.Equal(c.Entry, wantEntry) && slices.Equal(c.InternalEntry, wantEntry) && rv64MutableFuncrefGlobalLocalObserveExecutionCode(c)
}
