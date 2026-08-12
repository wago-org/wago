package wago

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/semver"
)

// ImportOverridePolicy controls whether later registrations may replace earlier
// import bindings.
type ImportOverridePolicy int

const (
	// NoPluginOverrides is the default: plugin import namespaces must be
	// unique, and per-call imports may not replace a reserved wago_* module.
	NoPluginOverrides ImportOverridePolicy = iota
	// AllowTestOverrides relaxes both rules, for tests that need to stub a
	// reserved module or a previously-registered import.
	AllowTestOverrides
)

// Runtime is the high-level entry point: selected plugins contribute guest capabilities and
// host imports into it, and it threads those through Compile/Instantiate. The
// package-level Compile/Instantiate remain available as the low-level API.
type Runtime struct {
	mu                   sync.Mutex
	stateCond            *sync.Cond
	state                runtimeState
	pluginsLoadAttempted bool
	operational          bool
	activeOperations     uint64
	closeState           *runtimeCloseState
	instances            map[*Instance]uint64
	instanceSequence     uint64
	moduleCloseMu        sync.RWMutex
	cfg                  *RuntimeConfig
	overridePolicy       ImportOverridePolicy
	managedActive        atomic.Bool
	callerResolverActive atomic.Bool
	hooks                *hookRegistry
	refStore             *referenceStore
	guestArguments       []string

	plugins      []PluginDefinition
	imports      Imports                      // "module.name" -> host fn (any)
	importMeta   map[string]*registeredImport // "module.name" -> declared signature/cap/docs
	importOwner  map[string]string            // "module.name" -> owning plugin ID
	moduleOwner  map[string]string            // import module -> owning plugin ID
	caps         map[Capability]string
	capOrder     []Capability
	instructions map[string]*registeredInstruction
	pluginRuns   []registeredPluginRun
}

type runtimeState uint8

const (
	runtimeReady runtimeState = iota
	runtimeLoading
	runtimeClosing
	runtimeClosed
)

type runtimeCloseState struct {
	done   chan struct{}
	result error
}

type registeredPluginRun struct {
	name           string
	lifecycle      PluginLifecycle
	shouldStop     bool
	consumed       []*contractSlot
	provided       []*contractSlot
	closeInstances []func() error
	drainInstances []func() error
	callbacks      *pluginCallGate
	handles        []func() error
}

// RuntimeOption configures a Runtime at construction.
type RuntimeOption func(*Runtime)

// WithRuntimeConfig sets the compile/instantiate configuration (feature gating,
// bounds-check mode). Defaults to NewRuntimeConfig.
func WithRuntimeConfig(cfg *RuntimeConfig) RuntimeOption {
	return func(rt *Runtime) { rt.cfg = cfg }
}

// WithImportOverridePolicy sets how import collisions are resolved.
func WithImportOverridePolicy(p ImportOverridePolicy) RuntimeOption {
	return func(rt *Runtime) { rt.overridePolicy = p }
}

// WithGuestArguments sets the immutable guest argv visible through the exact
// host.arguments.read authority on this Runtime only.
func WithGuestArguments(args []string) RuntimeOption {
	copyArgs := append([]string(nil), args...)
	return func(rt *Runtime) { rt.guestArguments = copyArgs }
}

// NewRuntime creates a runtime with no plugins loaded.
func NewRuntime(opts ...RuntimeOption) *Runtime {
	rt := &Runtime{
		cfg:          NewRuntimeConfig(),
		hooks:        &hookRegistry{},
		refStore:     newReferenceStore(false),
		imports:      Imports{},
		importMeta:   map[string]*registeredImport{},
		importOwner:  map[string]string{},
		moduleOwner:  map[string]string{},
		caps:         map[Capability]string{},
		instructions: map[string]*registeredInstruction{},
		instances:    map[*Instance]uint64{},
	}
	rt.stateCond = sync.NewCond(&rt.mu)
	for _, opt := range opts {
		opt(rt)
	}
	if rt.cfg == nil {
		rt.cfg = NewRuntimeConfig()
	}
	return rt
}

// beginOperation permanently seals plugin loading on first public runtime use
// and leases the immutable plugin callback set through the operation. Loading
// excludes public callers; committed authority handles may opt into the Start
// phase after the complete plan has activated.
func (rt *Runtime) beginOperation(label string, allowLoading bool) (func(), error) {
	if rt == nil {
		return nil, fmt.Errorf("wago: %s on a nil runtime", label)
	}
	rt.mu.Lock()
	switch rt.state {
	case runtimeLoading:
		if !allowLoading {
			rt.mu.Unlock()
			return nil, fmt.Errorf("wago: %s while plugins are loading", label)
		}
	case runtimeClosing, runtimeClosed:
		rt.mu.Unlock()
		return nil, fmt.Errorf("wago: %s on a closed runtime", label)
	}
	rt.operational = true
	rt.activeOperations++
	rt.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			rt.mu.Lock()
			if rt.activeOperations > 0 {
				rt.activeOperations--
			}
			rt.stateCond.Broadcast()
			rt.mu.Unlock()
		})
	}, nil
}

func (rt *Runtime) registerInstance(in *Instance) error {
	if rt == nil || in == nil {
		return fmt.Errorf("wago: cannot register a nil runtime instance")
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.state == runtimeClosing || rt.state == runtimeClosed {
		return fmt.Errorf("wago: runtime closed during instantiation")
	}
	rt.instanceSequence++
	rt.instances[in] = rt.instanceSequence
	return nil
}

func (rt *Runtime) directInstancesSnapshot() []*Instance {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	type orderedInstance struct {
		value    *Instance
		sequence uint64
	}
	ordered := make([]orderedInstance, 0, len(rt.instances))
	for in, sequence := range rt.instances {
		// Plugin-managed instances are owned by their InstanceManager. Closing
		// them here would tear down a provider's workers before dependent plugin
		// Stop callbacks have finished using that provider's contract.
		if in.instantiateOrigin() != InstantiateDirect {
			continue
		}
		ordered = append(ordered, orderedInstance{value: in, sequence: sequence})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].sequence < ordered[j].sequence })
	out := make([]*Instance, len(ordered))
	for i := range ordered {
		out[i] = ordered[i].value
	}
	return out
}

func (rt *Runtime) unregisterInstance(in *Instance) {
	if rt == nil || in == nil {
		return
	}
	rt.mu.Lock()
	delete(rt.instances, in)
	rt.mu.Unlock()
}

func (rt *Runtime) allowsLifecycleCallbacks() bool {
	if rt == nil {
		return false
	}
	rt.mu.Lock()
	active := rt.state != runtimeClosing && rt.state != runtimeClosed
	rt.mu.Unlock()
	return active
}

func (rt *Runtime) isClosed() bool {
	if rt == nil {
		return true
	}
	rt.mu.Lock()
	closed := rt.state == runtimeClosing || rt.state == runtimeClosed
	rt.mu.Unlock()
	return closed
}

// Compile compiles a wasm module under the runtime's configuration and wraps it
// as a *Module, resolving its imports against registered plugins and notifying
// successful-compile observers.
func (rt *Runtime) Compile(wasmBytes []byte) (*Module, error) {
	return rt.compile(wasmBytes, false)
}

func (rt *Runtime) compilePlugin(wasmBytes []byte) (*Module, error) {
	return rt.compile(wasmBytes, true)
}

func (rt *Runtime) compile(wasmBytes []byte, allowLoading bool) (*Module, error) {
	end, err := rt.beginOperation("Compile", allowLoading)
	if err != nil {
		return nil, err
	}
	defer end()
	var compilation CompilationIdentity
	if len(rt.hooks.beforeCompile) != 0 || len(rt.hooks.afterCompile) != 0 || len(rt.hooks.onCompileError) != 0 {
		compilation = CompilationIdentity{value: &compilationIdentityToken{}}
	}
	ctx := ModuleSourceContext{Compilation: compilation}
	emitError := func(original error) error {
		var hookErrs []error
		for _, fn := range rt.hooks.onCompileError {
			if panicErr := callHookSafely("ModuleCompileObserver.OnError", func() {
				fn(ModuleCompileErrorEvent{Compilation: compilation, Err: original})
			}); panicErr != nil {
				hookErrs = append(hookErrs, panicErr)
			}
		}
		return joinPrimary(original, hookErrs...)
	}
	source := wasmBytes
	if len(rt.hooks.beforeCompile) != 0 {
		source = append([]byte(nil), wasmBytes...)
	}
	for _, fn := range rt.hooks.beforeCompile {
		transform := fn
		var next []byte
		var transformErr error
		panicErr := callHookSafely("ModuleSourceTransformer", func() { next, transformErr = transform(ctx, source) })
		if err := joinPrimary(transformErr, panicErr); err != nil {
			return nil, emitError(err)
		}
		if next != nil {
			source = next
		}
	}
	rt.mu.Lock()
	instructions := make(map[string]*registeredInstruction, len(rt.instructions))
	for key, ins := range rt.instructions {
		instructions[key] = ins
	}
	cfg := rt.cfg
	rt.mu.Unlock()
	c, err := compileWithConfigAndInstructions(cfg, source, instructions)
	if err != nil {
		return nil, emitError(err)
	}
	mod := rt.buildModule(c)
	// The historical Imports key joins module and field with a dot. Both Wasm
	// names may themselves contain dots, so recover the exact pair from the
	// validated source for structured metadata and component-model linking.
	// The flat key remains unchanged for backwards-compatible Imports lookup.
	if decoded, decodeErr := wasm.DecodeModule(source); decodeErr == nil {
		funcIndex := 0
		for i := range decoded.Imports {
			im := &decoded.Imports[i]
			if im.Type.Kind != wasm.ExternFunc || funcIndex >= len(mod.imports) {
				continue
			}
			mod.imports[funcIndex].Module = im.Module
			mod.imports[funcIndex].Name = im.Name
			funcIndex++
		}
	}
	if len(rt.hooks.afterCompile) > 0 {
		sourceDigest := DigestModuleSource(source)
		for _, fn := range rt.hooks.afterCompile {
			if err := callHookSafely("ModuleCompileObserver", func() {
				fn(ModuleCompiledEvent{Compilation: compilation, Module: moduleView(mod), SourceDigest: sourceDigest})
			}); err != nil {
				return nil, emitError(joinPrimary(err, mod.Close(), c.Close()))
			}
		}
	}
	return mod, nil
}

// Module binds an already compiled artifact to this runtime's plugin imports
// and lifecycle. It is the precompiled counterpart of Runtime.Compile.
func (rt *Runtime) Module(c *Compiled) (*Module, error) {
	if rt == nil || c == nil {
		return nil, fmt.Errorf("wago: nil runtime or compiled module")
	}
	end, err := rt.beginOperation("Module", false)
	if err != nil {
		return nil, err
	}
	defer end()
	var compilation CompilationIdentity
	if len(rt.hooks.afterCompile) != 0 || len(rt.hooks.onCompileError) != 0 {
		compilation = CompilationIdentity{value: &compilationIdentityToken{}}
	}
	emitError := func(original error) error {
		var hookErrs []error
		for _, fn := range rt.hooks.onCompileError {
			if panicErr := callHookSafely("ModuleCompileObserver.OnError", func() {
				fn(ModuleCompileErrorEvent{Compilation: compilation, Err: original})
			}); panicErr != nil {
				hookErrs = append(hookErrs, panicErr)
			}
		}
		return joinPrimary(original, hookErrs...)
	}
	mod := rt.buildModule(c)
	if len(rt.hooks.afterCompile) > 0 {
		for _, fn := range rt.hooks.afterCompile {
			if err := callHookSafely("ModuleCompileObserver", func() { fn(ModuleCompiledEvent{Compilation: compilation, Module: moduleView(mod)}) }); err != nil {
				return nil, emitError(joinPrimary(err, mod.Close()))
			}
		}
	}
	return mod, nil
}

// InstantiateOption configures a single Instantiate call.
type InstantiateOption func(*instantiateConfig)

type instantiateConfig struct {
	imports       Imports
	gc            GCConfig
	hasGC         bool
	policy        Policy
	forceSyncHost bool
}

// WithPolicy applies a capability/resource policy to the instance. A module that
// requires a capability the policy does not allow (or that exceeds a resource
// limit) is rejected with an error wrapping ErrPermissionDenied.
func WithPolicy(p Policy) InstantiateOption {
	return func(c *instantiateConfig) { c.policy = p }
}

// WithImports adds per-call imports on top of the plugin-provided namespace.
// A per-call import may not shadow a reserved wago_* module unless the runtime's
// override policy is AllowTestOverrides.
func WithImports(im Imports) InstantiateOption {
	return func(c *instantiateConfig) {
		if c.imports == nil {
			c.imports = Imports{}
		}
		for k, v := range im {
			c.imports[k] = v
		}
	}
}

// WithGC sets the GC configuration for this instance.
func WithGC(gc GCConfig) InstantiateOption {
	return func(c *instantiateConfig) { c.gc, c.hasGC = gc, true }
}

// WithSynchronousHostCalls forces the parked native host-call protocol even
// when a module has no direct function imports. Component-model adapters need
// this when host functions can arrive indirectly through an imported table.
func WithSynchronousHostCalls() InstantiateOption {
	return func(c *instantiateConfig) { c.forceSyncHost = true }
}

// Instantiate instantiates a module, wiring the runtime's plugin imports plus
// any per-call imports. ctx is honored for cancellation before the (synchronous)
// instantiate work begins. Runtime ownership is attached before start executes;
// a failed start or AfterInstantiate hook closes the partial instance through the
// normal lifecycle before returning its joined failure. Fallible post-create
// interceptors run after initialization but before the start function. A function import that no
// plugin or per-call import provides is reported with a hint rather than a
// downstream binding failure.
func (rt *Runtime) Instantiate(ctx context.Context, mod *Module, opts ...InstantiateOption) (*Instance, error) {
	return rt.instantiateOrigin(ctx, mod, InstantiateDirect, false, opts...)
}

func (rt *Runtime) instantiateOrigin(ctx context.Context, mod *Module, origin InstantiateOrigin, allowLoading bool, opts ...InstantiateOption) (*Instance, error) {
	end, err := rt.beginOperation("Instantiate", allowLoading)
	if err != nil {
		return nil, err
	}
	defer end()
	if mod == nil {
		return nil, fmt.Errorf("wago: Instantiate: nil module")
	}
	if mod.isClosed() {
		return nil, fmt.Errorf("wago: Instantiate: module is closed")
	}
	if mod.rt != rt {
		return nil, fmt.Errorf("wago: Instantiate: module belongs to a different runtime")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var cfg instantiateConfig
	if len(opts) != 0 {
		cfg = applyInstantiateOptions(opts)
	}
	if err := applyPolicy(mod, cfg.policy); err != nil {
		return nil, err
	}

	rt.mu.Lock()
	// Merge plugin imports first, then per-call imports on top.
	var merged Imports
	if n := len(rt.imports) + len(cfg.imports); n != 0 {
		merged = make(Imports, n)
		for k, v := range rt.imports {
			merged[k] = v
		}
	}
	policy := rt.overridePolicy
	rt.mu.Unlock()

	for k, v := range cfg.imports {
		if module := importModule(k); isReserved(module) && policy != AllowTestOverrides {
			if _, provided := merged[k]; provided {
				return nil, fmt.Errorf("wago: import %q may not override reserved module %q", k, module)
			}
		}
		merged[k] = v
	}

	// Surface an unsatisfied function import as a clear, actionable error before
	// the low-level binder fails on it.
	for _, spec := range mod.imports {
		if spec.Kind != ImportFunc {
			continue
		}
		if _, ok := merged[spec.Key()]; !ok {
			return nil, missingImportError(spec)
		}
	}

	in, err := rt.instantiateWithHooksOrigin(mod, merged, cfg.gc, cfg.hasGC, cfg.forceSyncHost, origin)
	if err == nil && rt.isClosed() {
		err = joinPrimary(fmt.Errorf("wago: runtime closed during instantiation"), in.Close())
		in = nil
	}
	return in, err
}

func applyInstantiateOptions(opts []InstantiateOption) instantiateConfig {
	var cfg instantiateConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// instantiateWithHooksOrigin runs the Runtime-aware instantiation path and emits
// plugin lifecycle callbacks around the low-level instantiator.
func (rt *Runtime) instantiateWithHooksOrigin(mod *Module, imports Imports, gc GCConfig, hasGC, forceSyncHost bool, origin InstantiateOrigin) (*Instance, error) {
	iopts := InstantiateOptions{
		Imports: imports, store: rt.refStore, runtime: rt, origin: origin,
		forceSyncHost:  forceSyncHost || rt.callerResolverActive.Load(),
		moduleIdentity: mod.moduleIdentity(),
	}
	if hasGC {
		iopts.GC = gc
		iopts.pluginGC = &gc
	}

	// Keep the no-lifecycle-hook path allocation-free. The instance still retains
	// rt so invoke/close hooks registered before later calls can be observed.
	if len(rt.hooks.beforeInstantiate) == 0 && len(rt.hooks.afterCreate) == 0 && len(rt.hooks.afterInstantiate) == 0 && len(rt.hooks.onInstantiateError) == 0 {
		inst, err := instantiateCore(mod.c, iopts)
		if err != nil {
			return nil, err
		}
		return inst, nil
	}

	request := InstantiationRequest{Module: moduleView(mod), Origin: origin}
	emitError := func(original error) error {
		var hookErrs []error
		for _, fn := range rt.hooks.onInstantiateError {
			if panicErr := callHookSafely("OnInstantiateError", func() { fn(InstantiationErrorEvent{Module: request.Module, Origin: origin, Err: original}) }); panicErr != nil {
				hookErrs = append(hookErrs, panicErr)
			}
		}
		return joinPrimary(original, hookErrs...)
	}

	for _, fn := range rt.hooks.beforeInstantiate {
		var hookErr error
		panicErr := callHookSafely("BeforeInstantiate", func() { hookErr = fn(request) })
		if err := joinPrimary(hookErr, panicErr); err != nil {
			return nil, emitError(err)
		}
	}
	iopts.afterCreate = func(inst *Instance) error {
		event := InstantiationEvent{Module: request.Module, Instance: InstanceIdentity{value: inst}, Origin: origin}
		for _, fn := range rt.hooks.afterCreate {
			var hookErr error
			panicErr := callHookSafely("InstanceInstantiateInterceptor.After", func() { hookErr = fn(event) })
			if err := joinPrimary(hookErr, panicErr); err != nil {
				return err
			}
		}
		return nil
	}
	inst, err := instantiateCore(mod.c, iopts)
	if err != nil {
		return nil, emitError(err)
	}
	for _, fn := range rt.hooks.afterInstantiate {
		event := InstantiationEvent{Module: request.Module, Instance: InstanceIdentity{value: inst}, Origin: origin}
		if err := callHookSafely("InstanceInstantiateObserver", func() { fn(event) }); err != nil {
			failed := joinPrimary(err, inst.Close())
			return nil, emitError(failed)
		}
	}
	return inst, nil
}

// Plugins returns immutable definitions in dependency-resolved activation order.
func (rt *Runtime) Plugins() []PluginDefinition {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make([]PluginDefinition, len(rt.plugins))
	for i := range rt.plugins {
		out[i], _ = freezeDefinition(rt.plugins[i])
	}
	return out
}

// Capabilities returns the capabilities declared by registered plugins,
// sorted.
func (rt *Runtime) Capabilities() []Capability {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	caps := append([]Capability(nil), rt.capOrder...)
	sort.Slice(caps, func(i, j int) bool { return caps[i] < caps[j] })
	return caps
}

// ProvidedImports returns immutable metadata for host functions contributed by
// loaded plugins, sorted by Wasm module and name. It intentionally omits each
// callable HostFunc binding; this is an inspection surface, not an authority
// escape into the low-level instantiator.
func (rt *Runtime) ProvidedImports() []ImportSpec {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	specs := make([]ImportSpec, 0, len(rt.importMeta))
	for _, meta := range rt.importMeta {
		specs = append(specs, ImportSpec{
			Module:        meta.module,
			Name:          meta.name,
			Kind:          ImportFunc,
			Params:        append([]ValType(nil), meta.params...),
			Results:       append([]ValType(nil), meta.results...),
			Capability:    meta.cap,
			HasCapability: meta.hasCap,
			Docs:          meta.docs,
			Provided:      true,
		})
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Key() < specs[j].Key() })
	return specs
}

// Close stops plugins and marks the runtime unusable. It closes every instance
// created through this Runtime; callers retain only idempotent closed handles.
func (rt *Runtime) Close() error { return rt.CloseContext(context.Background()) }

// CloseContext is idempotent and joinable: concurrent callers wait for and
// receive the same complete shutdown result. The first caller's context is used
// for plugin Stop callbacks; later callers do not cancel an active shutdown.
// It must not be called synchronously from a callback or hook running on this
// Runtime because shutdown waits for the current operation to return.
func (rt *Runtime) CloseContext(ctx context.Context) error {
	if rt == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rt.mu.Lock()
	for rt.state == runtimeLoading {
		rt.stateCond.Wait()
	}
	if rt.closeState != nil {
		state := rt.closeState
		rt.mu.Unlock()
		<-state.done
		return state.result
	}
	state := &runtimeCloseState{done: make(chan struct{})}
	rt.closeState = state
	rt.state = runtimeClosing
	hooks := rt.hooks.onRuntimeClose
	internalClose := append([]func() error(nil), rt.hooks.internalClose...)
	pluginRuns := append([]registeredPluginRun(nil), rt.pluginRuns...)
	store := rt.refStore
	rt.mu.Unlock()

	var errs []error
	for i := len(hooks) - 1; i >= 0; i-- {
		observer := hooks[i]
		if err := callHookSafely("RuntimeCloseObserver", func() { observer(RuntimeCloseEvent{}) }); err != nil {
			errs = append(errs, err)
		}
	}
	// A Module.Close already inside an observer finishes before teardown. New
	// module closes see runtimeClosing and suppress callbacks into stopped code.
	rt.moduleCloseMu.Lock()
	rt.moduleCloseMu.Unlock()

	instances := rt.directInstancesSnapshot()
	for i := len(instances) - 1; i >= 0; i-- {
		if err := instances[i].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	for i := len(pluginRuns) - 1; i >= 0; i-- {
		pluginRuns[i].deactivateProviders()
		errs = append(errs, closePluginRun(ctx, pluginRuns[i])...)
	}
	rt.mu.Lock()
	for rt.activeOperations != 0 {
		rt.stateCond.Wait()
	}
	rt.mu.Unlock()
	for i := len(instances) - 1; i >= 0; i-- {
		state := instances[i].ensurePluginState().close.Load()
		if state != nil {
			<-state.quiesced
		}
	}
	for i := len(internalClose) - 1; i >= 0; i-- {
		if err := internalClose[i](); err != nil {
			errs = append(errs, err)
		}
	}
	store.closeRuntime()
	result := errors.Join(errs...)
	rt.mu.Lock()
	rt.state = runtimeClosed
	state.result = result
	close(state.done)
	rt.stateCond.Broadcast()
	rt.mu.Unlock()
	return result
}

func (rt *Runtime) rollbackCommittedPluginPlan(ctx context.Context) error {
	err := rt.CloseContext(ctx)
	rt.mu.Lock()
	rt.plugins = nil
	rt.imports = Imports{}
	rt.importMeta = map[string]*registeredImport{}
	rt.importOwner = map[string]string{}
	rt.moduleOwner = map[string]string{}
	rt.caps = map[Capability]string{}
	rt.capOrder = nil
	rt.instructions = map[string]*registeredInstruction{}
	rt.hooks = &hookRegistry{}
	rt.pluginRuns = nil
	rt.managedActive.Store(false)
	rt.callerResolverActive.Store(false)
	rt.instances = map[*Instance]uint64{}
	rt.instanceSequence = 0
	rt.mu.Unlock()
	return err
}

func (run registeredPluginRun) deactivateProviders() {
	for _, slot := range run.provided {
		slot.deactivate()
	}
}

func closePluginRun(ctx context.Context, run registeredPluginRun) (errs []error) {
	for i := len(run.provided) - 1; i >= 0; i-- {
		if err := run.provided[i].revoke(); err != nil {
			errs = append(errs, err)
		}
	}
	for i := len(run.closeInstances) - 1; i >= 0; i-- {
		if err := run.closeInstances[i](); err != nil {
			errs = append(errs, err)
		}
	}
	run.callbacks.deactivate()
	if run.shouldStop && run.lifecycle.Stop != nil {
		var stopErr error
		panicErr := callSafely("plugin Stop", func() { stopErr = run.lifecycle.Stop(ctx) })
		if err := joinPrimary(stopErr, panicErr); err != nil {
			errs = append(errs, &PluginError{Plugin: run.name, Phase: PluginPhaseStop, Err: err})
		}
	}
	if err := run.callbacks.closeAndWait(); err != nil {
		errs = append(errs, err)
	}
	for i := len(run.drainInstances) - 1; i >= 0; i-- {
		if err := run.drainInstances[i](); err != nil {
			errs = append(errs, err)
		}
	}
	for i := len(run.consumed) - 1; i >= 0; i-- {
		if err := run.consumed[i].revoke(); err != nil {
			errs = append(errs, err)
		}
	}
	for i := len(run.handles) - 1; i >= 0; i-- {
		if err := run.handles[i](); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// importModule returns the module part of a "module.name" import key (up to the
// first dot), matching how Compile builds the key.
func importModule(key string) string {
	for i := 0; i < len(key); i++ {
		if key[i] == '.' {
			return key[:i]
		}
	}
	return key
}

func (rt *Runtime) scopedHostCalls() bool {
	return rt != nil && (rt.managedActive.Load() || rt.callerResolverActive.Load())
}

func isReserved(module string) bool {
	switch module {
	case "wago_process", "wago_mailbox", "wago_timer", "wago_metrics",
		"wago_log", "wago_fs", "wago_net", "wago_http", "wago_kv",
		"wago_crypto", "wago_debug", "wago_runtime":
		return true
	default:
		return false
	}
}

// missingImportError explains an unsatisfied function import and hints at the
// fix, wrapping ErrMissingImport for errors.Is.
func missingImportError(spec ImportSpec) error {
	hint := fmt.Sprintf("provide it via WithImports or a plugin that registers module %q", spec.Module)
	if isReserved(spec.Module) {
		hint = fmt.Sprintf("select the plugin that provides %q in PluginSet", spec.Module)
	}
	return fmt.Errorf("module imports %q, but nothing provides it; %s: %w", spec.Key(), hint, ErrMissingImport)
}

// checkCompat validates the running wago Version against a plugin's declared
// "wago" engine constraint, a full semver 2.0.0 range (see src/core/semver). Other
// engines (tinygo, go, …) and platforms are advisory — surfaced by inspection but
// not enforced here, since the running binary already embodies them.
func checkCompat(c Compatibility) error {
	constraint, ok := c.Engines["wago"]
	if !ok {
		return nil
	}
	con, err := semver.ParseConstraint(constraint)
	if err != nil {
		return fmt.Errorf("invalid wago version constraint %q: %w", constraint, err)
	}
	ver, err := semver.Parse(Version)
	if err != nil {
		return nil // our own Version should always parse; don't block on a bug here
	}
	if !con.Check(ver) {
		return fmt.Errorf("requires wago %s, have %s", constraint, Version)
	}
	return nil
}
