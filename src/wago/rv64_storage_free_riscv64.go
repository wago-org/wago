//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import "github.com/wago-org/wago/src/core/compiler/wasm"

func installRV64StorageFreeProof(c *Compiled, m *wasm.Module) {
	if c != nil && !c.needsNativeGCABI() && !c.needsRuntimeGCCollectorDomain() && !c.usesManagedFunctionStorage() && rv64StorageFreeModule(m) {
		setRV64StorageFreeProof(c, true)
	}
}

// Initializers have completed before dependency transfer. Every later body is
// limited to numeric values, locals and control flow: no imported callback,
// memory/table/global access, reference operation or allocating helper can use
// an imported storage owner through a retained callable.
func rv64StorageFreeModule(m *wasm.Module) bool {
	if m == nil || m.ImportedFuncCount() != 0 || len(m.Code) == 0 || m.ImportedMemCount()+m.ImportedTableCount()+m.ImportedGlobalCount() == 0 {
		return false
	}
	// Collector state can remain visible to lifecycle/root scans even when
	// numeric bodies never read it. Keep its physical storage dependencies.
	for _, group := range m.Types {
		for _, sub := range group.SubTypes {
			if sub.Comp.Kind != wasm.CompFunc {
				return false
			}
		}
	}
	for _, global := range m.Globals {
		if global.Type.Type.Kind() == wasm.ValRef && !wasm.FunctionReferenceType(m, global.Type.Type) {
			return false
		}
	}
	for _, imp := range m.Imports {
		if imp.Type.Kind == wasm.ExternGlobal && imp.Type.GlobalType().Type.Kind() == wasm.ValRef && !wasm.FunctionReferenceType(m, imp.Type.GlobalType().Type) || imp.Type.Kind == wasm.ExternTable && !wasm.FunctionReferenceType(m, wasm.RefVal(imp.Type.TableType().Ref)) {
			return false
		}
	}
	for _, table := range m.Tables {
		if !wasm.FunctionReferenceType(m, wasm.RefVal(table.Type.Ref)) {
			return false
		}
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for index, fn := range m.Code {
		sig, ok := m.FuncSignature(uint32(index))
		if !ok {
			return false
		}
		for _, values := range [][]wasm.ValType{sig.Params, sig.Results} {
			for _, value := range values {
				if value != wasm.I32 && value != wasm.I64 && value != wasm.F32 && value != wasm.F64 {
					return false
				}
			}
		}
		for _, local := range fn.Locals.Runs {
			if local.Type != wasm.I32 && local.Type != wasm.I64 && local.Type != wasm.F32 && local.Type != wasm.F64 {
				return false
			}
		}
		r := wasm.NewReader(fn.BodyBytes)
		if !r.HasNext() {
			return false
		}
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return false
			}
			allowed := op <= 0x05 || op >= 0x0b && op <= 0x0f || op >= 0x1a && op <= 0x1c || op >= 0x20 && op <= 0x22 || op >= 0x41 && op <= 0xc4
			var imm wasm.InstructionImmediate
			if !allowed || classifier.ClassifyInto(r, op, &imm) != nil {
				return false
			}
		}
	}
	return true
}

func rv64StorageFreeProof(c *Compiled) bool {
	if c != nil {
		if indexes := c.loadCompileIndexes(); indexes != nil {
			return indexes.railshotTail.storageFreeBodies
		}
	}
	return false
}

func setRV64StorageFreeProof(c *Compiled, enabled bool) {
	c.ensureCompileIndexes().railshotTail.storageFreeBodies = enabled
}

func rv64StorageFreeCallable(c *Compiled) bool {
	return rv64StorageFreeProof(c) && rv64SourceContractValid(c)
}
