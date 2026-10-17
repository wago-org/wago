//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

func rv64PrivateIndexedFunctionType(c *Compiled, exact ValueTypeDescriptor) (uint32, bool) {
	if c == nil || exact.Kind != ValueTypeReference || exact.Ref.Nullable || exact.Ref.Exact || !exact.Ref.Heap.Defined || exact.Ref.Heap.Abstract != 0 || uint64(exact.Ref.Heap.TypeIndex) >= uint64(len(c.Types)) {
		return 0, false
	}
	typ := c.Types[exact.Ref.Heap.TypeIndex]
	if typ.RecGroup != exact.Ref.Heap.TypeIndex || typ.Kind != CompositeTypeFunction || !typ.Final || len(typ.Supers) != 0 || typ.HasDescribes || typ.Describes != 0 || typ.HasDescriptor || typ.Descriptor != 0 || len(typ.Fields) != 0 || typ.Array != (FieldTypeDescriptor{}) {
		return 0, false
	}
	return exact.Ref.Heap.TypeIndex, true
}

// Retained numeric state uses the ordinary exact metadata representation. The
// closed witness confines reference cells to own numeric descriptors;
// imported storage and signatures remain numeric.
// Shared validators subsequently check limits, initializer types and indices.
func rv64ValidateReferenceFunctionState(c *Compiled, remaining *int) error {
	reject := func(detail string) error { return fmt.Errorf("RV64 closed reference state: %s", detail) }
	consume := func(n int) bool {
		if n > *remaining {
			return false
		}
		*remaining -= n
		return true
	}
	if rv64GCTypeSubtypingRuntimeTypedTableCall(c) {
		// The exact product digest binds its indexed value-type pool and proves
		// that it has no memories, globals, imports, or data state.
		if !consume(64) {
			return reject("runtime typed-table state metadata budget")
		}
		return nil
	}
	memories := c.memoryCount()
	if memories > railshot.MaxProductionMemories || len(c.Globals) > 256 || len(c.GlobalImports) > len(c.Globals) || c.activeDataCount() > 256 || len(c.PassiveData) > 256 || len(c.ValueTypes) > 6 {
		return reject("directory bound")
	}
	if memories == 0 {
		if c.MemMinPages != 0 || c.MemMaxPages != 0 || c.MemHasMax || c.memoryImport != "" {
			return reject("inactive memory metadata")
		}
	} else if c.memoryDir == nil || len(c.memoryDir.defs) != memories || !c.memoryDir.exactExports {
		return reject("exact numeric memory definitions required")
	}
	if memories > 0 && (c.memoryImport != c.memoryDir.defs[0].ImportKey || c.MemHasMax != c.memoryDir.defs[0].HasMax) {
		return reject("primary memory import metadata")
	}
	seenOwned := false
	for i := 0; i < memories; i++ {
		def := c.memoryDef(i)
		if def.Shared || !def.HasMax && def.Max != 0 || def.ImportKey != "" && seenOwned {
			return reject("canonical unshared memory namespace required")
		}
		seenOwned = seenOwned || def.ImportKey == ""
		if !consume(21) || !consume(len(def.ImportKey)) {
			return reject("memory bytes")
		}
	}
	var kinds uint8
	var used [6]bool
	plainReference, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for i, v := range c.ValueTypes {
		if v.Kind > ValueTypeReference || v.Kind != ValueTypeReference && v.Ref != (ReferenceTypeDescriptor{}) || v.Kind == ValueTypeV128 && !c.requiredFeatures.IsEnabled(CoreFeatureSIMD) {
			return reject("bounded numeric or private indexed function exact value types required")
		}
		for j := range i {
			if c.ValueTypes[j] == v {
				return reject("duplicate exact value type")
			}
		}
		if v.Kind == ValueTypeReference {
			if v != plainReference {
				if _, ok := rv64PrivateIndexedFunctionType(c, v); !ok {
					return reject("private indexed function exact value type required")
				}
			}
			continue
		}
		if kinds&(1<<v.Kind) != 0 {
			return reject("unique numeric exact value types required")
		}
		kinds |= 1 << v.Kind
	}
	if c.tableCount() != 0 {
		// The preceding exact table proof establishes this pool binding even
		// when the module has no globals consuming the canonical funcref type.
		if uint64(c.TableValueTypeIndex) >= uint64(len(c.ValueTypes)) {
			return reject("table exact value type index")
		}
		used[c.TableValueTypeIndex] = true
	}
	for index, g := range c.Globals {
		if !g.HasValueType || uint64(g.ValueTypeIndex) >= uint64(len(c.ValueTypes)) {
			return reject("exact numeric global binding")
		}
		v, ok := c.ValueTypes[g.ValueTypeIndex].ABIType(c.Types)
		if !ok || v != g.Type {
			return reject("numeric global ABI mismatch")
		}
		used[g.ValueTypeIndex] = true
		if g.Type == ValFuncRef {
			exact := c.ValueTypes[g.ValueTypeIndex]
			typedIndex, typed := rv64PrivateIndexedFunctionType(c, exact)
			if index < len(c.GlobalImports) || g.HasInitGlobal || g.InitGlobal != 0 || len(g.InitExpr) != 0 || g.Bits != 0 || g.V128 != (V128{}) || !g.HasInitFunc && g.InitFunc != 0 || g.HasInitFunc && (uint64(g.InitFunc) >= uint64(c.NumImports+len(c.Funcs)) || railshotNonCallableFunction(c, int(g.InitFunc))) {
				return reject("private reference cell literal/target confinement")
			}
			if typed {
				local := int(g.InitFunc) - c.NumImports
				if g.Mutable || c.NumImports != 0 || c.tableCount() != 0 || c.memoryCount() != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || c.HasStart || !g.HasInitFunc || local < 0 || local >= len(c.Funcs) || !c.Funcs[local].HasTypeIndex || c.Funcs[local].TypeIndex != typedIndex || !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) || !c.needsFuncRefDescs() {
					return reject("private indexed reference cell exact target confinement")
				}
			} else if exact != plainReference {
				return reject("private reference cell exact type")
			}
			if !consume(18) {
				return reject("reference cell initializer bytes")
			}
			continue
		}
		if index < len(c.GlobalImports) {
			imp := c.GlobalImports[index]
			if !imp.HasValueType || imp.ValueTypeIndex != g.ValueTypeIndex || imp.Type != g.Type || imp.Mutable != g.Mutable || g.HasInitFunc || g.InitFunc != 0 || g.HasInitGlobal || g.InitGlobal != 0 || len(g.InitExpr) != 0 || g.Bits != 0 || g.V128 != (V128{}) {
				return reject("exact zero-initialized numeric global import binding")
			}
			if !consume(2) || !consume(len(imp.Module)) || !consume(len(imp.Name)) {
				return reject("global import name bytes")
			}
		}
		if g.HasInitFunc || g.InitFunc != 0 || !g.HasInitGlobal && g.InitGlobal != 0 || len(g.InitExpr) > railshot.MaxIntegerInitializerBytes {
			return reject("global initializer metadata")
		}
		if g.HasInitGlobal || len(g.InitExpr) != 0 {
			if g.Bits != 0 || g.V128 != (V128{}) || g.HasInitGlobal && len(g.InitExpr) != 0 {
				return reject("conflicting global initializer")
			}
		} else if g.Type == ValV128 {
			if g.Bits != 0 {
				return reject("inactive vector initializer bits")
			}
		} else if g.V128 != (V128{}) || (g.Type == ValI32 || g.Type == ValF32) && g.Bits>>32 != 0 {
			return reject("inactive numeric initializer bits")
		}
		if !consume(18 + len(g.InitExpr)) {
			return reject("global initializer bytes")
		}
	}
	// The preceding element proof checks exact plain funcref bindings, including
	// empty and dropped expression slots. They consume a pool entry without a
	// table/global use; discarded declarations do not manufacture a binding.
	for _, elements := range [][]ElemInit{c.Elems, c.passiveElems} {
		for _, e := range elements {
			if e.HasValueType {
				if uint64(e.ValueTypeIndex) >= uint64(len(c.ValueTypes)) {
					return reject("element exact value type index")
				}
				used[e.ValueTypeIndex] = true
			}
		}
	}
	for i := range c.ValueTypes {
		if !used[i] {
			return reject("unused exact value types")
		}
	}
	for i := 0; i < c.activeDataCount(); i++ {
		data := c.activeDataAt(i)
		if uint64(data.MemoryIndex) >= uint64(memories) {
			return reject("active data memory index")
		}
		off := data.Offset
		if c.memoryDef(int(data.MemoryIndex)).Addr64 && len(off.Expr) == 0 {
			return reject("memory64 offset expression required")
		}
		if !off.HasGlobal && off.Global != 0 || len(off.Expr) > railshot.MaxIntegerInitializerBytes || len(off.Expr) != 0 && (off.HasGlobal || off.Base != 0) || off.HasGlobal && off.Base != 0 {
			return reject("data offset metadata")
		}
		if !consume(4+len(off.Expr)) || !consume(len(data.Bytes)) {
			return reject("data initializer bytes")
		}
	}
	for _, data := range c.PassiveData {
		if !consume(4) || !consume(len(data.Bytes)) {
			return reject("passive data bytes")
		}
	}
	return nil
}

// A Wasm module has one export namespace. Keep its count and name bytes shared
// across the retained maps without allocating another name directory.
func rv64ValidateReferenceFunctionExports(c *Compiled, remaining *int) error {
	memories := c.memoryExportMap()
	if len(c.Exports)+len(c.GlobalExports)+len(memories) > railshot.MaxFunctions {
		return fmt.Errorf("RV64 closed reference export count bound")
	}
	for name, index := range c.GlobalExports {
		if index < 0 || index >= len(c.Globals) || c.Globals[index].Type == ValFuncRef {
			return fmt.Errorf("RV64 closed reference global export index")
		}
		if _, duplicate := c.Exports[name]; duplicate {
			return fmt.Errorf("RV64 closed reference duplicate export name")
		}
	}
	for name, index := range memories {
		if index < 0 || index >= c.memoryCount() {
			return fmt.Errorf("RV64 closed reference memory export index")
		}
		_, function := c.Exports[name]
		_, global := c.GlobalExports[name]
		if function || global {
			return fmt.Errorf("RV64 closed reference duplicate export name")
		}
	}
	for _, exports := range []map[string]int{c.Exports, c.GlobalExports, memories} {
		for name := range exports {
			if len(name) > *remaining {
				return fmt.Errorf("RV64 closed reference export name byte bound")
			}
			*remaining -= len(name)
		}
	}
	return nil
}
