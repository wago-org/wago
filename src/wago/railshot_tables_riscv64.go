//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"crypto/sha256"
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// rv64StateOnlyTable64 is the persisted-artifact witness for the first RV64
// table64 profile. Keep it structural and exact: admission may use this before
// the detailed table type and capacity checks below, including on hostile
// artifacts. Any executable or initializer-bearing shape remains rejected.
func rv64StateOnlyTable64(c *Compiled) bool {
	if c == nil || !c.TableAddr64 || !c.hasNoFunctionCode() || c.tableCount() != 1 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	return c.memoryDir == nil || len(c.memoryDir.ehTags) == 0
}

func rv64OwnedTable64ActiveElement(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64NoABI || !c.TableAddr64 || c.tableCount() != 1 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 1 || len(c.Exports) != 0 || len(c.tableExports) != 1 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 1 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	def := c.tableDef(0)
	exact, err := c.tableExactType(0)
	if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || !def.Addr64 || !def.HasMax || def.Size != 3 || def.Max != 3 || c.tableRuntimeCapacity(0) != 3 {
		return false
	}
	if index, ok := c.tableExports["table"]; !ok || index != 0 {
		return false
	}
	fn := c.Funcs[0]
	if len(fn.Params) != 0 || len(c.Types) != 1 || len(c.GCTypeDescs) != 1 || len(c.ValueTypes) != 1 || c.ValueTypes[0] != plain || len(c.FuncTypeID) != 1 || len(c.Entry) != 1 || c.Entry[0] != 0 || len(c.InternalEntry) != 1 || c.InternalEntry[0] != 0 {
		return false
	}
	// Bind both exact private targets' framed RV64G images. The exported table
	// can be imported by another instance, so retained metadata alone must not
	// relabel arbitrary callable code as this narrow initializer profile.
	codeHash := sha256.Sum256(c.code)
	voidCode := [32]byte{0x1d, 0xf4, 0x1d, 0xb6, 0xda, 0x6c, 0xf7, 0x49, 0x1e, 0x95, 0x1a, 0x23, 0xe0, 0x52, 0x51, 0xba, 0xc1, 0x48, 0x0b, 0xe5, 0xaa, 0x36, 0xe1, 0x01, 0x5d, 0x3c, 0xd9, 0x75, 0x4c, 0x4d, 0x65, 0x03}
	i32Code := [32]byte{0x82, 0x47, 0x89, 0xf5, 0x32, 0x7a, 0x36, 0x99, 0x57, 0x50, 0x78, 0x68, 0x12, 0x0e, 0x89, 0x9e, 0x52, 0xdd, 0xb0, 0x66, 0x39, 0xc9, 0x01, 0xfc, 0xae, 0xbb, 0xc3, 0x16, 0x0a, 0x0b, 0x3f, 0x6a}
	voidTarget := len(fn.Results) == 0 && c.FuncTypeID[0] == voidFuncTypeKey() && len(c.code) == 576 && codeHash == voidCode
	i32Key := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}})
	i32Target := len(fn.Results) == 1 && fn.Results[0] == ValI32 && c.FuncTypeID[0] == i32Key && len(c.code) == 584 && codeHash == i32Code
	if !voidTarget && !i32Target {
		return false
	}
	if c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTable64 || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs {
		return false
	}
	e := c.Elems[0]
	if e.Mode != ElemModeActive || e.TableIndex != 0 || e.RefType != ValFuncRef || !e.HasValueType || e.Offset.Base != 0 || e.Offset.HasGlobal || !bytes.Equal(e.Offset.Expr, []byte{0x42, 0x01, 0x0b}) || len(e.Values) != 1 {
		return false
	}
	elemExact, err := c.elemExactType(e)
	if err != nil || elemExact != plain {
		return false
	}
	v := e.Values[0]
	return !v.Null && v.FuncIndex == 0 && v.GlobalIndex == 0 && !v.HasGlobal && !v.I31Wrap && !v.RepeatPrevious && len(v.Expr) == 0
}

func rv64MixedTable64ActiveElement(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64NoABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 1 || len(c.Exports) != 0 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 1 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imported, ok := c.tableImportAt(0)
	if !ok || imported.Key == "" || !imported.Addr64 || !imported.HasMax || imported.Min != 3 || imported.Max != 3 {
		return false
	}
	local := c.tableDef(1)
	for index := 0; index < 2; index++ {
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain {
			return false
		}
	}
	if local.ImportKey != "" || local.ImportHasMax || local.HasInitFunc || local.InitFunc != 0 || !local.Addr64 || !local.HasMax || local.Size != 1 || local.Max != 1 || c.tableRuntimeCapacity(1) != 1 {
		return false
	}
	var tables [2]bool
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	if !tables[0] || !tables[1] {
		return false
	}
	fn := c.Funcs[0]
	if len(fn.Params) != 0 || len(fn.Results) != 1 || fn.Results[0] != ValI32 || len(c.Types) != 1 || len(c.GCTypeDescs) != 1 || len(c.ValueTypes) != 1 || c.ValueTypes[0] != plain || len(c.FuncTypeID) != 1 || len(c.Entry) != 1 || c.Entry[0] != 0 || len(c.InternalEntry) != 1 || c.InternalEntry[0] != 0 {
		return false
	}
	i32Key := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}})
	i32Code := [32]byte{0x82, 0x47, 0x89, 0xf5, 0x32, 0x7a, 0x36, 0x99, 0x57, 0x50, 0x78, 0x68, 0x12, 0x0e, 0x89, 0x9e, 0x52, 0xdd, 0xb0, 0x66, 0x39, 0xc9, 0x01, 0xfc, 0xae, 0xbb, 0xc3, 0x16, 0x0a, 0x0b, 0x3f, 0x6a}
	if c.FuncTypeID[0] != i32Key || len(c.code) != 584 || sha256.Sum256(c.code) != i32Code || c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTable64 || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs {
		return false
	}
	e := c.Elems[0]
	if e.Mode != ElemModeActive || e.TableIndex != 0 || e.RefType != ValFuncRef || !e.HasValueType || e.Offset.Base != 0 || e.Offset.HasGlobal || !bytes.Equal(e.Offset.Expr, []byte{0x42, 0x01, 0x0b}) || len(e.Values) != 1 {
		return false
	}
	elemExact, err := c.elemExactType(e)
	if err != nil || elemExact != plain {
		return false
	}
	v := e.Values[0]
	return !v.Null && v.FuncIndex == 0 && v.GlobalIndex == 0 && !v.HasGlobal && !v.I31Wrap && !v.RepeatPrevious && len(v.Expr) == 0
}

func rv64MixedTable64DualActiveElements(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64NoABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 1 || len(c.Exports) != 0 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 2 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imported, ok := c.tableImportAt(0)
	if !ok || imported.Key == "" || !imported.Addr64 || !imported.HasMax || imported.Min != 3 || imported.Max != 3 {
		return false
	}
	local := c.tableDef(1)
	for index := 0; index < 2; index++ {
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain {
			return false
		}
	}
	if local.ImportKey != "" || local.ImportHasMax || local.HasInitFunc || local.InitFunc != 0 || !local.Addr64 || !local.HasMax || local.Size != 3 || local.Max != 3 || c.tableRuntimeCapacity(1) != 3 {
		return false
	}
	var tables [2]bool
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	if !tables[0] || !tables[1] {
		return false
	}
	fn := c.Funcs[0]
	if len(fn.Params) != 0 || len(fn.Results) != 1 || fn.Results[0] != ValI32 || len(c.Types) != 1 || len(c.GCTypeDescs) != 1 || len(c.ValueTypes) != 1 || c.ValueTypes[0] != plain || len(c.FuncTypeID) != 1 || len(c.Entry) != 1 || c.Entry[0] != 0 || len(c.InternalEntry) != 1 || c.InternalEntry[0] != 0 {
		return false
	}
	i32Key := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}})
	i32Code := [32]byte{0x82, 0x47, 0x89, 0xf5, 0x32, 0x7a, 0x36, 0x99, 0x57, 0x50, 0x78, 0x68, 0x12, 0x0e, 0x89, 0x9e, 0x52, 0xdd, 0xb0, 0x66, 0x39, 0xc9, 0x01, 0xfc, 0xae, 0xbb, 0xc3, 0x16, 0x0a, 0x0b, 0x3f, 0x6a}
	if c.FuncTypeID[0] != i32Key || len(c.code) != 584 || sha256.Sum256(c.code) != i32Code || c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTable64 || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs {
		return false
	}
	for i, e := range c.Elems {
		if e.Mode != ElemModeActive || e.TableIndex != uint32(i) || e.RefType != ValFuncRef || !e.HasValueType || e.Offset.Base != 0 || e.Offset.HasGlobal || !bytes.Equal(e.Offset.Expr, []byte{0x42, 0x01, 0x0b}) || len(e.Values) != 1 {
			return false
		}
		elemExact, err := c.elemExactType(e)
		if err != nil || elemExact != plain {
			return false
		}
		v := e.Values[0]
		if v.Null || v.FuncIndex != 0 || v.GlobalIndex != 0 || v.HasGlobal || v.I31Wrap || v.RepeatPrevious || len(v.Expr) != 0 {
			return false
		}
	}
	return true
}

func rv64MixedTable64DualActiveVectors(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64NoABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 0 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 2 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imported, ok := c.tableImportAt(0)
	if !ok || imported.Key == "" || !imported.Addr64 || !imported.HasMax || imported.Min != 4 || imported.Max != 4 {
		return false
	}
	local := c.tableDef(1)
	for index := 0; index < 2; index++ {
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain {
			return false
		}
	}
	if local.ImportKey != "" || local.ImportHasMax || local.HasInitFunc || local.InitFunc != 0 || !local.Addr64 || !local.HasMax || local.Size != 4 || local.Max != 4 || c.tableRuntimeCapacity(1) != 4 {
		return false
	}
	var tables [2]bool
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	if !tables[0] || !tables[1] {
		return false
	}
	i32Key := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}})
	if len(c.Types) != 1 || len(c.GCTypeDescs) != 1 || len(c.ValueTypes) != 1 || c.ValueTypes[0] != plain || len(c.FuncTypeID) != 2 || len(c.Entry) != 2 || c.Entry[0] != 0 || c.Entry[1] != 592 || len(c.InternalEntry) != 2 || c.InternalEntry[0] != 0 || c.InternalEntry[1] != 592 {
		return false
	}
	for i := range c.Funcs {
		fn := c.Funcs[i]
		if len(fn.Params) != 0 || len(fn.Results) != 1 || fn.Results[0] != ValI32 || c.FuncTypeID[i] != i32Key {
			return false
		}
	}
	wantCode := [32]byte{0xb6, 0xc3, 0xbe, 0x0f, 0x45, 0x1e, 0xc5, 0x27, 0x96, 0x39, 0xb0, 0x42, 0x67, 0x8e, 0xf6, 0x60, 0x6d, 0x00, 0x67, 0xde, 0x4e, 0x08, 0x7a, 0x21, 0x2f, 0xd1, 0x20, 0x16, 0x61, 0xa8, 0x9a, 0x3d}
	if len(c.code) != 1176 || sha256.Sum256(c.code) != wantCode || c.requiredFeatures != CoreFeatureReferenceTypes|CoreFeatureTable64 || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs {
		return false
	}
	for i, e := range c.Elems {
		if e.Mode != ElemModeActive || e.TableIndex != uint32(i) || e.RefType != ValFuncRef || !e.HasValueType || e.Offset.Base != 0 || e.Offset.HasGlobal || !bytes.Equal(e.Offset.Expr, []byte{0x42, 0x01, 0x0b}) || len(e.Values) != 2 {
			return false
		}
		elemExact, err := c.elemExactType(e)
		if err != nil || elemExact != plain {
			return false
		}
		for target, v := range e.Values {
			if v.Null || v.FuncIndex != uint32(target) || v.GlobalIndex != 0 || v.HasGlobal || v.I31Wrap || v.RepeatPrevious || len(v.Expr) != 0 {
				return false
			}
		}
	}
	return true
}

// rv64StateOnlyTable32ScalarGlobals is the persisted-artifact witness for one
// bounded nullable-funcref table32 composed with four immutable literal scalar
// globals. It admits retained descriptors and cells only; no table/reference
// operation or initializer dependency is implied by this declaration profile.
func rv64StateOnlyTable32ScalarGlobals(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64NoABI || c.TableAddr64 || !c.hasNoFunctionCode() || c.tableCount() != 1 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 0 || len(c.Exports) != 0 || len(c.Types) != 0 || len(c.GCTypeDescs) != 0 || len(c.FuncTypeID) != 0 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 4 || len(c.GlobalImports) != 0 || len(c.GlobalExports) != 4 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	if c.requiredFeatures != 0 || compiledStructuralRequiredFeatures(c) != 0 || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs {
		return false
	}
	table := c.tableDef(0)
	plainRef, _ := valueTypeDescriptorFromValType(ValFuncRef)
	exactTable, err := c.tableExactType(0)
	if err != nil || exactTable != plainRef || !c.hasTableExportMetadata || len(c.tableExports) != 1 || table.ImportKey != "" || table.Type != ValFuncRef || !table.HasValueType || table.ValueTypeIndex != 4 || table.HasInitFunc || table.InitFunc != 0 || table.Addr64 || !table.HasMax || table.Size < 0 || table.Size > railshot.MaxOwnedTableEntries || table.Max > railshot.MaxOwnedTableEntries || table.Max < uint64(table.Size) || c.tableRuntimeCapacity(0) < table.Size || c.tableRuntimeCapacity(0) > railshot.MaxOwnedTableEntries {
		return false
	}
	for name, index := range c.tableExports {
		if index != 0 {
			return false
		}
		if _, duplicate := c.GlobalExports[name]; duplicate {
			return false
		}
	}
	want := [...]ValType{ValI32, ValI64, ValF32, ValF64}
	if len(c.ValueTypes) != len(want)+1 || c.ValueTypes[4] != plainRef {
		return false
	}
	for i, typ := range want {
		plain, _ := valueTypeDescriptorFromValType(typ)
		global := c.Globals[i]
		exact, exactErr := c.globalExactType(i)
		if exactErr != nil || exact != plain || c.ValueTypes[i] != plain || global.Type != typ || !global.HasValueType || global.ValueTypeIndex != uint32(i) || global.Mutable || global.HasInitFunc || global.InitFunc != 0 || global.HasInitGlobal || global.InitGlobal != 0 || len(global.InitExpr) != 0 || global.V128 != (V128{}) || (typ == ValI32 || typ == ValF32) && global.Bits>>32 != 0 {
			return false
		}
	}
	var exported [4]bool
	for _, index := range c.GlobalExports {
		if index < 0 || index >= len(exported) || exported[index] {
			return false
		}
		exported[index] = true
	}
	return exported[0] && exported[1] && exported[2] && exported[3]
}

// rv64StateOnlyTwoTable32 is the persisted-artifact witness for two bounded
// owned table32 declarations with no executable or initializer-bearing state.
// Keep the export directory exact so hostile artifacts cannot alias one table
// or smuggle a third resource through the state-only path.
func rv64StateOnlyTwoTable32(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64NoABI || c.TableAddr64 || !c.hasNoFunctionCode() || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 0 || len(c.Exports) != 0 || len(c.tableExports) != 2 || len(c.Types) != 0 || len(c.GCTypeDescs) != 0 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	if c.requiredFeatures != CoreFeatureReferenceTypes || c.nativeReferenceFunctionABI || len(c.FuncTypeID) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	if len(c.ValueTypes) != 1 || c.ValueTypes[0] != plain {
		return false
	}
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain || !def.HasValueType || def.ValueTypeIndex != 0 || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || def.Addr64 || !def.HasMax || def.Size < 0 || def.Size > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < uint64(def.Size) || c.tableRuntimeCapacity(index) < def.Size || c.tableRuntimeCapacity(index) > railshot.MaxOwnedTableEntries {
			return false
		}
	}
	var tables [2]bool
	for _, index := range c.tableExports {
		if index < 0 || index > 1 || tables[index] {
			return false
		}
		tables[index] = true
	}
	return tables[0] && tables[1]
}

func rv64OwnedTable64Size(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64SizeABI || !c.TableAddr64 || c.tableCount() != 1 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 1 || len(c.Exports) == 0 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	sig := c.Funcs[0]
	if len(sig.Params) != 0 || len(sig.Results) != 1 || sig.Results[0] != ValI64 {
		return false
	}
	for _, index := range c.Exports {
		if index != 0 {
			return false
		}
	}
	return true
}

func rv64OwnedTwoTable32Size(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64SizeABI || c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	if !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || c.requiredFeatures.IsEnabled(CoreFeatureTable64) || c.nativeReferenceFunctionABI || !c.NeedsFuncRefDescs {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || def.Addr64 || !def.HasMax || def.Size < 0 || def.Size > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < uint64(def.Size) || c.tableRuntimeCapacity(index) < def.Size || c.tableRuntimeCapacity(index) > railshot.MaxOwnedTableEntries {
			return false
		}
		sig := c.Funcs[index]
		if len(sig.Params) != 0 || len(sig.Results) != 1 || sig.Results[0] != ValI32 {
			return false
		}
	}
	var functions [2]bool
	var tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func rv64OwnedTwoTable32GetSet(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64GetSetABI || c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	if !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || c.requiredFeatures.IsEnabled(CoreFeatureTable64) || c.nativeReferenceFunctionABI || !c.NeedsFuncRefDescs {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || def.Addr64 || !def.HasMax || def.Size < 0 || def.Size > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < uint64(def.Size) || c.tableRuntimeCapacity(index) < def.Size || c.tableRuntimeCapacity(index) > railshot.MaxOwnedTableEntries {
			return false
		}
		sig := c.Funcs[index]
		if len(sig.Params) != 1 || sig.Params[0] != ValI32 || len(sig.Results) != 1 || sig.Results[0] != ValI32 {
			return false
		}
	}
	var functions, tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func rv64OwnedTwoTable32Grow(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64GrowABI || c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	if !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || c.requiredFeatures.IsEnabled(CoreFeatureTable64) || c.nativeReferenceFunctionABI || !c.NeedsFuncRefDescs {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || def.Addr64 || !def.HasMax || def.Size < 0 || def.Size > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < uint64(def.Size) || c.tableRuntimeCapacity(index) < def.Size || c.tableRuntimeCapacity(index) > railshot.MaxOwnedTableEntries {
			return false
		}
		sig := c.Funcs[index]
		if len(sig.Params) != 1 || sig.Params[0] != ValI32 || len(sig.Results) != 1 || sig.Results[0] != ValI32 {
			return false
		}
	}
	var functions, tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func rv64OwnedTwoTable32Fill(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64FillABI || c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	if !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || c.requiredFeatures.IsEnabled(CoreFeatureTable64) || c.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) || c.nativeReferenceFunctionABI || !c.NeedsFuncRefDescs {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || def.Addr64 || !def.HasMax || def.Size < 0 || def.Size > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < uint64(def.Size) || c.tableRuntimeCapacity(index) < def.Size || c.tableRuntimeCapacity(index) > railshot.MaxOwnedTableEntries {
			return false
		}
		sig := c.Funcs[index]
		if len(sig.Params) != 2 || sig.Params[0] != ValI32 || sig.Params[1] != ValI32 || len(sig.Results) != 1 || sig.Results[0] != ValI32 {
			return false
		}
	}
	var functions, tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func rv64OwnedTwoTable32Copy(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64CopyABI || c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	if !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations) || c.requiredFeatures.IsEnabled(CoreFeatureTable64) || c.nativeReferenceFunctionABI || !c.NeedsFuncRefDescs {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || def.Addr64 || !def.HasMax || def.Size < 0 || def.Size > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < uint64(def.Size) || c.tableRuntimeCapacity(index) < def.Size || c.tableRuntimeCapacity(index) > railshot.MaxOwnedTableEntries {
			return false
		}
		sig := c.Funcs[index]
		if len(sig.Params) != 3 || sig.Params[0] != ValI32 || sig.Params[1] != ValI32 || sig.Params[2] != ValI32 || len(sig.Results) != 1 || sig.Results[0] != ValI32 {
			return false
		}
	}
	var functions, tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func rv64OwnedTwoTable32InitDrop(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64InitDropABI || c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 3 || len(c.Exports) != 3 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 1 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || def.Addr64 || !def.HasMax || def.Size < 0 || def.Size > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < uint64(def.Size) || c.tableRuntimeCapacity(index) < def.Size || c.tableRuntimeCapacity(index) > railshot.MaxOwnedTableEntries {
			return false
		}
		init := c.Funcs[index]
		if len(init.Params) != 3 || init.Params[0] != ValI32 || init.Params[1] != ValI32 || init.Params[2] != ValI32 || len(init.Results) != 1 || init.Results[0] != ValI32 {
			return false
		}
	}
	drop := c.Funcs[2]
	if len(drop.Params) != 0 || len(drop.Results) != 1 || drop.Results[0] != ValI32 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations) || c.requiredFeatures.IsEnabled(CoreFeatureTable64) {
		return false
	}
	e := c.passiveElems[0]
	if e.Mode != ElemModePassive || e.TableIndex != 0 || e.RefType != ValFuncRef || !e.HasValueType || e.Offset.Base != 0 || e.Offset.HasGlobal || len(e.Offset.Expr) != 0 || len(e.Values) != 1 {
		return false
	}
	elementType, err := c.elemExactType(e)
	if err != nil || elementType != plain {
		return false
	}
	v := e.Values[0]
	if !v.Null || v.FuncIndex != 0 || v.GlobalIndex != 0 || v.HasGlobal || v.I31Wrap || v.RepeatPrevious || len(v.Expr) != 0 {
		return false
	}
	var functions [3]bool
	var tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 2 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && functions[2] && tables[0] && tables[1]
}

func rv64OwnedTwoTable64Size(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64SizeABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || !def.Addr64 || !def.HasMax || def.Size < 0 || def.Size > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < uint64(def.Size) || c.tableRuntimeCapacity(index) < def.Size || c.tableRuntimeCapacity(index) > railshot.MaxOwnedTableEntries {
			return false
		}
		sig := c.Funcs[index]
		if len(sig.Params) != 0 || len(sig.Results) != 1 || sig.Results[0] != ValI64 {
			return false
		}
	}
	var functions, tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1] && c.requiredFeatures.IsEnabled(CoreFeatureTable64|CoreFeatureReferenceTypes) && !c.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations)
}

func rv64MixedTable64Size(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64ImportedSizeABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imported, ok := c.tableImportAt(0)
	if !ok || imported.Key == "" || !imported.Addr64 || !imported.HasMax || imported.Min > railshot.MaxOwnedTableEntries || imported.Max > railshot.MaxOwnedTableEntries || imported.Max < imported.Min {
		return false
	}
	for index := 0; index < 2; index++ {
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain {
			return false
		}
		sig := c.Funcs[index]
		if len(sig.Params) != 0 || len(sig.Results) != 1 || sig.Results[0] != ValI64 {
			return false
		}
	}
	local := c.tableDef(1)
	if local.ImportKey != "" || local.ImportHasMax || local.HasInitFunc || local.InitFunc != 0 || !local.Addr64 || !local.HasMax || local.Size < 0 || local.Size > railshot.MaxOwnedTableEntries || local.Max > railshot.MaxOwnedTableEntries || local.Max < uint64(local.Size) || c.tableRuntimeCapacity(1) < local.Size || c.tableRuntimeCapacity(1) > railshot.MaxOwnedTableEntries {
		return false
	}
	if c.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs {
		return false
	}
	var functions, tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func rv64MixedTable64GetSet(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64ImportedGetSetABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imported, ok := c.tableImportAt(0)
	if !ok || imported.Key == "" || !imported.Addr64 || !imported.HasMax || imported.Min > railshot.MaxOwnedTableEntries || imported.Max > railshot.MaxOwnedTableEntries || imported.Max < imported.Min {
		return false
	}
	for index := 0; index < 2; index++ {
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain {
			return false
		}
		sig := c.Funcs[index]
		if len(sig.Params) != 1 || sig.Params[0] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI32 {
			return false
		}
	}
	local := c.tableDef(1)
	if local.ImportKey != "" || local.ImportHasMax || local.HasInitFunc || local.InitFunc != 0 || !local.Addr64 || !local.HasMax || local.Size < 0 || local.Size > railshot.MaxOwnedTableEntries || local.Max > railshot.MaxOwnedTableEntries || local.Max < uint64(local.Size) || c.tableRuntimeCapacity(1) < local.Size || c.tableRuntimeCapacity(1) > railshot.MaxOwnedTableEntries {
		return false
	}
	if c.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs {
		return false
	}
	var functions, tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func rv64MixedTable64Grow(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64ImportedGrowABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imported, ok := c.tableImportAt(0)
	if !ok || imported.Key == "" || !imported.Addr64 || !imported.HasMax || imported.Min > railshot.MaxOwnedTableEntries || imported.Max > railshot.MaxOwnedTableEntries || imported.Max < imported.Min {
		return false
	}
	for index := 0; index < 2; index++ {
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain {
			return false
		}
		sig := c.Funcs[index]
		if len(sig.Params) != 1 || sig.Params[0] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI64 {
			return false
		}
	}
	local := c.tableDef(1)
	if local.ImportKey != "" || local.ImportHasMax || local.HasInitFunc || local.InitFunc != 0 || !local.Addr64 || !local.HasMax || local.Size < 0 || local.Size > railshot.MaxOwnedTableEntries || local.Max > railshot.MaxOwnedTableEntries || local.Max < uint64(local.Size) || c.tableRuntimeCapacity(1) < local.Size || c.tableRuntimeCapacity(1) > railshot.MaxOwnedTableEntries {
		return false
	}
	if c.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs {
		return false
	}
	var functions, tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func rv64MixedTable64Fill(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64ImportedFillABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imported, ok := c.tableImportAt(0)
	if !ok || imported.Key == "" || !imported.Addr64 || !imported.HasMax || imported.Min > railshot.MaxOwnedTableEntries || imported.Max > railshot.MaxOwnedTableEntries || imported.Max < imported.Min {
		return false
	}
	for index := 0; index < 2; index++ {
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain {
			return false
		}
		sig := c.Funcs[index]
		if len(sig.Params) != 2 || sig.Params[0] != ValI64 || sig.Params[1] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI32 {
			return false
		}
	}
	local := c.tableDef(1)
	if local.ImportKey != "" || local.ImportHasMax || local.HasInitFunc || local.InitFunc != 0 || !local.Addr64 || !local.HasMax || local.Size < 0 || local.Size > railshot.MaxOwnedTableEntries || local.Max > railshot.MaxOwnedTableEntries || local.Max < uint64(local.Size) || c.tableRuntimeCapacity(1) < local.Size || c.tableRuntimeCapacity(1) > railshot.MaxOwnedTableEntries {
		return false
	}
	if c.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs {
		return false
	}
	var functions, tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func rv64MixedTable64Copy(c *Compiled) bool {
	want := [32]byte{0x9c, 0x86, 0x98, 0xb1, 0xcb, 0xd7, 0x25, 0xaf, 0xbf, 0x1d, 0x8a, 0xa6, 0x82, 0x78, 0xd2, 0xda, 0x63, 0x56, 0x95, 0x73, 0xd0, 0x61, 0xe1, 0xf0, 0xb8, 0xb3, 0x4f, 0x26, 0xaf, 0xb4, 0x2b, 0xab}
	return rv64MixedTable64CopyProfile(c) && len(c.code) == 2744 && sha256.Sum256(c.code) == want
}

func rv64MixedTable64CrossCopy(c *Compiled) bool {
	want := [32]byte{0x0c, 0x92, 0xb6, 0xbb, 0xad, 0x9d, 0x34, 0x69, 0x13, 0xfb, 0x5f, 0xdd, 0xfa, 0x64, 0x8f, 0xcd, 0x08, 0x43, 0x88, 0x89, 0x66, 0x79, 0x8b, 0xb4, 0x0b, 0x9d, 0x7a, 0x9c, 0x36, 0x78, 0x87, 0xaf}
	return rv64MixedTable64CopyProfile(c) && len(c.code) == 2936 && sha256.Sum256(c.code) == want
}

func rv64MixedTable64CopyProfile(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64ImportedCopyABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imported, ok := c.tableImportAt(0)
	if !ok || imported.Key == "" || !imported.Addr64 || !imported.HasMax || imported.Min > railshot.MaxOwnedTableEntries || imported.Max > railshot.MaxOwnedTableEntries || imported.Max < imported.Min {
		return false
	}
	for index := 0; index < 2; index++ {
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain {
			return false
		}
		sig := c.Funcs[index]
		if len(sig.Params) != 3 || sig.Params[0] != ValI64 || sig.Params[1] != ValI64 || sig.Params[2] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI32 {
			return false
		}
	}
	local := c.tableDef(1)
	if local.ImportKey != "" || local.ImportHasMax || local.HasInitFunc || local.InitFunc != 0 || !local.Addr64 || !local.HasMax || local.Size < 0 || local.Size > railshot.MaxOwnedTableEntries || local.Max > railshot.MaxOwnedTableEntries || local.Max < uint64(local.Size) || c.tableRuntimeCapacity(1) < local.Size || c.tableRuntimeCapacity(1) > railshot.MaxOwnedTableEntries {
		return false
	}
	if c.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs {
		return false
	}
	var functions, tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func rv64MixedTable64InitDrop(c *Compiled) bool {
	return rv64MixedTable64NullInitDrop(c) || rv64MixedTable64PassiveVector(c)
}

func rv64MixedTable64NullInitDrop(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64ImportedInitDropABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 3 || len(c.Exports) != 3 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 1 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imported, ok := c.tableImportAt(0)
	if !ok || imported.Key == "" || !imported.Addr64 || !imported.HasMax || imported.Min > railshot.MaxOwnedTableEntries || imported.Max > railshot.MaxOwnedTableEntries || imported.Max < imported.Min {
		return false
	}
	for index := 0; index < 2; index++ {
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain {
			return false
		}
		init := c.Funcs[index]
		if len(init.Params) != 3 || init.Params[0] != ValI64 || init.Params[1] != ValI32 || init.Params[2] != ValI32 || len(init.Results) != 1 || init.Results[0] != ValI32 {
			return false
		}
	}
	local := c.tableDef(1)
	if local.ImportKey != "" || local.ImportHasMax || local.HasInitFunc || local.InitFunc != 0 || !local.Addr64 || !local.HasMax || local.Size < 0 || local.Size > railshot.MaxOwnedTableEntries || local.Max > railshot.MaxOwnedTableEntries || local.Max < uint64(local.Size) || c.tableRuntimeCapacity(1) < local.Size || c.tableRuntimeCapacity(1) > railshot.MaxOwnedTableEntries {
		return false
	}
	drop := c.Funcs[2]
	if len(drop.Params) != 0 || len(drop.Results) != 1 || drop.Results[0] != ValI32 || c.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs {
		return false
	}
	e := c.passiveElems[0]
	if e.Mode != ElemModePassive || e.TableIndex != 0 || e.RefType != ValFuncRef || !e.HasValueType || e.Offset.Base != 0 || e.Offset.HasGlobal || len(e.Offset.Expr) != 0 || len(e.Values) != 1 {
		return false
	}
	elementType, err := c.elemExactType(e)
	if err != nil || elementType != plain {
		return false
	}
	v := e.Values[0]
	if !v.Null || v.FuncIndex != 0 || v.GlobalIndex != 0 || v.HasGlobal || v.I31Wrap || v.RepeatPrevious || len(v.Expr) != 0 {
		return false
	}
	var functions [3]bool
	var tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 2 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && functions[2] && tables[0] && tables[1]
}

func rv64MixedTable64PassiveVector(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64ImportedInitDropABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 7 || len(c.Exports) != 5 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 1 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imported, ok := c.tableImportAt(0)
	if !ok || imported.Key == "" || !imported.Addr64 || !imported.HasMax || imported.Min != 4 || imported.Max != 4 {
		return false
	}
	local := c.tableDef(1)
	for index := 0; index < 2; index++ {
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain {
			return false
		}
	}
	if local.ImportKey != "" || local.ImportHasMax || local.HasInitFunc || local.InitFunc != 0 || !local.Addr64 || !local.HasMax || local.Size != 4 || local.Max != 4 || c.tableRuntimeCapacity(1) != 4 {
		return false
	}
	initKey := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64, wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}})
	targetKey := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}})
	callKey := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I32}})
	entries := [...]int{0, 592, 1184, 2400, 3744, 4432, 7280}
	if len(c.Types) != 3 || len(c.GCTypeDescs) != 3 || len(c.ValueTypes) != 1 || c.ValueTypes[0] != plain || len(c.FuncTypeID) != 7 || len(c.Entry) != len(entries) || len(c.InternalEntry) != len(entries) {
		return false
	}
	for i, entry := range entries {
		if c.Entry[i] != entry || c.InternalEntry[i] != entry {
			return false
		}
	}
	wants := [...]uint64{targetKey, targetKey, initKey, initKey, targetKey, callKey, callKey}
	for i, fn := range c.Funcs {
		if c.FuncTypeID[i] != wants[i] || len(fn.Results) != 1 || fn.Results[0] != ValI32 {
			return false
		}
		switch i {
		case 0, 1, 4:
			if len(fn.Params) != 0 {
				return false
			}
		case 2, 3:
			if len(fn.Params) != 3 || fn.Params[0] != ValI64 || fn.Params[1] != ValI32 || fn.Params[2] != ValI32 {
				return false
			}
		case 5, 6:
			if len(fn.Params) != 1 || fn.Params[0] != ValI64 {
				return false
			}
		}
	}
	wantCode := [32]byte{0x5e, 0x04, 0x58, 0x25, 0xdd, 0x44, 0x3e, 0x7d, 0xb9, 0x7f, 0xa8, 0xfb, 0xd2, 0x16, 0x53, 0x3f, 0xdf, 0x6e, 0xff, 0x87, 0x9a, 0x04, 0xe1, 0xdd, 0x54, 0x2a, 0xa7, 0x2b, 0x34, 0x57, 0x68, 0x0e}
	if len(c.code) != 10192 || sha256.Sum256(c.code) != wantCode || c.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs {
		return false
	}
	e := c.passiveElems[0]
	if e.Mode != ElemModePassive || e.TableIndex != 0 || e.RefType != ValFuncRef || !e.HasValueType || e.Offset.Base != 0 || e.Offset.HasGlobal || len(e.Offset.Expr) != 0 || len(e.Values) != 2 {
		return false
	}
	elementType, err := c.elemExactType(e)
	if err != nil || elementType != plain {
		return false
	}
	for target, v := range e.Values {
		if v.Null || v.FuncIndex != uint32(target) || v.GlobalIndex != 0 || v.HasGlobal || v.I31Wrap || v.RepeatPrevious || len(v.Expr) != 0 {
			return false
		}
	}
	var functions [5]bool
	var tables [2]bool
	for _, index := range c.Exports {
		if index < 2 || index > 6 {
			return false
		}
		functions[index-2] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && functions[2] && functions[3] && functions[4] && tables[0] && tables[1]
}

func rv64OwnedTwoTable64GetSet(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64GetSetABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || !def.Addr64 || !def.HasMax || def.Size < 0 || def.Size > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < uint64(def.Size) || c.tableRuntimeCapacity(index) < def.Size || c.tableRuntimeCapacity(index) > railshot.MaxOwnedTableEntries {
			return false
		}
		sig := c.Funcs[index]
		if len(sig.Params) != 1 || sig.Params[0] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI32 {
			return false
		}
	}
	var functions, tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1] && c.requiredFeatures.IsEnabled(CoreFeatureTable64|CoreFeatureReferenceTypes) && !c.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations)
}

func rv64OwnedTwoTable64Grow(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64GrowABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || !def.Addr64 || !def.HasMax || def.Size < 0 || def.Size > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < uint64(def.Size) || c.tableRuntimeCapacity(index) < def.Size || c.tableRuntimeCapacity(index) > railshot.MaxOwnedTableEntries {
			return false
		}
		sig := c.Funcs[index]
		if len(sig.Params) != 1 || sig.Params[0] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI64 {
			return false
		}
	}
	var functions, tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1] && c.requiredFeatures.IsEnabled(CoreFeatureTable64|CoreFeatureReferenceTypes) && !c.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations)
}

func rv64OwnedTwoTable64Fill(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64FillABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || !def.Addr64 || !def.HasMax || def.Size < 0 || def.Size > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < uint64(def.Size) || c.tableRuntimeCapacity(index) < def.Size || c.tableRuntimeCapacity(index) > railshot.MaxOwnedTableEntries {
			return false
		}
		sig := c.Funcs[index]
		if len(sig.Params) != 2 || sig.Params[0] != ValI64 || sig.Params[1] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI32 {
			return false
		}
	}
	var functions, tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1] && c.requiredFeatures.IsEnabled(CoreFeatureTable64|CoreFeatureReferenceTypes) && !c.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations)
}

func rv64OwnedTwoTable64Copy(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64CopyABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || !def.Addr64 || !def.HasMax || def.Size < 0 || def.Size > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < uint64(def.Size) || c.tableRuntimeCapacity(index) < def.Size || c.tableRuntimeCapacity(index) > railshot.MaxOwnedTableEntries {
			return false
		}
		sig := c.Funcs[index]
		if len(sig.Params) != 3 || sig.Params[0] != ValI64 || sig.Params[1] != ValI64 || sig.Params[2] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI32 {
			return false
		}
	}
	var functions, tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1] && c.requiredFeatures.IsEnabled(CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations)
}

func rv64ImportedTable64Size(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64ImportedSizeABI || !c.TableAddr64 || c.tableCount() != 1 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 1 || len(c.Exports) == 0 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	def, imported := c.tableImportAt(0)
	if !imported || def.Key == "" || !def.Addr64 || !def.HasMax || def.Min > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < def.Min {
		return false
	}
	sig := c.Funcs[0]
	if len(sig.Params) != 0 || len(sig.Results) != 1 || sig.Results[0] != ValI64 {
		return false
	}
	for _, index := range c.Exports {
		if index != 0 {
			return false
		}
	}
	return true
}

func rv64ImportedTable64GetSet(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64ImportedGetSetABI || !c.TableAddr64 || c.tableCount() != 1 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 1 || len(c.Exports) == 0 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	def, imported := c.tableImportAt(0)
	if !imported || def.Key == "" || !def.Addr64 || !def.HasMax || def.Min > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < def.Min {
		return false
	}
	sig := c.Funcs[0]
	if len(sig.Params) != 1 || sig.Params[0] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI32 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
		return false
	}
	for _, index := range c.Exports {
		if index != 0 {
			return false
		}
	}
	return true
}

func rv64ImportedTable64Grow(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64ImportedGrowABI || !c.TableAddr64 || c.tableCount() != 1 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 1 || len(c.Exports) == 0 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	def, imported := c.tableImportAt(0)
	if !imported || def.Key == "" || !def.Addr64 || !def.HasMax || def.Min > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < def.Min {
		return false
	}
	sig := c.Funcs[0]
	if len(sig.Params) != 1 || sig.Params[0] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI64 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
		return false
	}
	for _, index := range c.Exports {
		if index != 0 {
			return false
		}
	}
	return true
}

func rv64ImportedTable64Fill(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64ImportedFillABI || !c.TableAddr64 || c.tableCount() != 1 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 1 || len(c.Exports) == 0 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	def, imported := c.tableImportAt(0)
	if !imported || def.Key == "" || !def.Addr64 || !def.HasMax || def.Min > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < def.Min {
		return false
	}
	sig := c.Funcs[0]
	if len(sig.Params) != 2 || sig.Params[0] != ValI64 || sig.Params[1] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI32 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || c.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) {
		return false
	}
	for _, index := range c.Exports {
		if index != 0 {
			return false
		}
	}
	return true
}

func rv64ImportedTable64Copy(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64ImportedCopyABI || !c.TableAddr64 || c.tableCount() != 1 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 1 || len(c.Exports) == 0 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	def, imported := c.tableImportAt(0)
	if !imported || def.Key == "" || !def.Addr64 || !def.HasMax || def.Min > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < def.Min {
		return false
	}
	sig := c.Funcs[0]
	if len(sig.Params) != 3 || sig.Params[0] != ValI64 || sig.Params[1] != ValI64 || sig.Params[2] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI32 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations) {
		return false
	}
	for _, index := range c.Exports {
		if index != 0 {
			return false
		}
	}
	return true
}

func rv64ImportedTable64InitDrop(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64ImportedInitDropABI || !c.TableAddr64 || c.tableCount() != 1 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) < 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 1 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	def, imported := c.tableImportAt(0)
	if !imported || def.Key == "" || !def.Addr64 || !def.HasMax || def.Min > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < def.Min {
		return false
	}
	init, drop := c.Funcs[0], c.Funcs[1]
	if len(init.Params) != 3 || init.Params[0] != ValI64 || init.Params[1] != ValI32 || init.Params[2] != ValI32 || len(init.Results) != 1 || init.Results[0] != ValI32 || len(drop.Params) != 0 || len(drop.Results) != 1 || drop.Results[0] != ValI32 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations) {
		return false
	}
	e := c.passiveElems[0]
	if e.Mode != ElemModePassive || e.TableIndex != 0 || e.RefType != ValFuncRef || !e.HasValueType || e.Offset.Base != 0 || e.Offset.HasGlobal || len(e.Offset.Expr) != 0 || len(e.Values) != 1 {
		return false
	}
	v := e.Values[0]
	if !v.Null || v.FuncIndex != 0 || v.GlobalIndex != 0 || v.HasGlobal || v.I31Wrap || v.RepeatPrevious || len(v.Expr) != 0 {
		return false
	}
	seen := [2]bool{}
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		seen[index] = true
	}
	return seen[0] && seen[1]
}

func rv64OwnedTable64Grow(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64GrowABI || !c.TableAddr64 || !c.TableHasMax || c.tableCount() != 1 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 1 || len(c.Exports) == 0 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	sig := c.Funcs[0]
	if len(sig.Params) != 1 || sig.Params[0] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI64 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
		return false
	}
	for _, index := range c.Exports {
		if index != 0 {
			return false
		}
	}
	return true
}

func rv64OwnedTable64GetSet(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64GetSetABI || !c.TableAddr64 || !c.TableHasMax || c.tableCount() != 1 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 1 || len(c.Exports) == 0 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	sig := c.Funcs[0]
	if len(sig.Params) != 1 || sig.Params[0] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI32 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
		return false
	}
	for _, index := range c.Exports {
		if index != 0 {
			return false
		}
	}
	return true
}

func rv64OwnedTable64Indirect(c *Compiled) bool {
	return rv64OwnedTable64IndirectCall(c) || rv64OwnedTable64IndirectTail(c)
}

func rv64OwnedTable64IndirectCall(c *Compiled) bool {
	if rv64NativeTailModule(c) != nil {
		return false
	}
	return rv64OwnedTable64IndirectCommon(c)
}

func rv64OwnedTable64IndirectTail(c *Compiled) bool {
	return rv64OwnedTable64IndirectTailProduct(c, false)
}

func rv64OwnedTable64IndirectTailPlanShape(c *Compiled) bool {
	return rv64OwnedTable64IndirectTailProduct(c, true)
}

func rv64OwnedTable64IndirectTailProduct(c *Compiled, beforeGCDescriptors bool) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64IndirectABI || !c.TableAddr64 || c.tableCount() != 1 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 3 || len(c.Exports) != 2 || len(c.tableExports) != 1 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	i32, _ := valueTypeDescriptorFromValType(ValI32)
	i64, _ := valueTypeDescriptorFromValType(ValI64)
	def := c.tableDef(0)
	exact, err := c.tableExactType(0)
	if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || !def.Addr64 || !def.HasMax || def.Size != 2 || def.Max != 2 || c.tableRuntimeCapacity(0) != 2 {
		return false
	}
	targetKey := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}})
	callerKey := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I32}})
	if len(c.Types) != 2 || len(c.ValueTypes) != 1 || c.ValueTypes[0] != plain || len(c.FuncTypeID) != 3 || c.FuncTypeID[0] != targetKey || c.FuncTypeID[1] != callerKey || c.FuncTypeID[2] != callerKey || len(c.Entry) != 3 || len(c.InternalEntry) != 3 {
		return false
	}
	entries := [...]int{0, 592, 1840}
	for i, entry := range entries {
		if c.Entry[i] != entry || c.InternalEntry[i] != entry {
			return false
		}
	}
	if (!beforeGCDescriptors && len(c.GCTypeDescs) != 2) || (beforeGCDescriptors && len(c.GCTypeDescs) != 0 && len(c.GCTypeDescs) != 2) {
		return false
	}
	for _, desc := range c.GCTypeDescs {
		if desc.Kind != gc.KindFunc || !desc.Final || desc.HasSuper {
			return false
		}
	}
	for i, d := range c.Types {
		if d.RecGroup != uint32(i) || !d.Final || len(d.Supers) != 0 || d.HasDescribes || d.HasDescriptor || d.Kind != CompositeTypeFunction || len(d.Fields) != 0 || d.Array != (FieldTypeDescriptor{}) || len(d.Results) != 1 || d.Results[0] != i32 || i == 0 && len(d.Params) != 0 || i == 1 && (len(d.Params) != 1 || d.Params[0] != i64) {
			return false
		}
	}
	for i, fn := range c.Funcs {
		if i == 0 {
			if len(fn.Params) != 0 || len(fn.Results) != 1 || fn.Results[0] != ValI32 {
				return false
			}
		} else if len(fn.Params) != 1 || fn.Params[0] != ValI64 || len(fn.Results) != 1 || fn.Results[0] != ValI32 {
			return false
		}
	}
	if c.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations|CoreFeatureTailCall || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs {
		return false
	}
	wantCode := [32]byte{0xda, 0xb3, 0x7b, 0x02, 0x59, 0x47, 0xed, 0x23, 0x0e, 0xb6, 0x05, 0x41, 0xdb, 0xea, 0xbc, 0xec, 0x4c, 0xd0, 0x67, 0x4a, 0xf3, 0xb2, 0xc3, 0xf3, 0xb0, 0x6b, 0x91, 0xa0, 0xba, 0x30, 0xb4, 0xef}
	if len(c.code) != 3276 || sha256.Sum256(c.code) != wantCode {
		return false
	}
	m := rv64NativeTailModule(c)
	if m == nil || len(m.Plans) != 1 {
		return false
	}
	p := m.Plans[0]
	if p.Kind != encoder.NativeTailIndirect || p.TypeIndex != 0 || p.FunctionIndex != 2 || p.ImportIndex != 0 || p.ReferenceBase != 56 || p.SourcePC != 3 || p.FrameBytes != 64 || p.SourceBase != 56 || p.SourceStride != 8 {
		return false
	}
	var functions [2]bool
	for _, index := range c.Exports {
		if index < 1 || index > 2 {
			return false
		}
		functions[index-1] = true
	}
	for _, index := range c.tableExports {
		if index != 0 {
			return false
		}
	}
	return functions[0] && functions[1]
}

func rv64OwnedTable64IndirectCommon(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64IndirectABI || !c.TableAddr64 || !c.TableHasMax || c.tableCount() != 1 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) == 0 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	target, caller := c.Funcs[0], c.Funcs[1]
	if len(target.Params) != 0 || len(target.Results) != 1 || target.Results[0] != ValI32 || len(caller.Params) != 1 || caller.Params[0] != ValI64 || len(caller.Results) != 1 || caller.Results[0] != ValI32 {
		return false
	}
	if !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations) || c.nativeReferenceFunctionABI || !c.NeedsFuncRefDescs || len(c.FuncTypeID) != 2 {
		return false
	}
	for _, index := range c.Exports {
		if index != 1 {
			return false
		}
	}
	return true
}

func rv64OwnedTwoTable32Indirect(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64IndirectABI || c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 3 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || def.Addr64 || !def.HasMax || def.Size < 0 || def.Size > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < uint64(def.Size) || c.tableRuntimeCapacity(index) < def.Size || c.tableRuntimeCapacity(index) > railshot.MaxOwnedTableEntries {
			return false
		}
		caller := c.Funcs[index+1]
		if len(caller.Params) != 1 || caller.Params[0] != ValI32 || len(caller.Results) != 1 || caller.Results[0] != ValI32 {
			return false
		}
	}
	target := c.Funcs[0]
	if len(target.Params) != 0 || len(target.Results) != 1 || target.Results[0] != ValI32 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations) || c.requiredFeatures.IsEnabled(CoreFeatureTable64) || c.nativeReferenceFunctionABI || !c.NeedsFuncRefDescs || len(c.FuncTypeID) != 3 {
		return false
	}
	var functions [2]bool
	var tables [2]bool
	for _, index := range c.Exports {
		if index < 1 || index > 2 {
			return false
		}
		functions[index-1] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func rv64OwnedTwoTable64Indirect(c *Compiled) bool {
	return rv64OwnedTwoTable64IndirectCall(c) || rv64OwnedTwoTable64IndirectTail(c)
}

func rv64OwnedTwoTable64IndirectTail(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64IndirectABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 5 || len(c.Exports) != 4 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		want := index + 2
		if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || !def.Addr64 || !def.HasMax || def.Size != want || def.Max != uint64(want) || c.tableRuntimeCapacity(index) != want {
			return false
		}
	}
	target := c.Funcs[0]
	targetKey := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}})
	callerKey := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I32}})
	entries := [...]int{0, 592, 1840, 3216, 4656}
	if len(target.Params) != 0 || len(target.Results) != 1 || target.Results[0] != ValI32 || c.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations|CoreFeatureTailCall || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs || len(c.FuncTypeID) != 5 || c.FuncTypeID[0] != targetKey || len(c.Entry) != len(entries) || len(c.InternalEntry) != len(entries) {
		return false
	}
	for i, entry := range entries {
		if c.Entry[i] != entry || c.InternalEntry[i] != entry {
			return false
		}
	}
	for index := 1; index < 5; index++ {
		fn := c.Funcs[index]
		if len(fn.Params) != 1 || fn.Params[0] != ValI64 || len(fn.Results) != 1 || fn.Results[0] != ValI32 || c.FuncTypeID[index] != callerKey {
			return false
		}
	}
	wantCode := [32]byte{0x44, 0x5d, 0xdd, 0x59, 0x50, 0x60, 0x90, 0x83, 0x8a, 0x71, 0xb0, 0x7a, 0xba, 0xfe, 0xbc, 0x94, 0x19, 0x41, 0xe1, 0xfd, 0x79, 0xd9, 0xc4, 0xab, 0xa4, 0xbf, 0xcd, 0xb5, 0xd5, 0x28, 0x4d, 0x69}
	if len(c.code) != 6156 || sha256.Sum256(c.code) != wantCode {
		return false
	}
	m := rv64NativeTailModule(c)
	if m == nil || len(m.Plans) != 2 {
		return false
	}
	for i, plan := range m.Plans {
		if plan.Kind != encoder.NativeTailIndirect || plan.TypeIndex != 0 || plan.FunctionIndex != uint32(i+3) || plan.ImportIndex != 0 || plan.ReferenceBase != 56 || plan.SourcePC != 3 || plan.FrameBytes != 64 || plan.SourceBase != 56 || plan.SourceStride != 8 {
			return false
		}
	}
	var functions [4]bool
	var tables [2]bool
	for _, index := range c.Exports {
		if index < 1 || index > 4 {
			return false
		}
		functions[index-1] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && functions[2] && functions[3] && tables[0] && tables[1]
}

func rv64OwnedTwoTable64IndirectCall(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64IndirectABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 3 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || !def.Addr64 || !def.HasMax || def.Size < 0 || def.Size > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < uint64(def.Size) || c.tableRuntimeCapacity(index) < def.Size || c.tableRuntimeCapacity(index) > railshot.MaxOwnedTableEntries {
			return false
		}
		caller := c.Funcs[index+1]
		if len(caller.Params) != 1 || caller.Params[0] != ValI64 || len(caller.Results) != 1 || caller.Results[0] != ValI32 {
			return false
		}
	}
	target := c.Funcs[0]
	if len(target.Params) != 0 || len(target.Results) != 1 || target.Results[0] != ValI32 || c.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs || len(c.FuncTypeID) != 3 {
		return false
	}
	if rv64NativeTailModule(c) != nil {
		return false
	}
	var functions [2]bool
	var tables [2]bool
	for _, index := range c.Exports {
		if index < 1 || index > 2 {
			return false
		}
		functions[index-1] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func rv64MixedTable64Indirect(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64IndirectABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 3 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imported, ok := c.tableImportAt(0)
	if !ok || imported.Key == "" || !imported.Addr64 || !imported.HasMax || imported.Min > railshot.MaxOwnedTableEntries || imported.Max > railshot.MaxOwnedTableEntries || imported.Max < imported.Min {
		return false
	}
	for index := 0; index < 2; index++ {
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain {
			return false
		}
		caller := c.Funcs[index+1]
		if len(caller.Params) != 1 || caller.Params[0] != ValI64 || len(caller.Results) != 1 || caller.Results[0] != ValI32 {
			return false
		}
	}
	local := c.tableDef(1)
	if local.ImportKey != "" || local.ImportHasMax || local.HasInitFunc || local.InitFunc != 0 || !local.Addr64 || !local.HasMax || local.Size < 0 || local.Size > railshot.MaxOwnedTableEntries || local.Max > railshot.MaxOwnedTableEntries || local.Max < uint64(local.Size) || c.tableRuntimeCapacity(1) < local.Size || c.tableRuntimeCapacity(1) > railshot.MaxOwnedTableEntries {
		return false
	}
	target := c.Funcs[0]
	if len(target.Params) != 0 || len(target.Results) != 1 || target.Results[0] != ValI32 || c.requiredFeatures != CoreFeatureTable64|CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs || len(c.FuncTypeID) != 3 || c.FuncTypeID[0] == 0 || c.FuncTypeID[1] == 0 || c.FuncTypeID[1] != c.FuncTypeID[2] {
		return false
	}
	var functions [2]bool
	var tables [2]bool
	for _, index := range c.Exports {
		if index < 1 || index > 2 {
			return false
		}
		functions[index-1] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func rv64MixedTable64ActiveIndirect(c *Compiled) bool {
	return rv64MixedTable64ActiveIndirectCall(c) || rv64MixedTable64ActiveIndirectTail(c)
}

func rv64MixedTable64ActiveIndirectCall(c *Compiled) bool {
	return rv64MixedTable64ActiveIndirectProduct(c, false, false)
}

func rv64MixedTable64ActiveIndirectTail(c *Compiled) bool {
	return rv64MixedTable64ActiveIndirectProduct(c, true, false)
}

// The native-tail appendix precedes GC descriptors in the artifact. Its plan
// validator may use this exact source/image witness while that one redundant
// descriptor vector is not decoded yet; complete retained admission still
// requires the full classifier above after decoding finishes.
func rv64MixedTable64ActiveIndirectTailPlanShape(c *Compiled) bool {
	return rv64MixedTable64ActiveIndirectProduct(c, true, true)
}

func rv64MixedTable64ActiveIndirectProduct(c *Compiled, tail, beforeGCDescriptors bool) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64IndirectABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 4 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	tailModule := rv64NativeTailModule(c)
	if tail != (tailModule != nil) {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 2 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	imported, ok := c.tableImportAt(0)
	if !ok || imported.Key == "" || !imported.Addr64 || !imported.HasMax || imported.Min != 4 || imported.Max != 4 {
		return false
	}
	local := c.tableDef(1)
	for index := 0; index < 2; index++ {
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain {
			return false
		}
	}
	if local.ImportKey != "" || local.ImportHasMax || local.HasInitFunc || local.InitFunc != 0 || !local.Addr64 || !local.HasMax || local.Size != 4 || local.Max != 4 || c.tableRuntimeCapacity(1) != 4 {
		return false
	}
	i32Key := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}})
	callerKey := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I32}})
	if len(c.Types) != 2 || (!beforeGCDescriptors && len(c.GCTypeDescs) != 2) || (beforeGCDescriptors && len(c.GCTypeDescs) != 0 && len(c.GCTypeDescs) != 2) || len(c.ValueTypes) != 1 || c.ValueTypes[0] != plain || len(c.FuncTypeID) != 4 || len(c.Entry) != 4 || len(c.InternalEntry) != 4 {
		return false
	}
	for _, desc := range c.GCTypeDescs {
		if desc.Kind != gc.KindFunc || !desc.Final || desc.HasSuper {
			return false
		}
	}
	if !tail && (c.Entry[0] != 0 || c.Entry[1] != 592 || c.Entry[2] != 1184 || c.Entry[3] != 4032 || c.InternalEntry[0] != 0 || c.InternalEntry[1] != 592 || c.InternalEntry[2] != 1184 || c.InternalEntry[3] != 4032) {
		return false
	}
	if tail && (c.Entry[0] != 0 || c.Entry[1] != 592 || c.Entry[2] != 1184 || c.Entry[3] != 2624 || c.InternalEntry[0] != 0 || c.InternalEntry[1] != 592 || c.InternalEntry[2] != 1184 || c.InternalEntry[3] != 2624) {
		return false
	}
	for i, fn := range c.Funcs {
		if i < 2 {
			if len(fn.Params) != 0 || len(fn.Results) != 1 || fn.Results[0] != ValI32 || c.FuncTypeID[i] != i32Key {
				return false
			}
		} else if len(fn.Params) != 1 || fn.Params[0] != ValI64 || len(fn.Results) != 1 || fn.Results[0] != ValI32 || c.FuncTypeID[i] != callerKey {
			return false
		}
	}
	wantCode := [32]byte{0x32, 0x5f, 0xde, 0x8d, 0x64, 0xeb, 0x6d, 0x83, 0x35, 0x27, 0x29, 0xcb, 0xa1, 0x58, 0x0d, 0x2f, 0x06, 0xf1, 0xf3, 0xb3, 0xb5, 0x16, 0xa2, 0x11, 0x39, 0x35, 0xb2, 0xd6, 0x00, 0x75, 0xb7, 0x28}
	if tail {
		wantCode = [32]byte{0x3b, 0xa0, 0x4d, 0x66, 0xb9, 0x27, 0xc3, 0x70, 0x5d, 0xff, 0xc3, 0x22, 0xb8, 0x38, 0xfd, 0x8b, 0x2d, 0x8a, 0x67, 0xbe, 0xaa, 0xb2, 0x9e, 0x2f, 0x66, 0xe0, 0x6e, 0xd4, 0x97, 0x4f, 0x88, 0xfe}
	}
	expectedFeatures := CoreFeatureTable64 | CoreFeatureReferenceTypes
	if tail {
		expectedFeatures |= CoreFeatureTailCall
	}
	wantCodeLen := 6944
	if tail {
		wantCodeLen = 4124
	}
	if len(c.code) != wantCodeLen || sha256.Sum256(c.code) != wantCode || c.requiredFeatures != expectedFeatures || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !c.NeedsFuncRefDescs {
		return false
	}
	if tail {
		if len(tailModule.Plans) != 2 {
			return false
		}
		for i, plan := range tailModule.Plans {
			if plan.Kind != encoder.NativeTailIndirect || plan.TypeIndex != 0 || plan.FunctionIndex != uint32(i+2) || plan.ImportIndex != 0 || plan.ReferenceBase != 56 || plan.SourcePC != 3 || plan.FrameBytes != 64 || plan.SourceBase != 56 || plan.SourceStride != 8 {
				return false
			}
		}
	}
	for i, e := range c.Elems {
		if e.Mode != ElemModeActive || e.TableIndex != uint32(i) || e.RefType != ValFuncRef || !e.HasValueType || e.Offset.Base != 0 || e.Offset.HasGlobal || !bytes.Equal(e.Offset.Expr, []byte{0x42, 1, 0x0b}) || len(e.Values) != 2 {
			return false
		}
		elemExact, err := c.elemExactType(e)
		if err != nil || elemExact != plain {
			return false
		}
		for target, v := range e.Values {
			if v.Null || v.FuncIndex != uint32(target) || v.GlobalIndex != 0 || v.HasGlobal || v.I31Wrap || v.RepeatPrevious || len(v.Expr) != 0 {
				return false
			}
		}
	}
	var functions, tables [2]bool
	for _, index := range c.Exports {
		if index < 2 || index > 3 {
			return false
		}
		functions[index-2] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && tables[0] && tables[1]
}

func rv64ImportedTable64Indirect(c *Compiled) bool {
	return rv64ImportedTable64IndirectCall(c) || rv64ImportedTable64IndirectTail(c)
}

func rv64ImportedTable64IndirectCall(c *Compiled) bool {
	if rv64NativeTailModule(c) != nil {
		return false
	}
	return rv64ImportedTable64IndirectCommon(c)
}

func rv64ImportedTable64IndirectTail(c *Compiled) bool {
	return rv64ImportedTable64IndirectTailProduct(c, false)
}

// The native-tail appendix precedes GC descriptors in an artifact. Admit the
// same exact product while that redundant descriptor vector is still absent;
// complete retained admission requires the full classifier after decoding.
func rv64ImportedTable64IndirectTailPlanShape(c *Compiled) bool {
	return rv64ImportedTable64IndirectTailProduct(c, true)
}

func rv64ImportedTable64IndirectTailProduct(c *Compiled, beforeGCDescriptors bool) bool {
	if !rv64ImportedTable64IndirectCommon(c) {
		return false
	}
	def, _ := c.tableImportAt(0)
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	i32, _ := valueTypeDescriptorFromValType(ValI32)
	i64, _ := valueTypeDescriptorFromValType(ValI64)
	callerKey := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I32}})
	exact, err := c.tableExactType(0)
	if def.Min != 1 || def.Max != 4 || err != nil || exact != plain || len(c.Types) != 2 || len(c.ValueTypes) != 1 || c.ValueTypes[0] != plain || len(c.FuncTypeID) != 1 || c.FuncTypeID[0] != callerKey || len(c.Entry) != 1 || len(c.InternalEntry) != 1 || c.Entry[0] != 0 || c.InternalEntry[0] != 0 || len(c.Exports) != 1 || len(c.tableExports) != 1 {
		return false
	}
	if (!beforeGCDescriptors && len(c.GCTypeDescs) != 2) || (beforeGCDescriptors && len(c.GCTypeDescs) != 0 && len(c.GCTypeDescs) != 2) {
		return false
	}
	for _, desc := range c.GCTypeDescs {
		if desc.Kind != gc.KindFunc || !desc.Final || desc.HasSuper {
			return false
		}
	}
	for i, d := range c.Types {
		if d.RecGroup != uint32(i) || !d.Final || len(d.Supers) != 0 || d.HasDescribes || d.HasDescriptor || d.Kind != CompositeTypeFunction || len(d.Fields) != 0 || d.Array != (FieldTypeDescriptor{}) || len(d.Results) != 1 || d.Results[0] != i32 || i == 0 && len(d.Params) != 0 || i == 1 && (len(d.Params) != 1 || d.Params[0] != i64) {
			return false
		}
	}
	if c.requiredFeatures != CoreFeatureTable64|CoreFeatureTailCall || c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI {
		return false
	}
	wantCode := [32]byte{0xa9, 0x67, 0xe6, 0x62, 0xfd, 0xd6, 0x5c, 0x25, 0x37, 0x1d, 0xd0, 0x96, 0x73, 0x16, 0x14, 0x00, 0x7c, 0x39, 0x98, 0x6d, 0xa3, 0x11, 0x30, 0xa1, 0x7b, 0xa1, 0xbd, 0xa4, 0x5c, 0xbf, 0x47, 0xae}
	if len(c.code) != 1436 || sha256.Sum256(c.code) != wantCode {
		return false
	}
	tailModule := rv64NativeTailModule(c)
	if tailModule == nil || len(tailModule.Plans) != 1 {
		return false
	}
	p := tailModule.Plans[0]
	if p.Kind != encoder.NativeTailIndirect || p.TypeIndex != 0 || p.FunctionIndex != 0 || p.ImportIndex != 0 || p.ReferenceBase != 56 || p.SourcePC != 3 || p.FrameBytes != 64 || p.SourceBase != 56 || p.SourceStride != 8 {
		return false
	}
	for _, index := range c.Exports {
		if index != 0 {
			return false
		}
	}
	for _, index := range c.tableExports {
		if index != 0 {
			return false
		}
	}
	return true
}

func rv64ImportedTable64IndirectCommon(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64IndirectABI || !c.TableAddr64 || c.tableCount() != 1 || c.tableImportCount() != 1 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 1 || len(c.Exports) == 0 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	def, imported := c.tableImportAt(0)
	if !imported || def.Key == "" || !def.Addr64 || !def.HasMax || def.Min > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < def.Min {
		return false
	}
	caller := c.Funcs[0]
	if len(caller.Params) != 1 || caller.Params[0] != ValI64 || len(caller.Results) != 1 || caller.Results[0] != ValI32 {
		return false
	}
	if c.nativeReferenceFunctionABI || !c.NeedsFuncRefDescs || len(c.FuncTypeID) != 1 {
		return false
	}
	for _, index := range c.Exports {
		if index != 0 {
			return false
		}
	}
	return true
}

func rv64OwnedTable64Fill(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64FillABI || !c.TableAddr64 || !c.TableHasMax || c.tableCount() != 1 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 1 || len(c.Exports) == 0 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	sig := c.Funcs[0]
	if len(sig.Params) != 2 || sig.Params[0] != ValI64 || sig.Params[1] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI32 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
		return false
	}
	for _, index := range c.Exports {
		if index != 0 {
			return false
		}
	}
	return true
}

func rv64OwnedTable64Copy(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64CopyABI || !c.TableAddr64 || !c.TableHasMax || c.tableCount() != 1 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 1 || len(c.Exports) == 0 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	sig := c.Funcs[0]
	if len(sig.Params) != 3 || sig.Params[0] != ValI64 || sig.Params[1] != ValI64 || sig.Params[2] != ValI64 || len(sig.Results) != 1 || sig.Results[0] != ValI32 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations) {
		return false
	}
	for _, index := range c.Exports {
		if index != 0 {
			return false
		}
	}
	return true
}

func rv64OwnedTable64InitDrop(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64InitDropABI || !c.TableAddr64 || !c.TableHasMax || c.tableCount() != 1 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) < 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 1 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	init, drop := c.Funcs[0], c.Funcs[1]
	if len(init.Params) != 3 || init.Params[0] != ValI64 || init.Params[1] != ValI32 || init.Params[2] != ValI32 || len(init.Results) != 1 || init.Results[0] != ValI32 || len(drop.Params) != 0 || len(drop.Results) != 1 || drop.Results[0] != ValI32 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations) {
		return false
	}
	e := c.passiveElems[0]
	if e.Mode != ElemModePassive || e.TableIndex != 0 || e.RefType != ValFuncRef || !e.HasValueType || e.Offset.Base != 0 || e.Offset.HasGlobal || len(e.Offset.Expr) != 0 || len(e.Values) != 1 {
		return false
	}
	v := e.Values[0]
	if !v.Null || v.FuncIndex != 0 || v.GlobalIndex != 0 || v.HasGlobal || v.I31Wrap || v.RepeatPrevious || len(v.Expr) != 0 {
		return false
	}
	seen := [2]bool{}
	for _, index := range c.Exports {
		if index < 0 || index > 1 {
			return false
		}
		seen[index] = true
	}
	return seen[0] && seen[1]
}

func rv64OwnedTwoTable64InitDrop(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64InitDropABI || !c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 3 || len(c.Exports) != 3 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 1 || c.memoryDir != nil && len(c.memoryDir.ehTags) != 0 {
		return false
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for index := 0; index < 2; index++ {
		def := c.tableDef(index)
		exact, err := c.tableExactType(index)
		if err != nil || exact != plain || def.ImportKey != "" || def.HasInitFunc || def.InitFunc != 0 || !def.Addr64 || !def.HasMax || def.Size < 0 || def.Size > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.Max < uint64(def.Size) || c.tableRuntimeCapacity(index) < def.Size || c.tableRuntimeCapacity(index) > railshot.MaxOwnedTableEntries {
			return false
		}
		init := c.Funcs[index]
		if len(init.Params) != 3 || init.Params[0] != ValI64 || init.Params[1] != ValI32 || init.Params[2] != ValI32 || len(init.Results) != 1 || init.Results[0] != ValI32 {
			return false
		}
	}
	drop := c.Funcs[2]
	if len(drop.Params) != 0 || len(drop.Results) != 1 || drop.Results[0] != ValI32 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations) {
		return false
	}
	e := c.passiveElems[0]
	if e.Mode != ElemModePassive || e.TableIndex != 0 || e.RefType != ValFuncRef || !e.HasValueType || e.Offset.Base != 0 || e.Offset.HasGlobal || len(e.Offset.Expr) != 0 || len(e.Values) != 1 {
		return false
	}
	elementType, err := c.elemExactType(e)
	if err != nil || elementType != plain {
		return false
	}
	v := e.Values[0]
	if !v.Null || v.FuncIndex != 0 || v.GlobalIndex != 0 || v.HasGlobal || v.I31Wrap || v.RepeatPrevious || len(v.Expr) != 0 {
		return false
	}
	var functions [3]bool
	var tables [2]bool
	for _, index := range c.Exports {
		if index < 0 || index > 2 {
			return false
		}
		functions[index] = true
	}
	for _, index := range c.tableExports {
		if index < 0 || index > 1 {
			return false
		}
		tables[index] = true
	}
	return functions[0] && functions[1] && functions[2] && tables[0] && tables[1]
}

func rv64QualifiedTable64(c *Compiled) bool {
	return rv64StateOnlyTable64(c) || rv64OwnedTable64ActiveElement(c) || rv64MixedTable64ActiveElement(c) || rv64MixedTable64DualActiveElements(c) || rv64MixedTable64DualActiveVectors(c) || rv64OwnedTable64Size(c) || rv64OwnedTwoTable64Size(c) || rv64MixedTable64Size(c) || rv64MixedTable64GetSet(c) || rv64MixedTable64Grow(c) || rv64MixedTable64Fill(c) || rv64MixedTable64Copy(c) || rv64MixedTable64CrossCopy(c) || rv64MixedTable64InitDrop(c) || rv64MixedTable64Indirect(c) || rv64MixedTable64ActiveIndirect(c) || rv64OwnedTable64Grow(c) || rv64OwnedTwoTable64Grow(c) || rv64OwnedTable64GetSet(c) || rv64OwnedTwoTable64GetSet(c) || rv64OwnedTable64Indirect(c) || rv64OwnedTwoTable64Indirect(c) || rv64ImportedTable64Indirect(c) || rv64OwnedTable64Fill(c) || rv64OwnedTwoTable64Fill(c) || rv64OwnedTable64Copy(c) || rv64OwnedTwoTable64Copy(c) || rv64OwnedTable64InitDrop(c) || rv64OwnedTwoTable64InitDrop(c) || rv64ImportedTable64Size(c) || rv64ImportedTable64GetSet(c) || rv64ImportedTable64Grow(c) || rv64ImportedTable64Fill(c) || rv64ImportedTable64Copy(c) || rv64ImportedTable64InitDrop(c)
}

// One bounded owned or imported table is qualified. Exact metadata prevents the legacy
// ExportedTable fallback from exposing an undeclared table handle.
func rv64ValidateOwnedTable(c *Compiled) error {
	if rv64Core3RefIsNullExecution(c) || rv64I31OfficialTable(c) || rv64I31OfficialTableGlobalInitializer(c) || rv64I31OfficialAnyTable(c) || rv64I31RefEqPilot(c) || rv64OfficialGCRefEq(c) || rv64GCArrayReference(c) || rv64GCArrayInitElem(c) {
		return nil
	}
	if c.tableCount() == 0 {
		if c.HasTableInitFunc || c.TableInitFunc != 0 || c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || c.TableAddr64 || c.TableType != 0 || c.TableHasValueType || len(c.Elems) != 0 || len(c.tableExports) != 0 {
			return fmt.Errorf("table metadata requires a declared table")
		}
		if len(c.passiveElems) > railshot.MaxOwnedElementSegments || len(c.passiveElems) != 0 && !c.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) {
			return fmt.Errorf("element directory count/feature")
		}
		remaining := railshot.MaxOwnedTableEntries
		hasLegacyPassive := false
		hasLegacyDescriptorPayload := false
		for _, e := range c.passiveElems {
			if (e.Mode != ElemModeDeclarative && e.Mode != ElemModePassive) || e.TableIndex != 0 || e.RefType != ValFuncRef && e.RefType != 0 || len(e.Values) > remaining || e.Offset.Base != 0 || e.Offset.HasGlobal || len(e.Offset.Expr) != 0 {
				return fmt.Errorf("bounded table-free passive/declarative funcref state required")
			}
			if e.Mode == ElemModeDeclarative && len(e.Values) != 0 {
				return fmt.Errorf("declarative state must start dropped")
			}
			remaining -= len(e.Values)
			if err := rv64ValidateElementPayload(c, e); err != nil {
				return err
			}
			hasLegacyPassive = hasLegacyPassive || e.Mode == ElemModePassive && !e.HasValueType
			hasLegacyDescriptorPayload = hasLegacyDescriptorPayload || e.Mode == ElemModePassive && !e.HasValueType && len(e.Values) != 0
		}
		hasReferenceGlobal := false
		for _, global := range c.Globals {
			hasReferenceGlobal = hasReferenceGlobal || global.Type == ValFuncRef
		}
		if !hasReferenceGlobal && !c.needsFuncRefDescs() && len(c.passiveElems) == 0 {
			return nil
		}
		if !c.nativeTableContextABI || !c.nativeTableReferenceABI || !c.registerABIDisabled {
			return fmt.Errorf("references require the framed retained reference ABI")
		}
		// Legacy passive function-index payloads belong to bulk memory and
		// materialize descriptors even without reference instructions. Explicit
		// reference globals, expression payloads and descriptor-only bodies
		// still require their ReferenceTypes witness.
		if (hasReferenceGlobal || !hasLegacyPassive || c.needsFuncRefDescs() && !hasLegacyDescriptorPayload) && !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
			return fmt.Errorf("references require the reference-types feature")
		}
		if c.needsFuncRefDescs() {
			return rv64ValidateReferenceFunctionKeys(c)
		}
		return nil
	}
	if !c.nativeTableContextABI || !c.nativeTableReferenceABI {
		return fmt.Errorf("retained native table context and reference ABI are required")
	}
	stateOnlyLocalDeclarations := c.hasNoFunctionCode() && c.tableImportCount() == 0 && len(c.GlobalImports) == 0 && c.memoryImportCount() == 0 && c.tagImportCount() == 0
	if stateOnlyLocalDeclarations && len(c.Globals) != 0 && !rv64StateOnlyTable32ScalarGlobals(c) {
		return fmt.Errorf("table/scalar globals require the exact state-only literal profile")
	}
	qualifiedTable64 := rv64QualifiedTable64(c)
	qualifiedMultiTable := rv64MixedTable64ActiveElement(c) || rv64MixedTable64DualActiveElements(c) || rv64MixedTable64DualActiveVectors(c) || rv64StateOnlyTwoTable32(c) || rv64OwnedTwoTable32Size(c) || rv64OwnedTwoTable32GetSet(c) || rv64OwnedTwoTable32Grow(c) || rv64OwnedTwoTable32Fill(c) || rv64OwnedTwoTable32Copy(c) || rv64OwnedTwoTable32InitDrop(c) || rv64OwnedTwoTable32Indirect(c) || rv64OwnedTwoTable64Size(c) || rv64MixedTable64Size(c) || rv64MixedTable64GetSet(c) || rv64MixedTable64Grow(c) || rv64MixedTable64Fill(c) || rv64MixedTable64Copy(c) || rv64MixedTable64CrossCopy(c) || rv64MixedTable64InitDrop(c) || rv64MixedTable64Indirect(c) || rv64MixedTable64ActiveIndirect(c) || rv64OwnedTwoTable64GetSet(c) || rv64OwnedTwoTable64Grow(c) || rv64OwnedTwoTable64Fill(c) || rv64OwnedTwoTable64Copy(c) || rv64OwnedTwoTable64InitDrop(c) || rv64OwnedTwoTable64Indirect(c)
	qualifiedGCRefTable := rv64GCRefTestTable(c) || rv64GCRefTestConcrete(c) || rv64GCRefTestAbstract(c) || rv64GCExtern(c) || rv64GCRefCastAbstract(c)
	if c.tableCount() != 1 && !qualifiedMultiTable && !qualifiedGCRefTable || !c.hasTableExportMetadata || len(c.tableExports) > railshot.MaxFunctions || !c.registerABIDisabled || c.TableAddr64 && !qualifiedTable64 || (c.TableType != 0 && c.TableType != ValFuncRef && !qualifiedGCRefTable) {
		return fmt.Errorf("one framed nullable funcref table32 or exact qualified table64/multi-table profile required")
	}
	if c.TableAddr64 && !c.requiredFeatures.IsEnabled(CoreFeatureTable64) {
		return fmt.Errorf("table64 requires exact feature metadata")
	}
	if qualifiedGCRefTable {
		return nil
	}
	if qualifiedMultiTable {
		return rv64ValidateReferenceFunctionKeys(c)
	}
	if c.HasTableInitFunc {
		if c.tableImportCount() != 0 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || uint64(c.TableInitFunc) >= uint64(c.NumImports+len(c.Funcs)) {
			return fmt.Errorf("table initializer requires owned storage, reference metadata and a valid function")
		}
	}
	for _, index := range c.tableExports {
		if index != 0 {
			return fmt.Errorf("owned table export index must be zero")
		}
	}
	exact, err := c.tableExactType(0)
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	if err != nil || exact != plain {
		return fmt.Errorf("private table requires exact nullable abstract funcref")
	}
	// Check uint64 before runtime capacity narrows to int.
	if c.TableSize < 0 || c.TableSize > railshot.MaxOwnedTableEntries || c.TableMax > railshot.MaxOwnedTableEntries || (c.TableHasMax && c.TableMax < uint64(c.TableSize)) || c.tableRuntimeCapacity(0) < c.TableSize || c.tableRuntimeCapacity(0) > railshot.MaxOwnedTableEntries {
		return fmt.Errorf("private table capacity bound")
	}
	if def, imported := c.tableImportAt(0); imported {
		if c.TableSize != 0 || c.TableMax != 0 || c.TableHasMax || def.Min > railshot.MaxOwnedTableEntries || def.Max > railshot.MaxOwnedTableEntries || def.HasMax && def.Max < def.Min {
			return fmt.Errorf("bounded imported table declaration with no local limits required")
		}
	}
	if len(c.Elems) > railshot.MaxOwnedElementSegments || len(c.passiveElems) > railshot.MaxOwnedElementSegments {
		return fmt.Errorf("element descriptor bound")
	}
	remaining := railshot.MaxOwnedTableEntries
	activeOffsetType := ValI32
	if rv64OwnedTable64ActiveElement(c) || rv64MixedTable64ActiveElement(c) {
		activeOffsetType = ValI64
	}

	for _, e := range c.Elems {
		if e.Mode != ElemModeActive || e.TableIndex != 0 || (e.RefType != 0 && e.RefType != ValFuncRef) || len(e.Values) > remaining {
			return fmt.Errorf("bounded active funcref elements required")
		}
		if len(e.Offset.Expr) != 0 {
			if e.Offset.HasGlobal {
				return fmt.Errorf("conflicting element initializer")
			}
			if err := rv64ValidateIntegerInitializer(c, e.Offset.Expr, activeOffsetType, len(c.Globals)); err != nil {
				return err
			}
		}
		if e.Offset.HasGlobal && (e.Offset.Base != 0 || !rv64InitializerGlobal(c, e.Offset.Global, activeOffsetType, len(c.Globals))) {
			return fmt.Errorf("element offset requires exact available immutable table index")
		}
		remaining -= len(e.Values)
		if err := rv64ValidateElementPayload(c, e); err != nil {
			return err
		}
	}
	if len(c.passiveElems) != 0 && !c.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) {
		return fmt.Errorf("element state requires bulk feature metadata")
	}
	for _, e := range c.passiveElems {
		if (e.Mode != ElemModeActive && e.Mode != ElemModePassive && e.Mode != ElemModeDeclarative) || e.TableIndex != 0 || (e.RefType != 0 && e.RefType != ValFuncRef) || e.Offset.Base != 0 || e.Offset.HasGlobal || len(e.Offset.Expr) != 0 || len(e.Values) > remaining {
			return fmt.Errorf("bounded private element state required")
		}
		if e.Mode != ElemModePassive && len(e.Values) != 0 {
			return fmt.Errorf("inactive element state must start dropped")
		}
		remaining -= len(e.Values)
		if err := rv64ValidateElementPayload(c, e); err != nil {
			return err
		}
	}
	return rv64ValidateReferenceFunctionKeys(c)
}

func rv64ValidateElementPayload(c *Compiled, e ElemInit) error {
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	if e.HasValueType {
		exact, err := c.elemExactType(e)
		if err != nil || exact != plain || e.RefType != ValFuncRef || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
			return fmt.Errorf("expression elements require exact nullable funcref metadata")
		}
	}
	for _, v := range e.Values {
		if v.I31Wrap || v.RepeatPrevious || len(v.Expr) != 0 {
			return fmt.Errorf("element requires structural null/ref.func/global payload")
		}
		if v.HasGlobal {
			if (e.Mode != ElemModeActive && e.Mode != ElemModePassive) || !e.HasValueType || v.Null || v.FuncIndex != 0 || !rv64InitializerGlobal(c, int(v.GlobalIndex), ValFuncRef, len(c.Globals)) {
				return fmt.Errorf("element global requires an available immutable funcref and expression metadata")
			}
			continue
		}
		if v.GlobalIndex != 0 {
			return fmt.Errorf("element global index without global initializer")
		}
		if v.Null {
			if !e.HasValueType || v.FuncIndex != 0 {
				return fmt.Errorf("element null requires expression metadata")
			}
		} else if !c.needsFuncRefDescs() || uint64(v.FuncIndex) >= uint64(c.NumImports+len(c.Funcs)) {
			return fmt.Errorf("element requires a valid function index")
		}
	}
	return nil
}

func rv64ValidateReferenceFunctionKeys(c *Compiled) error {
	if len(c.FuncTypeID) != c.NumImports+len(c.Funcs) {
		return fmt.Errorf("private table signature-key count")
	}
	var params, results [256]wasm.ValType
	for i := range c.FuncTypeID {
		if railshotNonCallableFunction(c, i) {
			continue // Source-attested internal signature has no callable row.
		}
		publicReferenceCarrier := i == 0 && rv64PublicReferenceCallBoundary(c) && rv64PublicReferenceCarrierSignature(c.Funcs[0])
		publicDynamicHostReferenceCarrier := i <= 1 && rv64PublicDynamicHostReferenceBoundary(c)
		if publicReferenceCarrier || publicDynamicHostReferenceCarrier {
			sig := c.Funcs[0]
			if publicDynamicHostReferenceCarrier && i == 0 {
				sig = c.importFuncSigs[0]
			}
			for j, t := range sig.Params {
				params[j] = rv64ReferenceWasmType(t)
			}
			for j, t := range sig.Results {
				results[j] = rv64ReferenceWasmType(t)
			}
			key := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: params[:len(sig.Params)], Results: results[:len(sig.Results)]})
			if c.FuncTypeID[i] != key {
				return fmt.Errorf("public reference-carrier target signature key differs from its exact type")
			}
			continue
		}
		var sig FuncSig
		if i < c.NumImports {
			sig = c.importFuncSigs[i]
		} else {
			sig = c.Funcs[i-c.NumImports]
		}
		for j, t := range sig.Params {
			params[j] = rv64NumericWasmType(t)
		}
		for j, t := range sig.Results {
			results[j] = rv64NumericWasmType(t)
		}
		key := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: params[:len(sig.Params)], Results: results[:len(sig.Results)]})
		if c.FuncTypeID[i] != key {
			return fmt.Errorf("private table signature key differs from numeric type")
		}
	}
	return nil
}

func rv64NumericWasmType(t ValType) wasm.ValType {
	switch t {
	case ValI64:
		return wasm.I64
	case ValF32:
		return wasm.F32
	case ValF64:
		return wasm.F64
	case ValV128:
		return wasm.V128
	default:
		return wasm.I32
	}
}

func rv64ReferenceWasmType(t ValType) wasm.ValType {
	switch t {
	case ValFuncRef:
		return wasm.FuncRef
	case ValExternRef:
		return wasm.ExternRef
	default:
		return rv64NumericWasmType(t)
	}
}
