//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Public instantiation owns the authoritative initializer metadata. Admission
// checks shapes without retaining private copies or placeholder values.
func admitSWARPublicInitializersWithMemories(m *wasm.Module, memories []wasm.MemType, rooted ...bool) error {
	if len(memories) > MaxProductionMemories {
		return unsupported("SWAR initializer memory count")
	}

	if m.ImportedGlobalCount()+len(m.Globals) > MaxPublicGlobals || len(m.Data) > 256 {
		return unsupported("SWAR initializer count bound")
	}
	localMutableReferences, err := admitLocalMutableFunctionGlobals(m)
	if err != nil {
		return err
	}
	localCall := admitMutableFunctionGlobalLocalCall(m)
	primitiveReferences := admitPublicPrimitiveReferenceModule(m, true)
	parser := newRootedScalarAggregateParser(m)
	for i, global := range m.Globals {
		if len(rooted) != 0 && rooted[0] && (wasm.ExternReferenceType(global.Type.Type) || wasm.CollectorReferenceType(m, global.Type.Type)) && rootedScalarAggregateGlobal(global, parser, m.ImportedGlobalCount()+i) {
			continue
		}
		if (primitiveReferences || wasm.ExternReferenceType(global.Type.Type)) && global.Type.Type.Kind() == wasm.ValRef {
			if !admitPrimitiveReferenceGlobalInitializer(global, m, m.ImportedGlobalCount()+i) {
				return unsupported("SWAR primitive reference global initializer")
			}
			continue
		}
		if localCall && i == 0 {
			// The exact profile pins the indexed nullable type and typed-null
			// initializer; runtime initialization owns the mutable cell itself.
			continue
		}
		if wasm.EqualValType(global.Type.Type, wasm.FuncRef) {
			if err := admitLocalFunctionGlobal(global, m, m.ImportedGlobalCount()+i, localMutableReferences); err != nil {
				return err
			}
			continue
		}
		if swarClassOf(global.Type.Type) == swarInvalid {
			return unsupported("SWAR global initializer carrier")
		}
		if scalarType(global.Type.Type, false) {
			if err := admitModuleIntegerInitializer(global.Init, global.Type.Type, m, m.ImportedGlobalCount()+i); err != nil {
				return err
			}
		} else if len(global.Init.BodyBytes) > 0 && global.Init.BodyBytes[0] == 0x23 {
			if err := admitGlobalExpression(global.Init, m, global.Type.Type, m.ImportedGlobalCount()+i); err != nil {
				return err
			}
		} else if _, err := swarLiteralGlobal(global); err != nil {
			return err
		}
	}
	remaining := MaxModuleBytes
	for _, data := range m.Data {
		if len(data.Init) > remaining {
			return unsupported("SWAR aggregate data byte bound")
		}
		remaining -= len(data.Init)
		switch data.Mode.Kind {
		case wasm.DataPassive:
			if data.Mode.Mem != 0 || len(data.Mode.Offset.Instrs) != 0 || len(data.Mode.Offset.BodyBytes) != 0 {
				return unsupported("SWAR passive data shape")
			}
		case wasm.DataActive:
			if uint64(data.Mode.Mem) >= uint64(len(memories)) {
				return unsupported("SWAR active data memory32")
			}
			if err := admitMemoryType(memories[data.Mode.Mem], true, true); err != nil {
				return err
			}
			if err := admitMemoryOffset(data.Mode.Offset, m, wasm.MemoryAddrType(memories[data.Mode.Mem]), true); err != nil {
				return err
			}
		default:
			return unsupported("SWAR data mode")
		}
	}
	return nil
}
