//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Public instantiation owns the authoritative initializer metadata. Admission
// checks shapes without retaining private copies or placeholder values.
func admitSWARPublicInitializers(m *wasm.Module, memory *wasm.MemType) error {
	if m.ImportedGlobalCount()+len(m.Globals) > 256 || len(m.Data) > 256 {
		return unsupported("SWAR initializer count bound")
	}
	for i, global := range m.Globals {
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
			if memory == nil || memory.Shared || memory.Limits.Addr64 || data.Mode.Mem != 0 {
				return unsupported("SWAR active data memory32")
			}
			if err := admitI32Offset(data.Mode.Offset, m, true); err != nil {
				return err
			}
		default:
			return unsupported("SWAR data mode")
		}
	}
	return nil
}
