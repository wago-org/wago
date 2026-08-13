package wago

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

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
	mu                    sync.Mutex
	stateCond             *sync.Cond
	state                 runtimeState
	loadingDone           chan struct{}
	pluginsLoadAttempted  bool
	operational           bool
	activeOperations      uint64
	compileOperations     uint64
	moduleCloseOperations uint64
	closeState            *runtimeCloseState
	instances             map[*Instance]uint64
	instanceSequence      uint64
	cfg                   *RuntimeConfig
	overridePolicy        ImportOverridePolicy
	managedActive         atomic.Bool
	callerResolverActive  atomic.Bool
	hooks                 *hookRegistry
	publishedHooks        atomic.Pointer[hookRegistry]
	refStore              *referenceStore
	guestArguments        []string

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
	return func(rt *Runtime) { rt.cfg = cfg.clone() }
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
	rt.loadingDone = make(chan struct{})
	close(rt.loadingDone)
	for _, opt := range opts {
		opt(rt)
	}
	if rt.cfg == nil {
		rt.cfg = NewRuntimeConfig()
	}
	rt.publishedHooks.Store(rt.hooks)
	return rt
}

// Config returns a caller-owned snapshot of the runtime's effective compile and
// execution configuration.
func (rt *Runtime) Config() *RuntimeConfig {
	if rt == nil {
		return nil
	}
	rt.mu.Lock()
	cfg := rt.cfg.clone()
	rt.mu.Unlock()
	return cfg
}

func (rt *Runtime) loadHooks() *hookRegistry {
	if rt == nil {
		return &hookRegistry{}
	}
	if hooks := rt.publishedHooks.Load(); hooks != nil {
		return hooks
	}
	return &hookRegistry{}
}

// storeHooks publishes one complete immutable hook generation. The caller must
// hold rt.mu while changing the production generation.
func (rt *Runtime) storeHooks(hooks *hookRegistry) {
	if hooks == nil {
		hooks = &hookRegistry{}
	}
	rt.hooks = hooks
	rt.publishedHooks.Store(hooks)
}

// beginOperation permanently seals plugin loading on first public runtime use
// and leases the immutable plugin callback set through the operation. Loading
// excludes public callers; committed authority handles may opt into the Start
// phase after the complete plan has activated.
func (rt *Runtime) beginOperation(label string, allowLoading bool) (func(), error) {
	return rt.beginOperationKind(label, allowLoading, false)
}

func (rt *Runtime) beginOperationGeneration(label string, allowLoading bool) (*hookRegistry, *pluginOperationReservation, func(), error) {
	if rt == nil {
		return nil, nil, nil, fmt.Errorf("wago: %s on a nil runtime", label)
	}
	rt.mu.Lock()
	switch rt.state {
	case runtimeLoading:
		if !allowLoading {
			rt.mu.Unlock()
			return nil, nil, nil, fmt.Errorf("wago: %s while plugins are loading", label)
		}
	case runtimeClosing, runtimeClosed:
		rt.mu.Unlock()
		return nil, nil, nil, fmt.Errorf("wago: %s on a closed runtime", label)
	}
	hooks := rt.loadHooks()
	reservation, err := reservePluginOperation(hooks.operationGates)
	if err != nil {
		rt.mu.Unlock()
		return nil, nil, nil, err
	}
	rt.operational = true
	rt.activeOperations++
	rt.mu.Unlock()
	var once sync.Once
	end := func() {
		once.Do(func() {
			reservation.release()
			rt.mu.Lock()
			if rt.activeOperations == 0 {
				rt.mu.Unlock()
				panic("wago: runtime operation lease underflow")
			}
			rt.activeOperations--
			rt.stateCond.Broadcast()
			rt.mu.Unlock()
		})
	}
	return hooks, reservation, end, nil
}

func (rt *Runtime) beginCompileOperation(label string, allowLoading bool) (func(), error) {
	return rt.beginOperationKind(label, allowLoading, true)
}

func (rt *Runtime) beginOperationKind(label string, allowLoading, compile bool) (func(), error) {
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
	if compile {
		rt.compileOperations++
	}
	rt.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			rt.mu.Lock()
			if rt.activeOperations == 0 || compile && rt.compileOperations == 0 {
				rt.mu.Unlock()
				panic("wago: runtime operation lease underflow")
			}
			rt.activeOperations--
			if compile {
				rt.compileOperations--
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

func (rt *Runtime) beginModuleCloseCallbacks() (*hookRegistry, func()) {
	if rt == nil {
		return nil, nil
	}
	rt.mu.Lock()
	if rt.state == runtimeClosing || rt.state == runtimeClosed {
		rt.mu.Unlock()
		return nil, nil
	}
	hooks := rt.loadHooks()
	rt.activeOperations++
	rt.moduleCloseOperations++
	rt.mu.Unlock()
	return hooks, func() {
		rt.mu.Lock()
		if rt.activeOperations == 0 || rt.moduleCloseOperations == 0 {
			rt.mu.Unlock()
			panic("wago: module close operation lease underflow")
		}
		rt.activeOperations--
		rt.moduleCloseOperations--
		rt.stateCond.Broadcast()
		rt.mu.Unlock()
	}
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

// PreparedCompile is one admitted runtime compilation generation. Preparation
// validates configuration, snapshots hooks/imports/custom instructions, and runs
// source transforms exactly once. Compile, Adopt, or Close releases the shutdown
// admission exactly once.
type PreparedCompile struct {
	mu           sync.Mutex
	rt           *Runtime
	end          func()
	compilation  CompilationIdentity
	source       []byte
	cfg          *RuntimeConfig
	bindings     moduleBindings
	hooks        *hookRegistry
	instructions map[string]*registeredInstruction
	cacheable    bool
	consumed     bool
	finished     bool
}

// PrepareCompile admits and prepares one public compilation.
func (rt *Runtime) PrepareCompile(wasmBytes []byte) (*PreparedCompile, error) {
	return rt.prepareCompile(wasmBytes, false)
}

func (rt *Runtime) prepareCompile(wasmBytes []byte, allowLoading bool) (*PreparedCompile, error) {
	end, err := rt.beginCompileOperation("Compile", allowLoading)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*PreparedCompile, error) {
		end()
		return nil, err
	}

	rt.mu.Lock()
	cfg := rt.cfg.clone()
	hooks := rt.loadHooks()
	bindings := rt.snapshotModuleBindingsLocked(hooks)
	instructions := make(map[string]*registeredInstruction, len(rt.instructions))
	for key, ins := range rt.instructions {
		instructions[key] = ins
	}
	rt.mu.Unlock()
	if err := cfg.Validate(); err != nil {
		return fail(err)
	}

	var compilation CompilationIdentity
	if len(hooks.beforeCompile) != 0 || len(hooks.afterCompile) != 0 || len(hooks.onCompileError) != 0 {
		compilation = CompilationIdentity{value: &compilationIdentityToken{}}
	}
	ctx := ModuleSourceContext{Compilation: compilation}
	source := wasmBytes
	if len(hooks.beforeCompile) != 0 {
		source = append([]byte(nil), wasmBytes...)
	}
	for _, fn := range hooks.beforeCompile {
		transform := fn
		var next []byte
		var transformErr error
		panicErr := callHookSafely("ModuleSourceTransformer", func() { next, transformErr = transform(ctx, source) })
		if err := joinPrimary(transformErr, panicErr); err != nil {
			return fail(emitCompileError(hooks, compilation, err))
		}
		if next == nil {
			next = source
		}
		// A transformer may retain either its input or returned slice. Snapshot
		// after every callback so the admitted generation cannot be mutated later
		// through plugin-owned storage.
		source = append([]byte(nil), next...)
	}
	return &PreparedCompile{
		rt: rt, end: end, compilation: compilation, source: source, cfg: cfg,
		bindings: bindings, hooks: hooks, instructions: instructions,
		cacheable: len(hooks.beforeCompile) == 0 && len(instructions) == 0,
	}, nil
}

// Source returns the admitted transformed source for this preparation. Callers
// receive a read-only view and must not mutate it. Transformer return slices are
// snapshotted; without transforms, the view retains PrepareCompile's input under
// the same ownership contract as Compile. A successful Compile may retain that
// storage until the returned Module closes.
func (p *PreparedCompile) Source() []byte {
	if p == nil {
		return nil
	}
	return p.source
}

// Cacheable reports whether this compiler generation has a trustworthy
// deterministic serialized-artifact identity.
func (p *PreparedCompile) Cacheable() bool { return p != nil && p.cacheable }

func (p *PreparedCompile) consume() error {
	if p == nil {
		return fmt.Errorf("wago: nil prepared compile")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.consumed {
		return fmt.Errorf("wago: prepared compile already consumed")
	}
	p.consumed = true
	return nil
}

func (p *PreparedCompile) finish() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.finished {
		p.mu.Unlock()
		return
	}
	p.finished = true
	end := p.end
	p.end = nil
	p.mu.Unlock()
	if end != nil {
		end()
	}
}

// Compile compiles the prepared source and adopts ownership into the returned
// Module.
func (p *PreparedCompile) Compile() (*Module, error) {
	if err := p.consume(); err != nil {
		return nil, err
	}
	defer p.finish()
	c, err := compileWithConfigAndInstructions(p.cfg, p.source, p.instructions)
	if err != nil {
		return nil, emitCompileError(p.hooks, p.compilation, err)
	}
	return p.finishCompile(c)
}

// Adopt binds a freshly decoded artifact to this preparation and transfers its
// ownership. A failed adoption closes the artifact exactly once.
func (p *PreparedCompile) Adopt(c *Compiled) (*Module, error) {
	if err := p.consume(); err != nil {
		return nil, err
	}
	defer p.finish()
	if c == nil {
		return nil, fmt.Errorf("wago: nil compiled artifact")
	}
	return p.finishCompile(c)
}

func (p *PreparedCompile) finishCompile(c *Compiled) (*Module, error) {
	mod := buildModule(c, p.bindings)
	if len(p.hooks.afterCompile) != 0 {
		event := ModuleCompiledEvent{Compilation: p.compilation, Module: moduleView(mod), SourceDigest: DigestModuleSource(p.source)}
		for _, fn := range p.hooks.afterCompile {
			observer := fn
			if err := callHookSafely("ModuleCompileObserver", func() { observer(event) }); err != nil {
				return nil, emitCompileError(p.hooks, p.compilation, joinPrimary(err, c.Close()))
			}
		}
	}
	mod.ownsCompiled = true
	return mod, nil
}

// Close abandons an unused preparation. It is idempotent.
func (p *PreparedCompile) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.consumed {
		p.mu.Unlock()
		return nil
	}
	p.consumed = true
	p.mu.Unlock()
	p.finish()
	return nil
}

func emitCompileError(hooks *hookRegistry, compilation CompilationIdentity, original error) error {
	var hookErrs []error
	for _, fn := range hooks.onCompileError {
		observer := fn
		if panicErr := callHookSafely("ModuleCompileObserver.OnError", func() {
			observer(ModuleCompileErrorEvent{Compilation: compilation, Err: original})
		}); panicErr != nil {
			hookErrs = append(hookErrs, panicErr)
		}
	}
	return joinPrimary(original, hookErrs...)
}

func (rt *Runtime) compile(wasmBytes []byte, allowLoading bool) (*Module, error) {
	prepared, err := rt.prepareCompile(wasmBytes, allowLoading)
	if err != nil {
		return nil, err
	}
	return prepared.Compile()
}

// Module binds an already compiled artifact to this runtime's plugin imports
// and lifecycle. It is the precompiled counterpart of Runtime.Compile.
func (rt *Runtime) Module(c *Compiled) (*Module, error) {
	return rt.bindModule(c, false)
}

// AdoptModule binds a decoded artifact and transfers ownership to the returned
// Module. If binding fails, the artifact is closed before returning.
func (rt *Runtime) AdoptModule(c *Compiled) (*Module, error) {
	mod, err := rt.bindModule(c, true)
	if err != nil && c != nil {
		_ = c.Close()
	}
	return mod, err
}

func (rt *Runtime) bindModule(c *Compiled, ownsCompiled bool) (*Module, error) {
	if rt == nil || c == nil {
		return nil, fmt.Errorf("wago: nil runtime or compiled module")
	}
	end, err := rt.beginCompileOperation("Module", false)
	if err != nil {
		return nil, err
	}
	defer end()
	rt.mu.Lock()
	hooks := rt.loadHooks()
	bindings := rt.snapshotModuleBindingsLocked(hooks)
	rt.mu.Unlock()
	var compilation CompilationIdentity
	if len(hooks.afterCompile) != 0 || len(hooks.onCompileError) != 0 {
		compilation = CompilationIdentity{value: &compilationIdentityToken{}}
	}
	mod := buildModule(c, bindings)
	if len(hooks.afterCompile) != 0 {
		event := ModuleCompiledEvent{Compilation: compilation, Module: moduleView(mod)}
		for _, fn := range hooks.afterCompile {
			observer := fn
			if err := callHookSafely("ModuleCompileObserver", func() { observer(event) }); err != nil {
				return nil, emitCompileError(hooks, compilation, joinPrimary(err, mod.Close()))
			}
		}
	}
	mod.ownsCompiled = ownsCompiled
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
	if mod == nil {
		return nil, fmt.Errorf("wago: Instantiate: nil module")
	}
	// Runtime ownership is intrinsic to the wrapper and takes precedence over
	// destination policy or closed-state behavior.
	if mod.rt != rt {
		return nil, fmt.Errorf("wago: Instantiate: %w", ErrForeignModule)
	}
	hooks, reservation, end, err := rt.beginOperationGeneration("Instantiate", allowLoading)
	if err != nil {
		return nil, err
	}
	defer end()
	if !mod.beginUse() {
		return nil, fmt.Errorf("wago: Instantiate: module is closed")
	}
	usingModule := true
	defer func() {
		if usingModule {
			mod.endUse()
		}
	}()
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

	imports, err := rt.resolveInstanceImports(mod.imports, cfg.imports)
	if err != nil {
		return nil, err
	}

	// Lifecycle callbacks may close their Module. End the inspection lease before
	// callbacks; low-level instantiation acquires a fresh lease and transfers it to
	// retained code ownership before start-time host callbacks.
	mod.endUse()
	usingModule = false
	in, err := rt.instantiateWithHooksOrigin(mod, imports, cfg.gc, cfg.hasGC, cfg.forceSyncHost, origin, hooks, reservation)
	if err == nil && rt.isClosed() {
		err = joinPrimary(fmt.Errorf("wago: runtime closed during instantiation"), in.Close())
		in = nil
	}
	return in, err
}

// resolveInstanceImports captures one instance-owned binding set without
// cloning runtime imports the module cannot use. Explicit per-call imports are
// retained even when undeclared, preserving the low-level Imports inspection
// behavior. Runtime imports are only retained for declared module keys.
func (rt *Runtime) resolveInstanceImports(specs []ImportSpec, overrides Imports) (Imports, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	for key := range overrides {
		module := importModule(key)
		if !isReserved(module) || rt.overridePolicy == AllowTestOverrides {
			continue
		}
		if _, provided := rt.imports[key]; provided {
			return nil, fmt.Errorf("wago: import %q may not override reserved module %q", key, module)
		}
	}

	capacity := len(overrides) + len(specs)
	if available := len(overrides) + len(rt.imports); available < capacity {
		capacity = available
	}
	var resolved Imports
	if len(overrides) != 0 {
		resolved = make(Imports, capacity)
		for key, value := range overrides {
			resolved[key] = value
		}
	}
	for _, spec := range specs {
		key := spec.Key()
		if _, provided := overrides[key]; provided {
			continue
		}
		value, provided := rt.imports[key]
		if !provided {
			if spec.Kind == ImportFunc {
				return nil, missingImportError(spec)
			}
			continue
		}
		if resolved == nil {
			resolved = make(Imports, capacity)
		}
		resolved[key] = value
	}
	return resolved, nil
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
func (rt *Runtime) instantiateWithHooksOrigin(mod *Module, imports Imports, gc GCConfig, hasGC, forceSyncHost bool, origin InstantiateOrigin, hooks *hookRegistry, reservation *pluginOperationReservation) (*Instance, error) {
	iopts := InstantiateOptions{
		Imports: imports, store: rt.refStore, runtime: rt, origin: origin,
		forceSyncHost:        forceSyncHost || rt.callerResolverActive.Load(),
		moduleIdentity:       mod.moduleIdentity(),
		operationReservation: reservation,
		independentInstances: mod.independentInstances,
		hasExecutionPolicy:   true,
	}
	if hasGC {
		iopts.GC = gc
		iopts.pluginGC = &gc
	}

	// Keep the no-lifecycle-hook path allocation-free.
	if len(hooks.beforeInstantiate) == 0 && len(hooks.afterCreate) == 0 && len(hooks.afterInstantiate) == 0 && len(hooks.onInstantiateError) == 0 {
		return instantiateCoreWithModuleUse(mod, iopts)
	}

	request := InstantiationRequest{Module: moduleView(mod), Origin: origin, reservation: reservation}
	emitError := func(original error) error {
		var hookErrs []error
		for _, fn := range hooks.onInstantiateError {
			observer := fn
			if panicErr := callHookSafely("OnInstantiateError", func() {
				observer(InstantiationErrorEvent{Module: request.Module, Origin: origin, Err: original, reservation: reservation})
			}); panicErr != nil {
				hookErrs = append(hookErrs, panicErr)
			}
		}
		return joinPrimary(original, hookErrs...)
	}

	for _, fn := range hooks.beforeInstantiate {
		interceptor := fn
		var hookErr error
		panicErr := callHookSafely("BeforeInstantiate", func() { hookErr = interceptor(request) })
		if err := joinPrimary(hookErr, panicErr); err != nil {
			return nil, emitError(err)
		}
	}
	iopts.afterCreate = func(inst *Instance) error {
		event := InstantiationEvent{Module: request.Module, Instance: InstanceIdentity{value: inst}, Origin: origin, reservation: reservation}
		for _, fn := range hooks.afterCreate {
			interceptor := fn
			var hookErr error
			panicErr := callHookSafely("InstanceInstantiateInterceptor.After", func() { hookErr = interceptor(event) })
			if err := joinPrimary(hookErr, panicErr); err != nil {
				return err
			}
		}
		return nil
	}
	inst, err := instantiateCoreWithModuleUse(mod, iopts)
	if err != nil {
		return nil, emitError(err)
	}
	for _, fn := range hooks.afterInstantiate {
		observer := fn
		event := InstantiationEvent{Module: request.Module, Instance: InstanceIdentity{value: inst}, Origin: origin, reservation: reservation}
		if err := callHookSafely("InstanceInstantiateObserver", func() { observer(event) }); err != nil {
			failed := joinPrimary(err, inst.Close())
			return nil, emitError(failed)
		}
	}
	return inst, nil
}

func instantiateCoreWithModuleUse(mod *Module, opts InstantiateOptions) (*Instance, error) {
	if !mod.beginUse() {
		return nil, fmt.Errorf("wago: Instantiate: module is closed")
	}
	return instantiateCoreWithModuleLease(mod.c, opts, mod)
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

// Close publishes shutdown and returns promptly. It never waits for teardown,
// so plugin callbacks, host imports, contract calls, invoke hooks, and close
// observers may initiate Runtime closure without self-deadlock. Use
// CloseContext or WaitClosed when completion and the joined teardown error are
// required.
func (rt *Runtime) Close() error {
	if rt == nil {
		return nil
	}
	rt.mu.Lock()
	if rt.closeState != nil {
		rt.mu.Unlock()
		return nil
	}
	state := rt.publishCloseLocked()
	if rt.stateWasLoadingLocked() {
		loadingDone := rt.loadingDone
		rt.mu.Unlock()
		go rt.finishCloseAfterLoading(context.Background(), state, loadingDone)
		return nil
	}
	hooks, internalClose, pluginRuns, store := rt.snapshotCloseLocked()
	rt.mu.Unlock()
	go rt.finishClose(context.Background(), state, hooks, internalClose, pluginRuns, store)
	return nil
}

// Closed returns a channel closed after teardown completes and the final result
// is published. It returns nil until shutdown has started.
func (rt *Runtime) Closed() <-chan struct{} {
	if rt == nil {
		return nil
	}
	rt.mu.Lock()
	state := rt.closeState
	rt.mu.Unlock()
	if state == nil {
		return nil
	}
	return state.done
}

// WaitClosed waits for an already-started shutdown and returns its final joined
// error. It does not initiate shutdown.
func (rt *Runtime) WaitClosed(ctx context.Context) error {
	if rt == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rt.mu.Lock()
	state := rt.closeState
	rt.mu.Unlock()
	if state == nil {
		return fmt.Errorf("wago: runtime shutdown has not started")
	}
	select {
	case <-state.done:
		return state.result
	case <-ctx.Done():
		return ctx.Err()
	}
}

// CloseContext starts shutdown and waits selectably for completion. The first
// caller's context is passed to plugin Stop callbacks; teardown continues to its
// single final result even if this waiter later times out. Shutdown callbacks
// that need reentrant closure must call Close, not WaitClosed or CloseContext.
func (rt *Runtime) CloseContext(ctx context.Context) error {
	if rt == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rt.mu.Lock()
	state := rt.closeState
	if state == nil {
		state = rt.publishCloseLocked()
		if rt.stateWasLoadingLocked() {
			loadingDone := rt.loadingDone
			rt.mu.Unlock()
			go rt.finishCloseAfterLoading(ctx, state, loadingDone)
		} else {
			hooks, internalClose, pluginRuns, store := rt.snapshotCloseLocked()
			rt.mu.Unlock()
			go rt.finishClose(ctx, state, hooks, internalClose, pluginRuns, store)
		}
	} else {
		rt.mu.Unlock()
	}
	select {
	case <-state.done:
		return state.result
	case <-ctx.Done():
		return ctx.Err()
	}
}

// publishCloseLocked publishes shutdown without waiting or running callbacks.
// rt.mu is held by the caller.
func (rt *Runtime) publishCloseLocked() *runtimeCloseState {
	wasLoading := rt.state == runtimeLoading
	state := &runtimeCloseState{done: make(chan struct{})}
	rt.closeState = state
	rt.state = runtimeClosing
	if wasLoading {
		// loadingDone remains owned by the plugin-loading transaction. The close
		// worker waits for its committed generation to finish starting or rolling
		// back before taking the teardown snapshot.
		rt.operational = true
	}
	return state
}

func (rt *Runtime) stateWasLoadingLocked() bool {
	select {
	case <-rt.loadingDone:
		return false
	default:
		return true
	}
}

func (rt *Runtime) snapshotCloseLocked() ([]func(RuntimeCloseEvent), []func() error, []registeredPluginRun, *referenceStore) {
	hooks := rt.loadHooks()
	return append([]func(RuntimeCloseEvent){}, hooks.onRuntimeClose...),
		append([]func() error(nil), hooks.internalClose...),
		append([]registeredPluginRun(nil), rt.pluginRuns...),
		rt.refStore
}

// startCloseLocked publishes shutdown and snapshots teardown state. rt.mu is
// held by the caller and plugin loading has completed.
func (rt *Runtime) startCloseLocked() (*runtimeCloseState, []func(RuntimeCloseEvent), []func() error, []registeredPluginRun, *referenceStore) {
	state := rt.publishCloseLocked()
	hooks, internalClose, pluginRuns, store := rt.snapshotCloseLocked()
	return state, hooks, internalClose, pluginRuns, store
}

func (rt *Runtime) finishCloseAfterLoading(ctx context.Context, state *runtimeCloseState, loadingDone <-chan struct{}) {
	<-loadingDone
	rt.mu.Lock()
	hooks, internalClose, pluginRuns, store := rt.snapshotCloseLocked()
	rt.mu.Unlock()
	rt.finishClose(ctx, state, hooks, internalClose, pluginRuns, store)
}

func (rt *Runtime) finishClose(ctx context.Context, state *runtimeCloseState, hooks []func(RuntimeCloseEvent), internalClose []func() error, pluginRuns []registeredPluginRun, store *referenceStore) {
	var errs []error
	// Compile preparations can retain transformed source, custom instruction
	// implementations, and compile observers between PrepareCompile and their
	// terminal action. Drain them before any plugin teardown callback can run.
	rt.mu.Lock()
	for rt.compileOperations != 0 {
		rt.stateCond.Wait()
	}
	rt.mu.Unlock()
	for i := len(hooks) - 1; i >= 0; i-- {
		observer := hooks[i]
		if err := callShutdownSafely("RuntimeCloseObserver", func() { observer(RuntimeCloseEvent{}) }); err != nil {
			errs = append(errs, err)
		}
	}
	// A Module.Close admitted before shutdown may still be running observers.
	// Drain those callbacks before closing instances or stopping their providers;
	// new module closes see runtimeClosing and suppress lifecycle callbacks.
	rt.mu.Lock()
	for rt.moduleCloseOperations != 0 {
		rt.stateCond.Wait()
	}
	rt.mu.Unlock()

	instances := rt.directInstancesSnapshot()
	for i := len(instances) - 1; i >= 0; i-- {
		if err := instances[i].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	for i := len(pluginRuns) - 1; i >= 0; i-- {
		errs = append(errs, closePluginRun(ctx, pluginRuns[i])...)
	}
	for i := len(pluginRuns) - 1; i >= 0; i-- {
		for j := len(pluginRuns[i].provided) - 1; j >= 0; j-- {
			var revokeErr error
			panicErr := callShutdownSafely("provided contract revoke", func() { revokeErr = pluginRuns[i].provided[j].revoke() })
			if err := joinPrimary(revokeErr, panicErr); err != nil {
				errs = append(errs, err)
			}
		}
	}
	rt.mu.Lock()
	for rt.activeOperations != 0 {
		rt.stateCond.Wait()
	}
	rt.mu.Unlock()
	for i := len(instances) - 1; i >= 0; i-- {
		closeState := instances[i].ensurePluginState().close.Load()
		if closeState != nil {
			<-closeState.done
			if closeState.result != nil {
				errs = append(errs, closeState.result)
			}
		}
	}
	for i := len(internalClose) - 1; i >= 0; i-- {
		var closeErr error
		panicErr := callShutdownSafely("internal runtime close", func() { closeErr = internalClose[i]() })
		if err := joinPrimary(closeErr, panicErr); err != nil {
			errs = append(errs, err)
		}
	}
	if err := callShutdownSafely("reference store close", store.closeRuntime); err != nil {
		errs = append(errs, err)
	}
	result := errors.Join(errs...)
	rt.mu.Lock()
	rt.state = runtimeClosed
	state.result = result
	close(state.done)
	rt.stateCond.Broadcast()
	rt.mu.Unlock()
}

func (rt *Runtime) rollbackCommittedPluginPlan(ctx context.Context) error {
	// Startup rollback is mandatory even when the loading context is already
	// canceled. That context is still delivered to Stop as its cancellation
	// signal, while rollback itself waits independently for complete teardown.
	if ctx == nil {
		ctx = context.Background()
	}
	rt.mu.Lock()
	state := rt.closeState
	if state == nil {
		var hooks []func(RuntimeCloseEvent)
		var internalClose []func() error
		var pluginRuns []registeredPluginRun
		var store *referenceStore
		state, hooks, internalClose, pluginRuns, store = rt.startCloseLocked()
		rt.mu.Unlock()
		go rt.finishClose(ctx, state, hooks, internalClose, pluginRuns, store)
	} else {
		rt.mu.Unlock()
	}
	<-state.done
	err := state.result
	rt.mu.Lock()
	rt.plugins = nil
	rt.imports = Imports{}
	rt.importMeta = map[string]*registeredImport{}
	rt.importOwner = map[string]string{}
	rt.moduleOwner = map[string]string{}
	rt.caps = map[Capability]string{}
	rt.capOrder = nil
	rt.instructions = map[string]*registeredInstruction{}
	rt.storeHooks(&hookRegistry{})
	rt.pluginRuns = nil
	rt.managedActive.Store(false)
	rt.callerResolverActive.Store(false)
	rt.instances = map[*Instance]uint64{}
	rt.instanceSequence = 0
	rt.mu.Unlock()
	return err
}

func closePluginRun(ctx context.Context, run registeredPluginRun) (errs []error) {
	for i := len(run.closeInstances) - 1; i >= 0; i-- {
		var closeErr error
		panicErr := callShutdownSafely("manager close admission", func() { closeErr = run.closeInstances[i]() })
		if err := joinPrimary(closeErr, panicErr); err != nil {
			errs = append(errs, err)
		}
	}
	run.callbacks.deactivate()
	if run.shouldStop && run.lifecycle.Stop != nil {
		var stopErr error
		panicErr := callShutdownSafely("plugin Stop", func() { stopErr = run.lifecycle.Stop(ctx) })
		if err := joinPrimary(stopErr, panicErr); err != nil {
			errs = append(errs, &PluginError{Plugin: run.name, Phase: PluginPhaseStop, Err: err})
		}
	}
	if err := run.callbacks.closeAndWait(); err != nil {
		errs = append(errs, err)
	}
	for i := len(run.drainInstances) - 1; i >= 0; i-- {
		var drainErr error
		panicErr := callShutdownSafely("manager drain", func() { drainErr = run.drainInstances[i]() })
		if err := joinPrimary(drainErr, panicErr); err != nil {
			errs = append(errs, err)
		}
	}
	for i := len(run.consumed) - 1; i >= 0; i-- {
		var revokeErr error
		panicErr := callShutdownSafely("consumed contract revoke", func() { revokeErr = run.consumed[i].revoke() })
		if err := joinPrimary(revokeErr, panicErr); err != nil {
			errs = append(errs, err)
		}
	}
	for i := len(run.handles) - 1; i >= 0; i-- {
		var closeErr error
		panicErr := callShutdownSafely("plugin handle revoke", func() { closeErr = run.handles[i]() })
		if err := joinPrimary(closeErr, panicErr); err != nil {
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
