package wago

import "time"

// HookRegistry collects lifecycle callbacks contributed by extensions: runtime
// close, compile, instantiate, and invoke. Hooks fire on the Runtime-aware paths
// (rt.Compile, rt.Instantiate, and Instance.Call) — the low-level package-level
// Compile/Instantiate/Invoke are hook-free.
type HookRegistry struct {
	onRuntimeClose    []func(*RuntimeContext)
	afterCompile      []func(*CompileContext, *Module) error
	beforeInstantiate []func(*InstantiateContext) error
	afterInstantiate  []func(*InstantiateContext, *Instance) error
	beforeInvoke      []func(*InvokeContext) error
	afterInvoke       []func(*InvokeContext, []Value, error)
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

// InstantiateContext is passed to instantiate hooks. Imports is the fully merged
// import namespace (extension-provided plus any per-call additions). Metadata is
// scratch space extensions may use to carry state from Before to After.
type InstantiateContext struct {
	Runtime  *Runtime
	Module   *Module
	Compiled *Compiled
	Imports  Imports
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
