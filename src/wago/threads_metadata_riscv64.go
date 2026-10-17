//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// Match the initial public threads execution boundary independently of a
// source compiler's cache. Returning wait helpers need the fixed four slots.
func rv64ThreadedMemoryProfile(c *Compiled) bool {
	if c == nil || !c.requiredFeatures.IsEnabled(CoreFeatureThreads) || c.boundsMode != BoundsChecksExplicit || c.memoryCount() != 1 || c.NumImports != 0 || c.tableCount() != 0 || c.tagImportCount() != 0 || c.activeDataCount() != 0 || len(c.PassiveData) != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	memory := c.memoryDef(0)
	if memory.Addr64 || memory.Min > 65536 || memory.HasMax && (memory.Max < memory.Min || memory.Max > 65536) || memory.Shared && (!memory.HasMax || c.memoryImport == "") {
		return false
	}
	for _, global := range c.GlobalImports {
		if global.Mutable {
			return false
		}
	}
	rootedReferences := c.requiredFeatures.IsEnabled(CoreFeatureGC) && c.usesGenericGCExecution() && c.nativeGCABIRequirement() == gc.NativeABIVersion && c.genericGCFrameRoots() != nil
	for _, function := range c.Funcs {
		for _, values := range [][]ValType{function.Params, function.Results} {
			for _, value := range values {
				if value != ValI32 && value != ValI64 && value != ValF32 && value != ValF64 && !(value == ValV128 && c.requiredFeatures.IsEnabled(CoreFeatureSIMD)) && !(rootedReferences && (value == ValAnyRef || value == ValI31Ref)) {
					return false
				}
			}
		}
	}
	return !c.usesAtomicWaitHelpers() || c.syncHostSlots >= 4 && c.needsFuncRefContextHeader
}

// Guard mode elides only memory-zero memory32 accesses. Indexed accesses retain
// explicit checks, and importing a primary memory requires a guarded backing.
func rv64GuardedMemoryProfile(c *Compiled) bool {
	if c == nil || !guardPageBuilt || c.threadedMemory0() {
		return false
	}
	return c.memoryCount() == 0 || !c.memoryDef(0).Addr64
}
