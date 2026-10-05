//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// This stage owns one private table. Exact export metadata prevents the legacy
// ExportedTable fallback from making it host-mutable despite zero named exports.
func rv64ValidateOwnedTable(c *Compiled) error {
	if c.tableCount() == 0 {
		if c.needsFuncRefDescs() || len(c.Elems) != 0 || len(c.passiveElems) != 0 || len(c.tableExports) != 0 {
			return fmt.Errorf("reference descriptors require the private table")
		}
		return nil
	}
	if c.tableCount() != 1 || c.tableImportCount() != 0 || !c.hasTableExportMetadata || len(c.tableExports) != 0 || !c.registerABIDisabled || c.TableAddr64 || c.HasTableInitFunc || (c.TableType != 0 && c.TableType != ValFuncRef) {
		return fmt.Errorf("one private framed default-null funcref table32 required")
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
	if len(c.Elems) > railshot.MaxOwnedElementSegments || len(c.passiveElems) > railshot.MaxOwnedElementSegments {
		return fmt.Errorf("element descriptor bound")
	}
	remaining := railshot.MaxOwnedTableEntries
	for _, e := range c.Elems {
		if e.Mode != ElemModeActive || e.TableIndex != 0 || e.HasValueType || (e.RefType != 0 && e.RefType != ValFuncRef) || e.Offset.HasGlobal || len(e.Offset.Expr) != 0 || len(e.Values) > remaining {
			return fmt.Errorf("bounded active legacy function-index elements required")
		}
		remaining -= len(e.Values)
		for _, v := range e.Values {
			if v.Null || v.HasGlobal || v.I31Wrap || v.RepeatPrevious || len(v.Expr) != 0 || uint64(v.FuncIndex) >= uint64(len(c.Funcs)) {
				return fmt.Errorf("element requires a defined local function")
			}
		}
	}
	if len(c.passiveElems) != 0 && !c.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) {
		return fmt.Errorf("element state requires bulk feature metadata")
	}
	for _, e := range c.passiveElems {
		if (e.Mode != ElemModeActive && e.Mode != ElemModePassive && e.Mode != ElemModeDeclarative) || e.TableIndex != 0 || e.HasValueType || (e.RefType != 0 && e.RefType != ValFuncRef) || e.Offset.Base != 0 || e.Offset.HasGlobal || len(e.Offset.Expr) != 0 || len(e.Values) > remaining {
			return fmt.Errorf("bounded private element state required")
		}
		if e.Mode != ElemModePassive && len(e.Values) != 0 {
			return fmt.Errorf("inactive element state must start dropped")
		}
		remaining -= len(e.Values)
		for _, v := range e.Values {
			if v.Null || v.HasGlobal || v.I31Wrap || v.RepeatPrevious || len(v.Expr) != 0 || uint64(v.FuncIndex) >= uint64(len(c.Funcs)) {
				return fmt.Errorf("element state requires a defined local function")
			}
		}
	}
	if len(c.FuncTypeID) != len(c.Funcs) {
		return fmt.Errorf("private table signature-key count")
	}
	var params, results [256]wasm.ValType
	for i, sig := range c.Funcs {
		for j, t := range sig.Params {
			params[j] = rv64ScalarWasmType(t)
		}
		for j, t := range sig.Results {
			results[j] = rv64ScalarWasmType(t)
		}
		key := wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: params[:len(sig.Params)], Results: results[:len(sig.Results)]})
		if c.FuncTypeID[i] != key {
			return fmt.Errorf("private table signature key differs from scalar type")
		}
	}
	return nil
}

func rv64ScalarWasmType(t ValType) wasm.ValType {
	switch t {
	case ValI64:
		return wasm.I64
	case ValF32:
		return wasm.F32
	case ValF64:
		return wasm.F64
	default:
		return wasm.I32
	}
}
