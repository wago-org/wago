package wago

import (
	"fmt"
	"sort"
	"sync"
)

// ExtensionFactory constructs a fresh extension instance. Plugins are registered
// under a short name so a binary that compiles them in can enable them by name
// (e.g. from a --plugin CLI flag). This is the database/sql-style registry: it
// selects among what is compiled into the binary, since Go cannot load native
// code at runtime.
type ExtensionFactory func() Extension

var (
	pluginMu  sync.Mutex
	pluginReg = map[string]ExtensionFactory{}
)

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
