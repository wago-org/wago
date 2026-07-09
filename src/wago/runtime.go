package wago

import (
	"context"
	"fmt"
	"sort"
	"sync"

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

// reservedModules are the wago_* import namespaces the built-in extensions own.
// A per-call import may not shadow one unless the override policy allows it.
var reservedModules = map[string]struct{}{
	"wago_process": {}, "wago_mailbox": {}, "wago_timer": {}, "wago_metrics": {},
	"wago_log": {}, "wago_fs": {}, "wago_net": {}, "wago_http": {}, "wago_kv": {},
	"wago_crypto": {}, "wago_debug": {}, "wago_runtime": {},
}

// Runtime is the high-level entry point: extensions register capabilities and
// host imports into it, and it threads those through Compile/Instantiate. The
// package-level Compile/Instantiate remain available as the low-level API.
type Runtime struct {
	mu             sync.Mutex
	cfg            *RuntimeConfig
	overridePolicy ImportOverridePolicy
	hooks          *HookRegistry

	exts        []ExtensionInfo
	imports     Imports                      // "module.name" -> host fn (any)
	importMeta  map[string]*registeredImport // "module.name" -> declared signature/cap/docs
	importOwner map[string]string            // "module.name" -> owning extension ID
	moduleOwner map[string]string            // import module -> owning extension ID
	caps        map[Capability]string
	capOrder    []Capability
	closed      bool

	procMu    sync.Mutex
	procs     map[PID]*Process
	procNames map[string]PID
	nextPID   PID
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
		imports:     Imports{},
		importMeta:  map[string]*registeredImport{},
		importOwner: map[string]string{},
		moduleOwner: map[string]string{},
		caps:        map[Capability]string{},
		procs:       map[PID]*Process{},
		procNames:   map[string]PID{},
		nextPID:     1,
	}
	for _, opt := range opts {
		opt(rt)
	}
	if rt.cfg == nil {
		rt.cfg = NewRuntimeConfig()
	}
	return rt
}

// UseOption reserved for per-registration configuration (none yet).
type UseOption func(*useConfig)

type useConfig struct{}

// Use registers an extension: it runs the extension's Register, checks version
// compatibility, and merges the declared capabilities and host imports. Import
// collisions are rejected per the runtime's override policy, leaving the runtime
// unchanged on error.
func (rt *Runtime) Use(ext Extension, _ ...UseOption) error {
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

	// Register into a scratch registry so a failure leaves the runtime untouched.
	reg := &Registry{info: info, hooks: rt.hooks}
	if err := ext.Register(reg); err != nil {
		return &ExtensionError{Extension: info.ID, Operation: "register", Err: err}
	}

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
	rt.exts = append(rt.exts, info)
	return nil
}

// Compile compiles a wasm module under the runtime's configuration and wraps it
// as a *Module, resolving its imports against the registered extensions and
// running any AfterCompile hooks.
func (rt *Runtime) Compile(wasmBytes []byte) (*Module, error) {
	c, err := Compile(rt.cfg, wasmBytes)
	if err != nil {
		return nil, err
	}
	mod := rt.buildModule(c)
	if len(rt.hooks.afterCompile) > 0 {
		cctx := &CompileContext{Runtime: rt, Metadata: map[string]any{}}
		for _, fn := range rt.hooks.afterCompile {
			if err := fn(cctx, mod); err != nil {
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
// instantiate work begins. A function import that no extension or per-call import
// provides is reported with a hint rather than a downstream binding failure.
func (rt *Runtime) Instantiate(ctx context.Context, mod *Module, opts ...InstantiateOption) (*Instance, error) {
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

	hctx := &InstantiateContext{Runtime: rt, Module: mod, Compiled: mod.c, Imports: merged, Metadata: map[string]any{}}
	for _, fn := range rt.hooks.beforeInstantiate {
		if err := fn(hctx); err != nil {
			return nil, err
		}
	}

	iopts := InstantiateOptions{Imports: merged}
	if cfg.hasGC {
		iopts.GC = cfg.gc
	}
	inst, err := instantiateCore(mod.c, iopts)
	if err != nil {
		return nil, err
	}
	inst.rt = rt // enable Instance.Call invoke hooks
	for _, fn := range rt.hooks.afterInstantiate {
		if err := fn(hctx, inst); err != nil {
			inst.Close()
			return nil, err
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

// Close runs runtime-close hooks (in reverse registration order) and marks the
// runtime unusable. It does not close instances the caller still holds.
func (rt *Runtime) Close() error {
	rt.mu.Lock()
	if rt.closed {
		rt.mu.Unlock()
		return nil
	}
	rt.closed = true
	hooks := rt.hooks.onRuntimeClose
	rt.mu.Unlock()

	rctx := &RuntimeContext{Runtime: rt}
	for i := len(hooks) - 1; i >= 0; i-- {
		hooks[i](rctx)
	}
	return nil
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
		return fmt.Errorf("invalid wago version constraint %q: %w", constraint)
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
