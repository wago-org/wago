//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

const (
	MaxHostFunctionImports = 256
	MaxProductionImports   = MaxHostFunctionImports + 256 + MaxProductionMemories + 1 // functions, globals, memories, table
	trapImportMissing      = 11
	trapImportContext      = 12
)

// Only dynamic per-instance targets are emitted. Foreign bindings, immediate
// code/home pointers and EH transfer need separate lowering. The public retained
// table profile can combine numeric imports with defined canonical references.
// Byte-only and isolated compilers retain their old gates.
func admitHostBindings(m *wasm.Module, opts *CompileOptions) error {
	count := 0
	for _, im := range m.Imports {
		if im.Type.Kind == wasm.ExternFunc {
			count++
		}
	}
	if count > MaxHostFunctionImports {
		return unsupported("aggregate function import bound")
	}
	if len(opts.ImportBindings) != count || (count == 0 && opts.SyncHostCalls && atomicHelperSlots(m) == 0 && !opts.OwnedExceptionReferences && !NeedsFunctionSubtypeHelpers(m)) {
		return unsupported("dynamic host binding count")
	}
	for i, binding := range opts.ImportBindings {
		if !binding.Dynamic || binding.ImportIndex != uint32(i) || binding.CrossInstance || binding.CalleeEntry != 0 || binding.CalleeLinMem != 0 || binding.EHTransfer && !opts.ExceptionHandlingSource {
			return unsupported("dynamic host binding identity")
		}
	}
	if count != 0 && (len(m.Tables)+m.ImportedTableCount() != 0 || len(m.Elements) != 0) && (!opts.Interruptible || !opts.ForeignTableCalls) {
		return unsupported("function imports with tables require the retained public table ABI")
	}
	if slots := atomicHelperSlots(m); slots != 0 && (!opts.SyncHostCalls || opts.SyncHostSlots < slots || !opts.Interruptible || !opts.InvocationContext || !opts.ForeignTableCalls) {
		return unsupported("atomic wait helper requires cooperative synchronous host slots")
	}
	opts.hostImports = count != 0
	return nil
}
