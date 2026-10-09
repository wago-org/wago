//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

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
	var kinds, used uint8
	plainReference, _ := valueTypeDescriptorFromValType(ValFuncRef)
	for _, v := range c.ValueTypes {
		if v.Kind > ValueTypeReference || v.Kind == ValueTypeReference && v != plainReference || v.Kind != ValueTypeReference && v.Ref != (ReferenceTypeDescriptor{}) || v.Kind == ValueTypeV128 && !c.requiredFeatures.IsEnabled(CoreFeatureSIMD) || kinds&(1<<v.Kind) != 0 {
			return reject("unique numeric or canonical nullable funcref exact value types required")
		}
		kinds |= 1 << v.Kind
	}
	if c.tableCount() != 0 {
		// The preceding exact table proof establishes this pool binding even
		// when the module has no globals consuming the canonical funcref type.
		used |= 1 << c.TableValueTypeIndex
	}
	for index, g := range c.Globals {
		if !g.HasValueType || uint64(g.ValueTypeIndex) >= uint64(len(c.ValueTypes)) {
			return reject("exact numeric global binding")
		}
		v, ok := c.ValueTypes[g.ValueTypeIndex].ABIType(c.Types)
		if !ok || v != g.Type {
			return reject("numeric global ABI mismatch")
		}
		used |= 1 << g.ValueTypeIndex
		if g.Type == ValFuncRef {
			if index < len(c.GlobalImports) || c.ValueTypes[g.ValueTypeIndex] != plainReference || g.HasInitGlobal || g.InitGlobal != 0 || len(g.InitExpr) != 0 || g.Bits != 0 || g.V128 != (V128{}) || !g.HasInitFunc && g.InitFunc != 0 || g.HasInitFunc && (uint64(g.InitFunc) >= uint64(c.NumImports+len(c.Funcs)) || railshotNonCallableFunction(c, int(g.InitFunc))) {
				return reject("private reference cell literal/target confinement")
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
				used |= 1 << e.ValueTypeIndex
			}
		}
	}
	if used != uint8((1<<len(c.ValueTypes))-1) {
		return reject("unused numeric exact value types")
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
