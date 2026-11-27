// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"github.com/wago-org/wago/internal/buildfeatures"
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

// The shared table marker identifies the exact indexed table32 witness to check.
func rv64IndexedTable32InstructionWitness(c *Compiled) bool {
	switch c.nativeTable64ABI {
	case nativeTable64SizeABI:
		return rv64OwnedTwoTable32Size(c)
	case nativeTable64GrowABI:
		return rv64OwnedTwoTable32Grow(c)
	case nativeTable64GetSetABI:
		return rv64OwnedTwoTable32GetSet(c)
	case nativeTable64IndirectABI:
		return buildfeatures.BulkMemoryOperations != 0 && rv64OwnedTwoTable32Indirect(c)
	case nativeTable64FillABI:
		return rv64OwnedTwoTable32Fill(c)
	case nativeTable64CopyABI:
		return buildfeatures.BulkMemoryOperations != 0 && rv64OwnedTwoTable32Copy(c)
	case nativeTable64InitDropABI:
		return buildfeatures.BulkMemoryOperations != 0 && rv64OwnedTwoTable32InitDrop(c)
	default:
		return false
	}
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
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.exceptionTags()) != 0 {
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

func rv64OwnedTwoTable32Size(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64SizeABI || c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.exceptionTags()) != 0 {
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

func rv64OwnedTwoTable32Grow(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64GrowABI || c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.exceptionTags()) != 0 {
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

func rv64OwnedTwoTable32GetSet(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64GetSetABI || c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.exceptionTags()) != 0 {
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

func rv64OwnedTwoTable32Indirect(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64IndirectABI || c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 3 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.exceptionTags()) != 0 {
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

func rv64OwnedTwoTable32Fill(c *Compiled) bool {
	if c == nil || c.nativeTable64ABI != nativeTable64FillABI || c.TableAddr64 || c.tableCount() != 2 || c.tableImportCount() != 0 || c.NumImports != 0 || len(c.Imports) != 0 || len(c.Funcs) != 2 || len(c.Exports) != 2 || len(c.tableExports) != 2 {
		return false
	}
	if c.memoryCount() != 0 || len(c.Globals) != 0 || len(c.GlobalImports) != 0 || c.tagImportCount() != 0 || c.HasStart || c.HasTableInitFunc || c.TableInitFunc != 0 {
		return false
	}
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.exceptionTags()) != 0 {
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
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryDir != nil && len(c.memoryDir.exceptionTags()) != 0 {
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
	if c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 1 || c.memoryDir != nil && len(c.memoryDir.exceptionTags()) != 0 {
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
