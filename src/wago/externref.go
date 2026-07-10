package wago

import "fmt"

// NewExternRef registers value in this runtime's reference store and returns an
// opaque, non-null WebAssembly externref token. Runtime-created instances share
// the store, so the token may cross their public and host-call boundaries.
func (rt *Runtime) NewExternRef(value any) (ExternRef, error) {
	if rt == nil || rt.refStore == nil {
		return ExternRef{}, fmt.Errorf("wago: nil runtime")
	}
	token, err := rt.refStore.issueExternref(value)
	if err != nil {
		return ExternRef{}, err
	}
	return ExternRef{token: token}, nil
}

// ExternRefValue resolves a token issued by this runtime. It returns false for
// forged, stale, or incompatible-store tokens. Null resolves to (nil, true).
func (rt *Runtime) ExternRefValue(ref ExternRef) (any, bool) {
	if ref.IsNull() {
		return nil, true
	}
	if rt == nil || rt.refStore == nil {
		return nil, false
	}
	return rt.refStore.resolveExternref(ref.token)
}

// NewExternRef registers value in this instance's reference store. Runtime
// instances use their shared runtime store; standalone instances create a lazy
// private store whose tokens are incompatible with other standalone instances.
func (in *Instance) NewExternRef(value any) (ExternRef, error) {
	if in == nil {
		return ExternRef{}, fmt.Errorf("wago: nil instance")
	}
	store, err := in.referenceStoreForBoundary()
	if err != nil {
		return ExternRef{}, err
	}
	token, err := store.issueExternref(value)
	if err != nil {
		return ExternRef{}, err
	}
	return ExternRef{token: token}, nil
}

// ExternRefValue resolves a token issued by this instance's compatible store.
// It returns false for forged, stale, cross-runtime, or cross-private-store
// tokens. Null resolves to (nil, true).
func (in *Instance) ExternRefValue(ref ExternRef) (any, bool) {
	if ref.IsNull() {
		return nil, true
	}
	if in == nil || in.refStore == nil {
		return nil, false
	}
	return in.refStore.resolveExternref(ref.token)
}

func (in *Instance) validExternrefToken(token uint64) bool {
	if token == 0 {
		return true
	}
	if in == nil || in.refStore == nil {
		return false
	}
	_, ok := in.refStore.resolveExternref(token)
	return ok
}
