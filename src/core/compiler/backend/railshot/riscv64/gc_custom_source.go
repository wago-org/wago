//go:build linux && !tinygo

package riscv64

import (
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// A custom carrier declaration can be opaque only when every actual binding to
// that signature is a validated inline import. Shared ordinary bindings retain
// their original reference meaning.
func rootedCustomImportType(m *wasm.Module, directory *pluginInstructionDirectory, typeIndex uint32) bool {
	if directory == nil || !directory.hasCustom() {
		return false
	}
	for _, index := range m.FuncTypes {
		if !index.Rec && index.Index == typeIndex {
			return false
		}
	}
	found := false
	function := uint32(0)
	for _, imported := range m.Imports {
		if imported.Type.Kind != wasm.ExternFunc {
			continue
		}
		index := imported.Type.FuncType()
		if !index.Rec && index.Index == typeIndex {
			if _, registered := directory.instructions[function]; !registered {
				return false
			}
			found = true
		}
		function++
	}
	return found
}

// AdmitGCRootedScalarCustomModule applies the ordinary rooted source rules to
// the original type graph after checking each registered native inline import.
// It does not validate an arbitrary module in place of frontend validation.
func AdmitGCRootedScalarCustomModule(m *wasm.Module, instructions map[uint32]railcore.CustomInstruction) bool {
	return len(instructions) != 0 && admitCustomInstructions(m, instructions) == nil && admitRV64GCRootedScalarModule(m, pluginInstructions(instructions))
}
