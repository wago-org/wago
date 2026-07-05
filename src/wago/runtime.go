package wago

import (
	"context"
	"fmt"
	"sort"
	"sync"
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
	imports     Imports           // "module.name" -> host fn (any)
	importOwner map[string]string // "module.name" -> owning extension ID
	moduleOwner map[string]string // import module -> owning extension ID
	caps        map[Capability]string
	capOrder    []Capability
	closed      bool
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
	if info.MinWago != "" && compareVersions(info.MinWago, Version) > 0 {
		return &ExtensionError{Extension: info.ID, Operation: "use",
			Err: fmt.Errorf("requires wago >= %s, have %s", info.MinWago, Version)}
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

// Compile compiles a wasm module under the runtime's configuration.
func (rt *Runtime) Compile(wasmBytes []byte) (*Compiled, error) {
	return CompileWithConfig(rt.cfg, wasmBytes)
}

// InstantiateOption configures a single Instantiate call.
type InstantiateOption func(*instantiateConfig)

type instantiateConfig struct {
	imports Imports
	gc      GCConfig
	hasGC   bool
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

// Instantiate instantiates a compiled module, wiring the runtime's extension
// imports plus any per-call imports. ctx is accepted for forward compatibility
// with the context-aware instance API and cancellation; it is honored for
// cancellation before the (synchronous) instantiate work begins.
func (rt *Runtime) Instantiate(ctx context.Context, c *Compiled, opts ...InstantiateOption) (*Instance, error) {
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

	hctx := &InstantiateContext{Runtime: rt, Compiled: c, Imports: merged, Metadata: map[string]any{}}
	for _, fn := range rt.hooks.beforeInstantiate {
		if err := fn(hctx); err != nil {
			return nil, err
		}
	}

	iopts := InstantiateOptions{Imports: merged}
	if cfg.hasGC {
		iopts.GC = cfg.gc
	}
	inst, err := InstantiateWithOptions(c, iopts)
	if err != nil {
		return nil, err
	}
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

// compareVersions does a numeric dotted-version compare (e.g. "0.2.0" > "0.1.9").
// Non-numeric or ragged components compare lexically / by presence. It returns
// -1, 0, or 1.
func compareVersions(a, b string) int {
	as, bs := splitVersion(a), splitVersion(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

func splitVersion(v string) []int {
	var out []int
	cur, has := 0, false
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c >= '0' && c <= '9' {
			cur, has = cur*10+int(c-'0'), true
			continue
		}
		if c == '.' {
			out = append(out, cur)
			cur, has = 0, false
			continue
		}
		// Stop at the first non-numeric, non-dot char (e.g. a pre-release suffix).
		break
	}
	if has || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}
