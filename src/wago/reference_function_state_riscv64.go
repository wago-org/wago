//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

// Retained numeric state uses the ordinary exact metadata representation. The
// closed witness cannot authorize references in storage or imported resources.
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
	if memories > railshot.MaxProductionMemories || len(c.Globals) > 256 || c.activeDataCount() > 256 || len(c.PassiveData) > 256 || len(c.ValueTypes) > 5 {
		return reject("directory bound")
	}
	if memories == 0 {
		if c.MemMinPages != 0 || c.MemMaxPages != 0 || c.MemHasMax {
			return reject("inactive memory metadata")
		}
	} else if c.memoryDir == nil || len(c.memoryDir.defs) != memories || !c.memoryDir.exactExports {
		return reject("exact owned memory definitions required")
	}
	for i := 0; i < memories; i++ {
		def := c.memoryDef(i)
		if def.ImportKey != "" || def.Shared || !def.HasMax && def.Max != 0 {
			return reject("owned canonical unshared memory required")
		}
		if !consume(21) {
			return reject("memory bytes")
		}
	}
	var kinds, used uint8
	for _, v := range c.ValueTypes {
		if v.Kind > ValueTypeV128 || v.Ref != (ReferenceTypeDescriptor{}) || v.Kind == ValueTypeV128 && !c.requiredFeatures.IsEnabled(CoreFeatureSIMD) || kinds&(1<<v.Kind) != 0 {
			return reject("unique numeric exact value types required")
		}
		kinds |= 1 << v.Kind
	}
	for _, g := range c.Globals {
		if !g.HasValueType || uint64(g.ValueTypeIndex) >= uint64(len(c.ValueTypes)) {
			return reject("exact numeric global binding")
		}
		v, ok := c.ValueTypes[g.ValueTypeIndex].ABIType(c.Types)
		if !ok || v != g.Type {
			return reject("numeric global ABI mismatch")
		}
		used |= 1 << g.ValueTypeIndex
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
