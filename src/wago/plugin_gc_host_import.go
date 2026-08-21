package wago

import "fmt"

// pluginHostImport marks one declarative Runtime plugin host function as trusted
// to use the calling instance's GC-aware synchronous host boundary. It is not a
// HostFuncRef: it owns no Wasm function identity, exact structural signature,
// collector, or GC domain. Those belong to the importing Compiled module and
// calling Instance.
type pluginHostImport struct {
	fn    HostFunc
	sig   FuncSig
	store *referenceStore
}

func newPluginHostImport(fn HostFunc, sig FuncSig, store *referenceStore) *pluginHostImport {
	return &pluginHostImport{
		fn: fn,
		sig: FuncSig{
			Params:  append([]ValType(nil), sig.Params...),
			Results: append([]ValType(nil), sig.Results...),
		},
		store: store,
	}
}

func (p *pluginHostImport) validate(store *referenceStore, sig FuncSig) error {
	if p == nil || p.fn == nil || p.store == nil {
		return fmt.Errorf("Runtime plugin host import is invalid")
	}
	if store == nil || p.store != store {
		return fmt.Errorf("Runtime plugin host import belongs to a different Runtime reference store")
	}
	if !funcSigEqual(p.sig, sig) {
		return fmt.Errorf("Runtime plugin host import signature mismatch")
	}
	if !funcSigHasGCRefs(sig) {
		return fmt.Errorf("Runtime plugin GC host import requires a collector-reference signature")
	}
	return nil
}
