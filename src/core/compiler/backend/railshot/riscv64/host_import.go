//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

const (
	MaxHostFunctionImports = 256
	MaxProductionImports   = MaxHostFunctionImports + 256 + 1
	trapImportMissing      = 11
	trapImportContext      = 12
)

// Only dynamic per-instance targets are emitted. Foreign bindings, immediate
// code/home pointers, EH transfer, and imported reexports require further
// qualified lowering. Byte-only and isolated compilers retain their old gates.
func admitHostBindings(m *wasm.Module, opts *CompileOptions) error {
	count := 0
	for _, im := range m.Imports {
		if im.Type.Kind == wasm.ExternFunc {
			count++
		}
	}
	if count > MaxHostFunctionImports || count+len(m.Code) > MaxFunctions {
		return unsupported("aggregate function import bound")
	}
	if len(opts.ImportBindings) != count || (count == 0 && opts.SyncHostCalls) {
		return unsupported("dynamic host binding count")
	}
	for i, binding := range opts.ImportBindings {
		if !binding.Dynamic || binding.ImportIndex != uint32(i) || binding.CrossInstance || binding.CalleeEntry != 0 || binding.CalleeLinMem != 0 || binding.EHTransfer {
			return unsupported("dynamic host binding identity")
		}
	}
	if count != 0 && (len(m.Tables) != 0 || len(m.Elements) != 0) {
		return unsupported("function imports with private references are pending")
	}
	opts.hostImports = count != 0
	return nil
}
