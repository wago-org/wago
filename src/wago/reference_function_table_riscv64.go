//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

// No public/imported table can inject a foreign descriptor into private frames
// or cells. Source confinement is attested separately; retained literals must
// independently exclude every private sparse function row before publication.
func rv64ValidateReferenceFunctionTable(c *Compiled, remaining *int) error {
	reject := func(detail string) error { return fmt.Errorf("RV64 closed reference table: %s", detail) }
	consume := func(n int) bool {
		if n > *remaining {
			return false
		}
		*remaining -= n
		return true
	}
	if rv64GCTypeSubtypingRuntimeTypedTableCall(c) {
		// The exact retained classifier pins the indexed table type, typed active
		// element, two private targets, capacities, and absence of public escape.
		if !consume(64) {
			return reject("runtime typed-table metadata budget")
		}
		return nil
	}
	hasTable := c.tableCount() != 0
	if hasTable {
		plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
		if c.tableCount() != 1 || c.tableImportCount() != 0 || !c.hasTableExportMetadata || len(c.tableExports) != 0 || c.TableAddr64 || c.TableType != ValFuncRef || !c.TableHasValueType || uint64(c.TableValueTypeIndex) >= uint64(len(c.ValueTypes)) || c.ValueTypes[c.TableValueTypeIndex] != plain || !c.HasTableInitFunc && c.TableInitFunc != 0 || !consume(21) {
			return reject("exact single private nullable own table32 required")
		}
		if c.HasTableInitFunc {
			if uint64(c.TableInitFunc) >= uint64(c.NumImports+len(c.Funcs)) || railshotNonCallableFunction(c, int(c.TableInitFunc)) || !consume(5) {
				return reject("numeric table initializer required even at size zero")
			}
		}
		if c.TableSize < 0 || c.TableSize > railshot.MaxOwnedTableEntries || c.TableMax > railshot.MaxOwnedTableEntries || c.TableHasMax && c.TableMax < uint64(c.TableSize) || c.tableRuntimeCapacity(0) < c.TableSize || c.tableRuntimeCapacity(0) > railshot.MaxOwnedTableEntries {
			return reject("owned table capacity bound")
		}
	} else if c.TableHasValueType || c.TableValueTypeIndex != 0 || len(c.Elems) != 0 {
		return reject("unused table metadata")
	}
	if len(c.Elems) > railshot.MaxOwnedElementSegments || len(c.passiveElems) > railshot.MaxOwnedElementSegments {
		return reject("element directory bound")
	}
	plain, _ := valueTypeDescriptorFromValType(ValFuncRef)
	elementType := func(e ElemInit, active bool) bool {
		if e.HasValueType {
			return e.RefType == ValFuncRef && uint64(e.ValueTypeIndex) < uint64(len(c.ValueTypes)) && c.ValueTypes[e.ValueTypeIndex] == plain
		}
		return e.ValueTypeIndex == 0 && (e.RefType == ValFuncRef || !active && e.RefType == 0)
	}
	literal := func(v RefInit, expression bool) bool {
		if v.HasGlobal || v.GlobalIndex != 0 || v.I31Wrap || v.RepeatPrevious || len(v.Expr) != 0 {
			return false
		}
		if v.Null {
			return expression && v.FuncIndex == 0
		}
		return uint64(v.FuncIndex) < uint64(c.NumImports+len(c.Funcs)) && !railshotNonCallableFunction(c, int(v.FuncIndex))
	}
	values := railshot.MaxOwnedTableEntries
	for _, e := range c.Elems {
		if !hasTable || e.Mode != ElemModeActive || e.TableIndex != 0 || !elementType(e, true) || e.Offset.HasGlobal || e.Offset.Global != 0 || len(e.Offset.Expr) != 0 || len(e.Values) > values || !consume(20) || e.HasValueType && !consume(4) || !consume(4*len(e.Values)) {
			return reject("bounded active numeric literal elements required")
		}
		values -= len(e.Values)
		for _, v := range e.Values {
			if !literal(v, e.HasValueType) {
				return reject("private element target escape or noncanonical literal")
			}
		}
	}
	for _, e := range c.passiveElems {
		modeOK := e.Mode == ElemModeDeclarative || e.Mode == ElemModePassive || hasTable && e.Mode == ElemModeActive
		tableFreePassive := !hasTable && e.Mode == ElemModePassive
		if !modeOK || tableFreePassive && (e.HasValueType || len(e.Values) == 0) || e.TableIndex != 0 || !elementType(e, false) || e.Mode != ElemModePassive && len(e.Values) != 0 || len(e.Values) > values || e.Offset.Base != 0 || e.Offset.HasGlobal || e.Offset.Global != 0 || len(e.Offset.Expr) != 0 {
			return reject("canonical passive or dropped active/declarative state required")
		}
		if (hasTable || tableFreePassive || e.HasValueType) && (!consume(16) || e.HasValueType && !consume(4) || !consume(4*len(e.Values))) {
			return reject("aggregate element metadata/payload byte bound")
		}
		values -= len(e.Values)
		for _, v := range e.Values {
			if !literal(v, e.HasValueType) {
				return reject("private passive target escape or noncanonical literal")
			}
		}
	}
	return nil
}
