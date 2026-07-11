package wago

import "time"

// HookRegistry collects lifecycle callbacks contributed by extensions: runtime
// close, compile, instantiate, instance close, and invoke. Hooks fire on the
// Runtime-aware paths (rt.Compile, rt.Instantiate, Instance.Call, and
// Instance.Close) — the low-level package-level Compile/Instantiate/Invoke are
// hook-free.
type HookRegistry struct {
	onRuntimeClose       []func(*RuntimeContext)
	internalRuntimeClose int
	internalClose        []func() error
	internalBeforeClose  []func(*Instance)
	beforeCompile        []func(*CompileContext, []byte) ([]byte, error)
	afterCompile         []func(*CompileContext, *Module) error
	beforeInstantiate    []func(*InstantiateContext) error
	afterInstantiate     []func(*InstantiateContext, *Instance) error
	onInstantiateError   []func(*InstantiateContext, error)
	beforeClose          []func(*InstanceContext)
	afterClose           []func(*InstanceContext)
	beforeInvoke         []func(*InvokeContext) error
	afterInvoke          []func(*InvokeContext, []Value, error)
}

// BeforeCompile registers ordered source transforms. Returning nil bytes keeps
// the current source; returning bytes passes that source to the next transform.
// An error aborts compilation before code generation.
func (h *HookRegistry) BeforeCompile(fns ...func(*CompileContext, []byte) ([]byte, error)) {
	h.beforeCompile = append(h.beforeCompile, fns...)
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

// OnInstantiateError registers a callback run when instantiation fails after its
// preflight checks have completed. This includes failures from BeforeInstantiate,
// the low-level instantiator, and AfterInstantiate. Error hooks are observers and
// cannot replace or suppress the returned error.
func (h *HookRegistry) OnInstantiateError(fns ...func(*InstantiateContext, error)) {
	h.onInstantiateError = append(h.onInstantiateError, fns...)
}

// BeforeClose registers a callback run immediately before a Runtime-created
// instance releases its resources. Close hooks run in reverse registration order
// and do not fire for low-level package-level Instantiate instances.
func (h *HookRegistry) BeforeClose(fns ...func(*InstanceContext)) {
	h.beforeClose = append(h.beforeClose, fns...)
}

// AfterClose registers a callback run after a Runtime-created instance releases
// its resources. It shares Metadata with BeforeClose and runs in reverse
// registration order. Instance.Close is idempotent, so these hooks fire once.
func (h *HookRegistry) AfterClose(fns ...func(*InstanceContext)) {
	h.afterClose = append(h.afterClose, fns...)
}

// AfterCompile registers a callback run after each rt.Compile produces a Module.
// Returning an error fails the compile.
func (h *HookRegistry) AfterCompile(fns ...func(*CompileContext, *Module) error) {
	h.afterCompile = append(h.afterCompile, fns...)
}

// BeforeInvoke registers a callback run before each Instance.Call. Returning an
// error aborts the call (the export is not run).
func (h *HookRegistry) BeforeInvoke(fns ...func(*InvokeContext) error) {
	h.beforeInvoke = append(h.beforeInvoke, fns...)
}

// AfterInvoke registers a callback run after each Instance.Call returns, with the
// results and error. It runs even when the call errored (results is nil then),
// so it is the place for timing, metrics, and trap reporting.
func (h *HookRegistry) AfterInvoke(fns ...func(*InvokeContext, []Value, error)) {
	h.afterInvoke = append(h.afterInvoke, fns...)
}

// RuntimeContext is passed to runtime-lifecycle hooks.
type RuntimeContext struct {
	Runtime *Runtime
}

// InstantiateOrigin identifies which Runtime-owned path created an instance.
// Plugins can use it to distinguish direct application instances from neutral
// plugin-managed instances without inferring from imports.
type InstantiateOrigin uint8

const (
	InstantiateDirect InstantiateOrigin = iota
	InstantiateManaged
)

// InstantiateContext is passed to instantiate hooks. Imports is the fully merged
// import namespace (extension-provided plus any per-call additions). Metadata is
// scratch space extensions may use to carry state from Before to After.
type InstantiateContext struct {
	Runtime  *Runtime
	Module   *Module
	Compiled *Compiled
	Imports  Imports
	Origin   InstantiateOrigin
	Metadata map[string]any
}

// InstanceContext is passed to instance-close hooks. Metadata is shared from
// BeforeClose to AfterClose for one Close call.
type InstanceContext struct {
	Runtime  *Runtime
	Compiled *Compiled
	Instance *Instance
	Origin   InstantiateOrigin
	Metadata map[string]any
}

// CompileContext is passed to compile hooks.
type CompileContext struct {
	Runtime  *Runtime
	Metadata map[string]any
}

// InvokeContext is passed to invoke hooks. Start is set when the Before hooks run,
// so an After hook can measure the call duration. Metadata carries extension-local
// state from Before to After (e.g. a metrics start marker).
type InvokeContext struct {
	Runtime  *Runtime
	Instance *Instance
	Export   string
	Args     []Value
	Start    time.Time
	Metadata map[string]any
}

// appendFrom commits hooks collected in an extension's scratch Registry. Keeping
// registration transactional prevents a failed Use from leaking active hooks.
func (h *HookRegistry) appendFrom(src *HookRegistry) {
	if src == nil {
		return
	}
	h.onRuntimeClose = append(h.onRuntimeClose, src.onRuntimeClose...)
	h.internalRuntimeClose += src.internalRuntimeClose
	h.internalClose = append(h.internalClose, src.internalClose...)
	h.internalBeforeClose = append(h.internalBeforeClose, src.internalBeforeClose...)
	h.beforeCompile = append(h.beforeCompile, src.beforeCompile...)
	h.afterCompile = append(h.afterCompile, src.afterCompile...)
	h.beforeInstantiate = append(h.beforeInstantiate, src.beforeInstantiate...)
	h.afterInstantiate = append(h.afterInstantiate, src.afterInstantiate...)
	h.onInstantiateError = append(h.onInstantiateError, src.onInstantiateError...)
	h.beforeClose = append(h.beforeClose, src.beforeClose...)
	h.afterClose = append(h.afterClose, src.afterClose...)
	h.beforeInvoke = append(h.beforeInvoke, src.beforeInvoke...)
	h.afterInvoke = append(h.afterInvoke, src.afterInvoke...)
}

func (h *HookRegistry) requiredPluginCapabilities() []PluginCapability {
	var caps []PluginCapability
	if len(h.onRuntimeClose) > h.internalRuntimeClose {
		caps = append(caps, PluginRuntimeHooks)
	}
	if len(h.beforeCompile)+len(h.afterCompile) != 0 {
		caps = append(caps, PluginCompileHooks)
	}
	if len(h.beforeInstantiate)+len(h.afterInstantiate)+len(h.onInstantiateError)+len(h.beforeClose)+len(h.afterClose) != 0 {
		caps = append(caps, PluginInstanceHooks)
	}
	if len(h.beforeInvoke)+len(h.afterInvoke) != 0 {
		caps = append(caps, PluginInvokeHooks)
	}
	return caps
}
