package wago

import (
	"errors"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Direct offsets are not a drop-in handle ABI. Admission remains intentionally
// narrow until every host/cross-instance storage edge has a relocation contract.
func (b *instanceBuilder) admitDirectGCExperiment() error {
	c := b.c
	if !c.hasGCHelperOnlyReceipt() || b.opts.store == nil || b.opts.store.private || !c.needsRuntimeGCCollectorDomain() || !c.sharedGCPersistentDomainSafe() || c.genericGCFrameRoots() == nil {
		return errors.New("wago: direct-offset GC requires a fresh helper-only artifact and an owned exact-root Runtime domain")
	}
	if len(c.Imports) != 0 || len(c.GlobalImports) != 0 || c.tableCount() != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryImport != "" || c.usesManagedFunctionStorage() || c.hasOpaqueExternFrames() || c.requiredFeatures.IsEnabled(CoreFeatureExceptionHandling) || c.usesAtomicWaitHelpers() || len(c.customInstructions) != 0 || b.opts.pluginGC != nil {
		return errors.New("wago: direct-offset GC does not admit imports, tables, exceptions, custom collectors, or managed external references")
	}
	// Numeric arguments avoid introducing a partially audited incoming reference
	// boundary. Reference results and owned GC globals remain supported and rooted.
	for _, idx := range c.Exports {
		if idx < 0 || idx >= len(c.Funcs) {
			return errors.New("wago: invalid direct-offset export")
		}
		for _, v := range c.Funcs[idx].Params {
			switch v {
			case ValI32, ValI64, ValF32, ValF64, ValV128:
			default:
				return errors.New("wago: direct-offset exported arguments must be numeric")
			}
		}
	}
	for _, d := range c.GCTypeDescs {
		unsupported := func(k gc.StorageKind) bool {
			return k == gc.StorageExternRef || k == gc.StorageExternRefNull || k == gc.StorageFuncRef || k == gc.StorageFuncRefNull
		}
		if unsupported(d.Elem) {
			return errors.New("wago: direct-offset external reference arrays are unsupported")
		}
		for _, f := range d.Fields {
			if unsupported(f.Kind) {
				return errors.New("wago: direct-offset external reference fields are unsupported")
			}
		}
	}
	return nil
}
