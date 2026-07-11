package wago

import (
	"fmt"
	"sort"
	"sync"
)

// ExtensionFactory constructs a fresh extension instance. Plugins are registered
// under a stable canonical module path so a binary that compiles them in can enable them
// (e.g. from a --plugin CLI flag). This is the database/sql-style registry: it
// selects among what is compiled into the binary, since Go cannot load native
// code at runtime.
type ExtensionFactory func() Extension

var (
	pluginMu  sync.Mutex
	pluginReg = map[string]ExtensionFactory{}
)

// HostEnvironment is the narrow host state explicitly exposed to plugins.
type HostEnvironment struct{}

// GuestArgs returns a defensive copy of the current guest command line.
func (*HostEnvironment) GuestArgs() []string { return GuestArgs() }

// The per-process host environment that host-import plugins draw on when their
// factory takes no per-run config. A host program (e.g. the CLI) sets the guest
// command line before a run; a plugin factory can read it without engine-specific
// integration.
var (
	hostEnvMu sync.Mutex
	guestArgs []string
)

// SetGuestArgs records the guest command line (argv) for the current run.
func SetGuestArgs(args []string) {
	hostEnvMu.Lock()
	guestArgs = args
	hostEnvMu.Unlock()
}

// GuestArgs returns the guest command line set for the current run, or nil.
func GuestArgs() []string {
	hostEnvMu.Lock()
	defer hostEnvMu.Unlock()
	return guestArgs
}

// RegisterExtension registers a plugin factory under a short, stable name (e.g.
// "timer"). Call it from an init() in the binary that compiles the plugin in. It
// panics on an empty name, a nil factory, or a duplicate name — all build-time
// programming errors.
func RegisterExtension(name string, factory ExtensionFactory) {
	if name == "" || factory == nil {
		panic("wago: RegisterExtension: empty name or nil factory")
	}
	pluginMu.Lock()
	defer pluginMu.Unlock()
	if _, dup := pluginReg[name]; dup {
		panic("wago: RegisterExtension: duplicate plugin " + name)
	}
	pluginReg[name] = factory
}

// NewExtension constructs a registered plugin by name. The boolean is false for
// an unregistered name.
func NewExtension(name string) (Extension, bool) {
	pluginMu.Lock()
	f := pluginReg[name]
	pluginMu.Unlock()
	if f == nil {
		return nil, false
	}
	return f(), true
}

// RegisteredPluginNames returns the names of all plugins compiled into this
// binary, sorted.
func RegisteredPluginNames() []string {
	pluginMu.Lock()
	defer pluginMu.Unlock()
	names := make([]string, 0, len(pluginReg))
	for n := range pluginReg {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// UsePlugin resolves a registered plugin by name and registers it on the runtime.
func (rt *Runtime) UsePlugin(name string, opts ...UseOption) error {
	ext, ok := NewExtension(name)
	if !ok {
		return fmt.Errorf("wago: unknown plugin %q (registered: %v)", name, RegisteredPluginNames())
	}
	return rt.Use(ext, opts...)
}

// Extension returns the registered extension instance with the given stable
// extension ID. This lets a host retrieve a plugin-owned Go service after
// selecting the plugin indirectly through UsePlugin. The returned extension is
// owned by the runtime and must not be registered with another runtime.
func (rt *Runtime) Extension(id string) (Extension, bool) {
	if rt == nil {
		return nil, false
	}
	rt.mu.Lock()
	ext, ok := rt.extensions[id]
	rt.mu.Unlock()
	return ext, ok
}

// HostImports returns a copy of the host-import bindings the runtime's registered
// extensions provide, keyed by "module.name". It lets the low-level Instantiate
// path be fed plugin-provided imports without going through Runtime.Instantiate.
func (rt *Runtime) HostImports() Imports {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make(Imports, len(rt.imports))
	for k, v := range rt.imports {
		out[k] = v
	}
	return out
}

// ProvidedImports returns the host imports the runtime's registered extensions
// provide, as ImportSpecs (module, name, declared signature, capability, docs),
// sorted by "module.name". It powers inspection/CLI output. Provided is always
// true here (these are the bindings the runtime supplies).
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
