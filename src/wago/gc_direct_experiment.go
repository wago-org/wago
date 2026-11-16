package wago

import (
	"errors"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Direct offsets are not a drop-in handle ABI. Admission remains intentionally
// narrow until every host/cross-instance storage edge has a relocation contract.
func (b *instanceBuilder) admitDirectGCExperiment() error {
	c := b.c
	if c.gcTrustedDirectVersion != 0 {
		if c.gcTrustedDirectVersion > 5 || c.gcDirectReadABIReceipt() != gc.DirectABIVersion || c.hasGCHelperOnlyReceipt() || c.loadValidateMemo().executionView() != c {
			return errors.New("wago: trusted direct reads require a fresh private compiler snapshot and matching direct ABI")
		}
	}
	if v := c.gcDirectReadABIReceipt(); v != 0 && v != gc.DirectABIVersion {
		return errors.New("wago: unsupported direct-read ABI receipt")
	}
	if (!c.hasGCHelperOnlyReceipt() && c.gcDirectReadABIReceipt() != gc.DirectABIVersion) || b.opts.store == nil || b.opts.store.private || !c.needsRuntimeGCCollectorDomain() || !c.sharedGCPersistentDomainSafe() || c.genericGCFrameRoots() == nil {
		return errors.New("wago: direct-offset GC requires a fresh helper-only artifact and an owned exact-root Runtime domain")
	}
	if len(c.Imports) != 0 || len(c.GlobalImports) != 0 || c.tableCount() != 0 || len(c.Elems) != 0 || len(c.passiveElems) != 0 || c.memoryImport != "" || c.usesManagedFunctionStorage() || c.hasOpaqueExternFrames() || c.requiredFeatures.IsEnabled(CoreFeatureExceptionHandling) || c.usesAtomicWaitHelpers() || len(c.customInstructions) != 0 || (b.opts.runtime != nil && len(b.opts.runtime.plugins) != 0) {
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
	for _, g := range c.Globals {
		if g.Type == ValExternRef || g.Type == ValFuncRef || g.Type == ValExnRef {
			return errors.New("wago: direct-offset external reference globals are unsupported")
		}
	}
	for _, sig := range c.Funcs {
		for _, v := range append(append([]ValType(nil), sig.Params...), sig.Results...) {
			if v == ValExternRef || v == ValFuncRef || v == ValExnRef {
				return errors.New("wago: direct-offset external reference signatures are unsupported")
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
