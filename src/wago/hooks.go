package wago

// HookRegistry collects lifecycle callbacks contributed by extensions. Phase 1
// wires the runtime-lifecycle and instantiate hooks (the points the Runtime
// itself drives); compile, invoke, and process hooks are added in later phases.
type HookRegistry struct {
	onRuntimeClose    []func(*RuntimeContext)
	beforeInstantiate []func(*InstantiateContext) error
	afterInstantiate  []func(*InstantiateContext, *Instance) error
}

// OnRuntimeClose registers a callback run when the runtime is closed, in reverse
// registration order (last registered runs first), so extensions tear down after
// the ones that depend on them.
func (h *HookRegistry) OnRuntimeClose(fns ...func(*RuntimeContext)) {
	h.onRuntimeClose = append(h.onRuntimeClose, fns...)
}

// BeforeInstantiate registers a callback run before each Instantiate. Returning
// an error aborts instantiation.
func (h *HookRegistry) BeforeInstantiate(fns ...func(*InstantiateContext) error) {
	h.beforeInstantiate = append(h.beforeInstantiate, fns...)
}

// AfterInstantiate registers a callback run after each successful Instantiate.
func (h *HookRegistry) AfterInstantiate(fns ...func(*InstantiateContext, *Instance) error) {
	h.afterInstantiate = append(h.afterInstantiate, fns...)
}

// RuntimeContext is passed to runtime-lifecycle hooks.
type RuntimeContext struct {
	Runtime *Runtime
}

// InstantiateContext is passed to instantiate hooks. Imports is the fully merged
// import namespace (extension-provided plus any per-call additions). Metadata is
// scratch space extensions may use to carry state from Before to After.
type InstantiateContext struct {
	Runtime  *Runtime
	Compiled *Compiled
	Imports  Imports
	Metadata map[string]any
}
