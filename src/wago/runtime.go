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
	// NoExtensionOverrides is the default: extension import namespaces must be
	// unique, and per-call imports may not replace a reserved wago_* module.
	NoExtensionOverrides ImportOverridePolicy = iota
	// AllowTestOverrides relaxes both rules, for tests that need to stub a
	// reserved module or a previously-registered import.
	AllowTestOverrides
)

// reservedModules are standard wago_* extension namespaces. Their
// implementations live in plugins; a per-call import may not shadow one unless
// the override policy allows it.
var reservedModules = map[string]struct{}{
	"wago_process": {}, "wago_mailbox": {}, "wago_timer": {}, "wago_metrics": {},
	"wago_log": {}, "wago_fs": {}, "wago_net": {}, "wago_http": {}, "wago_kv": {},
	"wago_crypto": {}, "wago_debug": {}, "wago_runtime": {},
}

// Runtime is the high-level entry point: extensions register capabilities and
// host imports into it, and it threads those through Compile/Instantiate. The
// package-level Compile/Instantiate remain available as the low-level API.
type Runtime struct {
	mu                   sync.Mutex
	cfg                  *RuntimeConfig
	overridePolicy       ImportOverridePolicy
	managedActive        atomic.Bool
	callerResolverActive atomic.Bool
	hooks                *HookRegistry
	refStore             *referenceStore

	exts        []ExtensionInfo
	extensions  map[string]Extension
	imports     Imports                      // "module.name" -> host fn (any)
	importMeta  map[string]*registeredImport // "module.name" -> declared signature/cap/docs
	importOwner map[string]string            // "module.name" -> owning extension ID
	moduleOwner map[string]string            // import module -> owning extension ID
	caps        map[Capability]string
	capOrder    []Capability
	closed      bool
	pluginStops []registeredPluginStop
}

type registeredPluginStop struct {
	name string
	stop func(context.Context) error
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

// NewRuntime creates a runtime with no extensions registered.
func NewRuntime(opts ...RuntimeOption) *Runtime {
	rt := &Runtime{
		cfg:         NewRuntimeConfig(),
		hooks:       &HookRegistry{},
		refStore:    newReferenceStore(false),
		extensions:  map[string]Extension{},
		imports:     Imports{},
		importMeta:  map[string]*registeredImport{},
		importOwner: map[string]string{},
		moduleOwner: map[string]string{},
		caps:        map[Capability]string{},
	}
	for _, opt := range opts {
		opt(rt)
	}
	if rt.cfg == nil {
		rt.cfg = NewRuntimeConfig()
	}
	return rt
}

// UseOption configures one programmatic plugin registration. Manifest-driven
// loading supplies these from wago.json after validating the complete load DAG.
type UseOption func(*useConfig)

type useConfig struct {
	strict bool
	grants map[PluginCapability]struct{}
	config []byte
}

// WithPluginGrants explicitly grants privileged plugin capabilities. Supplying
// this option makes registration strict: every declared and exercised plugin
// capability must be present.
func WithPluginGrants(caps ...PluginCapability) UseOption {
	return func(cfg *useConfig) {
		cfg.strict = true
		if cfg.grants == nil {
			cfg.grants = map[PluginCapability]struct{}{}
		}
		for _, cap := range caps {
			cfg.grants[cap] = struct{}{}
		}
	}
}

// Use registers an extension: it runs the extension's Register, checks version
// compatibility, and merges the declared capabilities and host imports. Import
// collisions are rejected per the runtime's override policy, leaving the runtime
// unchanged on error.
func (rt *Runtime) Use(ext Extension, opts ...UseOption) error {
	if ext == nil {
		return fmt.Errorf("wago: Use: nil extension")
	}
	info := ext.Info()
	if info.ID == "" {
		return fmt.Errorf("wago: Use: extension has no ID")
	}
	if err := checkCompat(info.Compat); err != nil {
		return &ExtensionError{Extension: info.ID, Operation: "use", Err: err}
	}

	cfg := useConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.strict {
		for cap := range cfg.grants {
			if !validPluginCapability(cap) {
				return &ExtensionError{Extension: info.ID, Operation: "authorize", Err: fmt.Errorf("unknown plugin capability %q", cap)}
			}
		}
		for _, cap := range info.RequiresCapabilities {
			if _, ok := cfg.grants[cap]; !ok {
				return &ExtensionError{Extension: info.ID, Operation: "authorize", Err: fmt.Errorf("plugin capability %q was not granted: %w", cap, ErrPermissionDenied)}
			}
		}
	}

	// Register into a scratch registry so a failure leaves the runtime untouched,
	// including hooks (which must not become active until the whole Use commits).
	reg := &Registry{info: info, hooks: &HookRegistry{}, grants: cfg.grants, config: cfg.config}
	if err := ext.Register(reg); err != nil {
		return &ExtensionError{Extension: info.ID, Operation: "register", Err: err}
	}
	if cfg.strict {
		for _, cap := range reg.requiredPluginCapabilities() {
			if _, ok := cfg.grants[cap]; !ok {
				return &ExtensionError{Extension: info.ID, Operation: "authorize", Err: fmt.Errorf("plugin exercised capability %q without a grant: %w", cap, ErrPermissionDenied)}
			}
		}
	}

	commitErr := func() error {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		if rt.closed {
			return fmt.Errorf("wago: Use on a closed runtime")
		}
		for _, id := range rt.exts {
			if id.ID == info.ID {
				return &ExtensionError{Extension: info.ID, Operation: "use", Err: ErrExtensionConflict}
			}
		}
		// Validate all imports before mutating any runtime state.
		for _, imp := range reg.imports {
			if imp.fn == nil {
				return &ExtensionError{Extension: info.ID, Operation: "register",
					Err: fmt.Errorf("import %q has no function", imp.key())}
			}
			if owner, ok := rt.moduleOwner[imp.module]; ok && owner != info.ID && rt.overridePolicy != AllowTestOverrides {
				return &ExtensionError{Extension: info.ID, Operation: "register",
					Err: fmt.Errorf("import module %q already owned by extension %q: %w", imp.module, owner, ErrExtensionConflict)}
			}
			if owner, ok := rt.importOwner[imp.key()]; ok && owner != info.ID && rt.overridePolicy != AllowTestOverrides {
				return &ExtensionError{Extension: info.ID, Operation: "register",
					Err: fmt.Errorf("import %q already provided by extension %q: %w", imp.key(), owner, ErrExtensionConflict)}
			}
		}

		// Commit.
		for _, imp := range reg.imports {
			rt.imports[imp.key()] = imp.fn
			rt.importMeta[imp.key()] = imp
			rt.importOwner[imp.key()] = info.ID
			rt.moduleOwner[imp.module] = info.ID
		}
		for _, spec := range reg.caps {
			if _, ok := rt.caps[spec.cap]; !ok {
				rt.capOrder = append(rt.capOrder, spec.cap)
			}
			rt.caps[spec.cap] = info.ID
		}
		rt.hooks.appendFrom(reg.hooks)
		for _, manager := range reg.managers {
			manager.activate(rt)
		}
		for _, activate := range reg.activate {
			activate(rt)
		}
		rt.exts = append(rt.exts, info)
		rt.extensions[info.ID] = ext
		return nil
	}()
	if commitErr != nil {
		return commitErr
	}
	return rt.startPluginPlan(context.Background(), []plannedExtension{{name: info.ID, ext: ext, info: info, reg: reg}})
}

// Compile compiles a wasm module under the runtime's configuration and wraps it
// as a *Module, resolving its imports against the registered extensions and
// running any AfterCompile hooks.
func (rt *Runtime) Compile(wasmBytes []byte) (*Module, error) {
	ctx := &CompileContext{Runtime: rt, Metadata: map[string]any{}}
	source := wasmBytes
	for _, fn := range rt.hooks.beforeCompile {
		next, err := fn(ctx, source)
		if err != nil {
			return nil, err
		}
		if next != nil {
			source = next
		}
	}
	c, err := Compile(rt.cfg, source)
	if err != nil {
		return nil, err
	}
	mod := rt.buildModule(c)
	if len(rt.hooks.afterCompile) > 0 {
		for _, fn := range rt.hooks.afterCompile {
			if err := fn(ctx, mod); err != nil {
				return nil, err
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
	rt.mu.Lock()
	closed := rt.closed
	rt.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("wago: Module on a closed runtime")
	}
	mod := rt.buildModule(c)
	if len(rt.hooks.afterCompile) > 0 {
		ctx := &CompileContext{Runtime: rt, Metadata: map[string]any{}}
		for _, fn := range rt.hooks.afterCompile {
			if err := fn(ctx, mod); err != nil {
				return nil, err
			}
		}
	}
	return mod, nil
}

// InstantiateOption configures a single Instantiate call.
type InstantiateOption func(*instantiateConfig)

type instantiateConfig struct {
	imports Imports
	gc      GCConfig
	hasGC   bool
	policy  Policy
}

// WithPolicy applies a capability/resource policy to the instance. A module that
// requires a capability the policy does not allow (or that exceeds a resource
// limit) is rejected with an error wrapping ErrPermissionDenied.
func WithPolicy(p Policy) InstantiateOption {
	return func(c *instantiateConfig) { c.policy = p }
}

// WithImports adds per-call imports on top of the extension-provided namespace.
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

// Instantiate instantiates a module, wiring the runtime's extension imports plus
// any per-call imports. ctx is honored for cancellation before the (synchronous)
// instantiate work begins. Runtime ownership is attached before start executes;
// a failed start or AfterInstantiate hook closes the partial instance through the
// normal lifecycle before returning its joined failure. A function import that no
// extension or per-call import provides is reported with a hint rather than a
// downstream binding failure.
func (rt *Runtime) Instantiate(ctx context.Context, mod *Module, opts ...InstantiateOption) (*Instance, error) {
	return rt.instantiateOrigin(ctx, mod, InstantiateDirect, opts...)
}

func (rt *Runtime) instantiateOrigin(ctx context.Context, mod *Module, origin InstantiateOrigin, opts ...InstantiateOption) (*Instance, error) {
	if mod == nil {
		return nil, fmt.Errorf("wago: Instantiate: nil module")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var cfg instantiateConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	if err := applyPolicy(mod, cfg.policy); err != nil {
		return nil, err
	}

	rt.mu.Lock()
	if rt.closed {
		rt.mu.Unlock()
		return nil, fmt.Errorf("wago: Instantiate on a closed runtime")
	}
	// Merge extension imports first, then per-call imports on top.
	merged := make(Imports, len(rt.imports)+len(cfg.imports))
	for k, v := range rt.imports {
		merged[k] = v
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

	return rt.instantiateWithHooksOrigin(mod, merged, cfg.gc, cfg.hasGC, origin)
}

// instantiateWithHooks runs a direct Runtime-aware instantiation.
func (rt *Runtime) instantiateWithHooks(mod *Module, imports Imports, gc GCConfig, hasGC bool) (*Instance, error) {
	return rt.instantiateWithHooksOrigin(mod, imports, gc, hasGC, InstantiateDirect)
}

// instantiateWithHooksOrigin runs the Runtime-aware instantiation path and emits
// plugin lifecycle callbacks around the low-level instantiator.
func (rt *Runtime) instantiateWithHooksOrigin(mod *Module, imports Imports, gc GCConfig, hasGC bool, origin InstantiateOrigin) (*Instance, error) {
	iopts := InstantiateOptions{Imports: imports, store: rt.refStore, runtime: rt, origin: origin}
	if hasGC {
		iopts.GC = gc
		iopts.pluginGC = &gc
	}

	// Keep the no-lifecycle-hook path allocation-free. The instance still retains
	// rt so invoke/close hooks registered before later calls can be observed.
	if len(rt.hooks.beforeInstantiate) == 0 && len(rt.hooks.afterInstantiate) == 0 && len(rt.hooks.onInstantiateError) == 0 {
		return instantiateCore(mod.c, iopts)
	}

	hctx := &InstantiateContext{Runtime: rt, Module: mod, Compiled: mod.c, Imports: imports, Origin: origin, Metadata: map[string]any{}}
	emitError := func(original error) error {
		var hookErrs []error
		for _, fn := range rt.hooks.onInstantiateError {
			if panicErr := callHookSafely("OnInstantiateError", func() { fn(hctx, original) }); panicErr != nil {
				hookErrs = append(hookErrs, panicErr)
			}
		}
		return joinPrimary(original, hookErrs...)
	}

	for _, fn := range rt.hooks.beforeInstantiate {
		var hookErr error
		panicErr := callHookSafely("BeforeInstantiate", func() { hookErr = fn(hctx) })
		if err := joinPrimary(hookErr, panicErr); err != nil {
			return nil, emitError(err)
		}
	}
	iopts.Imports = hctx.Imports

	inst, err := instantiateCore(mod.c, iopts)
	if err != nil {
		return nil, emitError(err)
	}
	for _, fn := range rt.hooks.afterInstantiate {
		var hookErr error
		panicErr := callHookSafely("AfterInstantiate", func() { hookErr = fn(hctx, inst) })
		if err := joinPrimary(hookErr, panicErr); err != nil {
			failed := joinPrimary(err, inst.Close())
			return nil, emitError(failed)
		}
	}
	return inst, nil
}

// Extensions returns the registered extensions in registration order.
func (rt *Runtime) Extensions() []ExtensionInfo {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return append([]ExtensionInfo(nil), rt.exts...)
}

// Capabilities returns the capabilities declared by registered extensions,
// sorted.
func (rt *Runtime) Capabilities() []Capability {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	caps := append([]Capability(nil), rt.capOrder...)
	sort.Slice(caps, func(i, j int) bool { return caps[i] < caps[j] })
	return caps
}

// Close stops plugins, runs runtime-close hooks in reverse registration order,
// closes managed instances, and marks the runtime unusable. Direct instances
// remain caller-owned.
func (rt *Runtime) Close() error { return rt.CloseContext(context.Background()) }

// CloseContext stops plugins in reverse load order, then closes internal
// services and runtime hooks. It is idempotent.
func (rt *Runtime) CloseContext(ctx context.Context) error {
	rt.mu.Lock()
	if rt.closed {
		rt.mu.Unlock()
		return nil
	}
	rt.closed = true
	hooks := rt.hooks.onRuntimeClose
	internalClose := append([]func() error(nil), rt.hooks.internalClose...)
	pluginStops := append([]registeredPluginStop(nil), rt.pluginStops...)
	store := rt.refStore
	rt.mu.Unlock()

	var errs []error
	for i := len(pluginStops) - 1; i >= 0; i-- {
		if err := pluginStops[i].stop(ctx); err != nil {
			errs = append(errs, &PluginError{Plugin: pluginStops[i].name, Phase: PluginPhaseStop, Err: err})
		}
	}
	for i := len(internalClose) - 1; i >= 0; i-- {
		if err := internalClose[i](); err != nil {
			errs = append(errs, err)
		}
	}
	rctx := &RuntimeContext{Runtime: rt}
	for i := len(hooks) - 1; i >= 0; i-- {
		hooks[i](rctx)
	}
	store.closeRuntime()
	return errors.Join(errs...)
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
	_, ok := reservedModules[module]
	return ok
}

// missingImportError explains an unsatisfied function import and hints at the
// fix, wrapping ErrMissingImport for errors.Is.
func missingImportError(spec ImportSpec) error {
	hint := fmt.Sprintf("provide it via WithImports or an extension that registers module %q", spec.Module)
	if isReserved(spec.Module) {
		hint = fmt.Sprintf("register the extension that provides %q, e.g. rt.Use(<ext>.Ext(...))", spec.Module)
	}
	return fmt.Errorf("module imports %q, but nothing provides it; %s: %w", spec.Key(), hint, ErrMissingImport)
}

// checkCompat validates the running wago Version against an extension's declared
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
