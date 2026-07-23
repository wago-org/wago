package wago

import (
	"encoding/json"
)

// Registry is the builder surface an extension uses inside Register. It records
// the capabilities, host imports, and hooks the extension contributes; the
// runtime reads the recorded state after Register returns and wires it in.
type Registry struct {
	info         ExtensionInfo
	caps         []capabilitySpec
	imports      []*registeredImport
	hooks        *HookRegistry
	managers     []*InstanceManager
	activate     []func(*Runtime)
	provides     []serviceProvision
	requires     []serviceBinder
	grants       map[PluginCapability]struct{}
	budgets      map[PluginCapability]CapabilityBudget
	used         map[PluginCapability]struct{}
	config       json.RawMessage
	compiler     *CompilerRegistry
	instructions []*registeredInstruction
}

// ManagedInstances requests a plugin-scoped runtime instance owner. Manifest
// loading requires the instance.manage capability. The returned handle remains
// inactive until the complete plugin plan commits.
func (r *Registry) ManagedInstances() (*InstanceManager, error) {
	if err := r.authorize(PluginManagedInstances); err != nil {
		return nil, err
	}
	m := newPendingInstanceManager(r.info.ID, r.budgets[PluginManagedInstances])
	r.managers = append(r.managers, m)
	r.hooks.internalClose = append(r.hooks.internalClose, m.close)
	return m, nil
}

// Granted reports whether the manifest authorized this plugin capability.
func (r *Registry) Granted(cap PluginCapability) bool {
	_, ok := r.grants[cap]
	return ok
}

// Config decodes the plugin's opaque wago.json configuration. An absent config
// decodes as an empty JSON object.
func (r *Registry) Config(dst any) error {
	b := r.config
	if len(b) == 0 {
		b = []byte("{}")
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return &PluginError{Plugin: r.info.ID, Phase: PluginPhaseConfigure, Path: "config", Err: err}
	}
	return nil
}

// HostEnvironment returns the deliberately exposed host environment view when
// authorized. It does not expose os.Environ, files, sockets, or process control.
func (r *Registry) HostEnvironment() (*HostEnvironment, error) {
	if err := r.authorize(PluginHostEnvironment); err != nil {
		return nil, err
	}
	return &HostEnvironment{}, nil
}

func (r *Registry) requiredPluginCapabilities() []PluginCapability {
	set := map[PluginCapability]struct{}{}
	for cap := range r.used {
		set[cap] = struct{}{}
	}
	if len(r.imports) != 0 {
		set[PluginHostImports] = struct{}{}
	}
	if r.hooks != nil {
		for _, cap := range r.hooks.requiredPluginCapabilities() {
			set[cap] = struct{}{}
		}
	}
	out := make([]PluginCapability, 0, len(set))
	for cap := range set {
		out = append(out, cap)
	}
	return out
}

// capabilitySpec is a declared capability plus optional docs.
type capabilitySpec struct {
	cap  Capability
	docs string
}

// registeredImport is one host function an extension exposes to guests, keyed by
// its wasm ("module", "name"). params/results are the declared signature (used
// for the manifest and later validation); the actual binding uses the importing
// module's own signature. fn is always a HostFunc — the reflection-free stack
// form — so a plugin's host imports bind identically under standard Go and TinyGo.
type registeredImport struct {
	module  string
	name    string
	fn      HostFunc
	params  []ValType
	results []ValType
	cap     Capability
	hasCap  bool
	docs    string
}

func (i *registeredImport) key() string { return i.module + "." + i.name }

// Capability declares that this extension provides cap. A policy can then allow
// or deny it, and inspection surfaces it to users.
func (r *Registry) Capability(cap Capability, opts ...CapabilityOption) {
	spec := capabilitySpec{cap: cap}
	for _, opt := range opts {
		opt(&spec)
	}
	r.caps = append(r.caps, spec)
}

// CapabilityOption configures a declared capability.
type CapabilityOption func(*capabilitySpec)

// CapabilityDocs attaches a human description to a capability declaration.
func CapabilityDocs(docs string) CapabilityOption {
	return func(s *capabilitySpec) { s.docs = docs }
}

// ImportModule begins declaring host imports under a wasm import module name
// (e.g. "wago_timer"). Call Func on the returned builder for each function.
func (r *Registry) ImportModule(name string) *ImportModuleBuilder {
	return &ImportModuleBuilder{reg: r, module: name}
}

// Hooks returns the hook registry for observing runtime and instance lifecycle.
func (r *Registry) Hooks() *HookRegistry { return r.hooks }

// Compiler returns the trusted compiler contribution registry.
func (r *Registry) Compiler() *CompilerRegistry {
	if r.compiler == nil {
		r.compiler = &CompilerRegistry{reg: r}
	}
	return r.compiler
}

// ImportModuleBuilder scopes host-import declarations to one wasm module name.
type ImportModuleBuilder struct {
	reg    *Registry
	module string
}

// Func declares a host function named `name` in this module. fn is a
// HostFunc: it reads its wasm params from params (i32/f32 in the low 32 bits)
// and writes results into results, with the calling instance's memory available
// via the HostModule. This reflection-free stack form is the single, portable way
// to write a plugin host import — it binds identically under standard Go and
// TinyGo. A bare func literal of the same shape is accepted without an explicit
// HostFunc conversion. Chain Params/Results/Capability on the returned builder
// to record the signature and required capability.
func (m *ImportModuleBuilder) Func(name string, fn HostFunc) *ImportFuncBuilder {
	imp := &registeredImport{module: m.module, name: name, fn: fn}
	m.reg.imports = append(m.reg.imports, imp)
	return &ImportFuncBuilder{imp: imp}
}

// ImportFuncBuilder records the declared signature and metadata of one host
// import. The methods mutate the import in place and return the builder for
// chaining.
type ImportFuncBuilder struct {
	imp *registeredImport
}

// Params records the host function's wasm parameter types.
func (f *ImportFuncBuilder) Params(types ...ValType) *ImportFuncBuilder {
	f.imp.params = append(f.imp.params[:0], types...)
	return f
}

// Results records the host function's wasm result types.
func (f *ImportFuncBuilder) Results(types ...ValType) *ImportFuncBuilder {
	f.imp.results = append(f.imp.results[:0], types...)
	return f
}

// Capability records the capability a guest must hold to call this import.
func (f *ImportFuncBuilder) Capability(cap Capability) *ImportFuncBuilder {
	f.imp.cap, f.imp.hasCap = cap, true
	return f
}

// Docs attaches a human description used by manifest/CLI inspection.
func (f *ImportFuncBuilder) Docs(docs string) *ImportFuncBuilder {
	f.imp.docs = docs
	return f
}
