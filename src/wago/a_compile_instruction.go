package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreplugins "github.com/wago-org/wago/src/core/plugins"
)

func resolveInstructionLowerings(m *wasm.Module, registered map[instructionKey]*registeredInstruction) map[uint32]coreplugins.Instruction {
	if len(registered) == 0 {
		return nil
	}
	resolved := make(map[uint32]coreplugins.Instruction)
	var functionIndex uint32
	for i := range m.Imports {
		imp := &m.Imports[i]
		if imp.Type.Kind != wasm.ExternFunc {
			continue
		}
		if ins := registered[instructionKey{imp.Module, imp.Name}]; ins != nil {
			if native, ok := ins.definition.Native(); ok {
				resolved[functionIndex] = native
			}
		}
		functionIndex++
	}
	if len(resolved) == 0 {
		return nil
	}
	return resolved
}
