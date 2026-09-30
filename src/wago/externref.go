package wago

import "fmt"

// NewExternRef registers value in this runtime's reference store and returns an
// opaque, non-null WebAssembly externref token. Runtime-created instances share
// the store, so the token may cross their public and host-call boundaries.
func (rt *Runtime) NewExternRef(value any) (ExternRef, error) {
	if rt == nil || rt.refStore == nil {
		return ExternRef{}, fmt.Errorf("wago: nil runtime")
	}
	operation, err := rt.beginOperation("NewExternRef", false)
	if err != nil {
		return ExternRef{}, err
	}
	defer operation.end()
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
	operation, err := rt.beginOperation("ExternRefValue", false)
	if err != nil {
		return nil, false
	}
	defer operation.end()
	return rt.refStore.resolveExternref(ref.token)
}

// ReleaseExternRef releases the host value retained by ref. It returns false
// for a stale, forged, or foreign token. The caller must first remove the token
// from every Wasm table, global, and other location that may still use it.
func (rt *Runtime) ReleaseExternRef(ref ExternRef) bool {
	if ref.IsNull() {
		return true
	}
	if rt == nil || rt.refStore == nil {
		return false
	}
	operation, err := rt.beginOperation("ReleaseExternRef", false)
	if err != nil {
		return false
	}
	defer operation.end()
	return rt.refStore.releaseExternref(ref.token)
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
	if in == nil || in.refStore == nil || in.rt != nil && in.rt.isClosed() {
		return nil, false
	}
	return in.refStore.resolveExternref(ref.token)
}

// ReleaseExternRef releases an externref from this instance's compatible store.
// The token must no longer be reachable by Wasm. Stale and foreign tokens fail.
func (in *Instance) ReleaseExternRef(ref ExternRef) bool {
	if ref.IsNull() {
		return true
	}
	if in == nil || in.refStore == nil || in.rt != nil && in.rt.isClosed() {
		return false
	}
	return in.refStore.releaseExternref(ref.token)
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
