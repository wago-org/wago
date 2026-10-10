//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
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
	if len(target.Params) != 0 || len(target.Results) != 1 || target.Results[0] != ValI32 || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes|CoreFeatureBulkMemoryOperations) || c.nativeReferenceFunctionABI || !c.NeedsFuncRefDescs || len(c.FuncTypeID) != 3 {
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

func rv64ImportedTable64Indirect(c *Compiled) bool {
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
	return rv64StateOnlyTable64(c) || rv64OwnedTable64Size(c) || rv64OwnedTwoTable64Size(c) || rv64OwnedTable64Grow(c) || rv64OwnedTwoTable64Grow(c) || rv64OwnedTable64GetSet(c) || rv64OwnedTwoTable64GetSet(c) || rv64OwnedTable64Indirect(c) || rv64OwnedTwoTable64Indirect(c) || rv64ImportedTable64Indirect(c) || rv64OwnedTable64Fill(c) || rv64OwnedTwoTable64Fill(c) || rv64OwnedTable64Copy(c) || rv64OwnedTwoTable64Copy(c) || rv64OwnedTable64InitDrop(c) || rv64OwnedTwoTable64InitDrop(c) || rv64ImportedTable64Size(c) || rv64ImportedTable64GetSet(c) || rv64ImportedTable64Grow(c) || rv64ImportedTable64Fill(c) || rv64ImportedTable64Copy(c) || rv64ImportedTable64InitDrop(c)
}

// One bounded owned or imported table is qualified. Exact metadata prevents the legacy
// ExportedTable fallback from exposing an undeclared table handle.
func rv64ValidateOwnedTable(c *Compiled) error {
	if rv64Core3RefIsNullExecution(c) {
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
	qualifiedMultiTable := rv64StateOnlyTwoTable32(c) || rv64OwnedTwoTable32Size(c) || rv64OwnedTwoTable32GetSet(c) || rv64OwnedTwoTable32Grow(c) || rv64OwnedTwoTable32Fill(c) || rv64OwnedTwoTable32Copy(c) || rv64OwnedTwoTable32InitDrop(c) || rv64OwnedTwoTable32Indirect(c) || rv64OwnedTwoTable64Size(c) || rv64OwnedTwoTable64GetSet(c) || rv64OwnedTwoTable64Grow(c) || rv64OwnedTwoTable64Fill(c) || rv64OwnedTwoTable64Copy(c) || rv64OwnedTwoTable64InitDrop(c) || rv64OwnedTwoTable64Indirect(c)
	if c.tableCount() != 1 && !qualifiedMultiTable || !c.hasTableExportMetadata || len(c.tableExports) > railshot.MaxFunctions || !c.registerABIDisabled || c.TableAddr64 && !qualifiedTable64 || (c.TableType != 0 && c.TableType != ValFuncRef) {
		return fmt.Errorf("one framed nullable funcref table32 or exact qualified table64/multi-table profile required")
	}
	if c.TableAddr64 && !c.requiredFeatures.IsEnabled(CoreFeatureTable64) {
		return fmt.Errorf("table64 requires exact feature metadata")
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

	for _, e := range c.Elems {
		if e.Mode != ElemModeActive || e.TableIndex != 0 || (e.RefType != 0 && e.RefType != ValFuncRef) || len(e.Values) > remaining {
			return fmt.Errorf("bounded active funcref elements required")
		}
		if len(e.Offset.Expr) != 0 {
			if e.Offset.HasGlobal {
				return fmt.Errorf("conflicting element initializer")
			}
			if err := rv64ValidateIntegerInitializer(c, e.Offset.Expr, ValI32, len(c.Globals)); err != nil {
				return err
			}
		}
		if e.Offset.HasGlobal && (e.Offset.Base != 0 || !rv64InitializerGlobal(c, e.Offset.Global, ValI32, len(c.Globals))) {
			return fmt.Errorf("element offset requires exact available immutable i32")
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
