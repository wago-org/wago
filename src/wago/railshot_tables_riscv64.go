//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// One bounded owned or imported table is qualified. Exact metadata prevents the legacy
// ExportedTable fallback from exposing an undeclared table handle.
func rv64ValidateOwnedTable(c *Compiled) error {
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
	if c.tableCount() != 1 || !c.hasTableExportMetadata || len(c.tableExports) > railshot.MaxFunctions || !c.registerABIDisabled || c.TableAddr64 || (c.TableType != 0 && c.TableType != ValFuncRef) {
		return fmt.Errorf("one framed nullable funcref table32 required")
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
