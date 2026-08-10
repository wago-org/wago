package component

import (
	"context"
	"fmt"

	"github.com/wago-org/wago/plugin"
	"github.com/wago-org/wago/src/component/internal/engine"
	"github.com/wago-org/wago/src/component/internal/instance"
	"github.com/wago-org/wago/src/wago"
)

// PluginID is the stable extension ID for the Component Model runtime.
const PluginID = "wago-org/component-model"

// ServiceName is the versioned Component Model runtime service consumed by
// plugins that provide component-level worlds such as WASI Preview 2.
const ServiceName = "wago-org/component-model/runtime/v1"

// Service is the public execution surface provided by the Component Model
// plugin. Depending plugins should require RuntimeService rather than importing
// engine internals or asking for core-runtime authority themselves.
type Service interface {
	Instantiate(context.Context, []byte, ...Option) (*Instance, error)
}

// RuntimeService identifies the versioned Component Model execution service.
var RuntimeService = plugin.NewServiceKey[Service](ServiceName)

func init() {
	wago.RegisterExtension(PluginID, func() wago.Extension { return NewExtension() })
}

// Extension installs Component Model execution into a Wago runtime. Use Enable
// for programmatic installation, or register NewExtension in a manifest-driven
// host with the runtime.core plugin capability granted.
type Extension struct {
	runtime *Runtime
}

// NewExtension returns an unregistered Component Model extension.
func NewExtension() *Extension { return &Extension{} }

func (*Extension) Info() wago.ExtensionInfo {
	return wago.ExtensionInfo{
		ID:                   PluginID,
		Name:                 "WebAssembly Component Model",
		Description:          "Decodes, links, and executes WebAssembly Components",
		Stability:            wago.Experimental,
		Repository:           "https://github.com/wago-org/wago",
		License:              "BSD-3-Clause",
		Tags:                 []string{"component-model", "canonical-abi"},
		RequiresCapabilities: []wago.PluginCapability{wago.PluginCoreEngine},
	}
}

func (e *Extension) Register(reg *wago.Registry) error {
	access, err := reg.CoreEngine()
	if err != nil {
		return err
	}
	e.runtime = &Runtime{engine: access}
	return plugin.Provide[Service](reg, RuntimeService, e.runtime)
}

// Runtime is the capability-scoped Component Model execution service installed
// in one Wago runtime. It cannot be moved to or used with another runtime.
type Runtime struct {
	engine wago.CoreEngine
}

// Enable installs the Component Model plugin with its required authority and
// returns the runtime-scoped component service.
func Enable(rt *wago.Runtime) (*Runtime, error) {
	if rt == nil {
		return nil, fmt.Errorf("component: enable on nil Wago runtime")
	}
	if err := rt.UsePlugin(PluginID, wago.WithPluginGrants(wago.PluginCoreEngine)); err != nil {
		return nil, err
	}
	return FromRuntime(rt)
}

// FromRuntime resolves the installed Component Model plugin. It fails closed
// when the plugin is absent, has the wrong concrete implementation, or its
// runtime authority has been revoked.
func FromRuntime(rt *wago.Runtime) (*Runtime, error) {
	if rt == nil {
		return nil, fmt.Errorf("component: nil Wago runtime")
	}
	ext, ok := rt.Extension(PluginID)
	if !ok {
		return nil, fmt.Errorf("component: plugin %q is not enabled", PluginID)
	}
	plugin, ok := ext.(*Extension)
	if !ok || plugin == nil || plugin.runtime == nil || plugin.runtime.engine == nil {
		return nil, fmt.Errorf("component: plugin %q has an invalid implementation", PluginID)
	}
	return plugin.runtime, nil
}

// Instantiate decodes and instantiates a component through this plugin's
// authorized core-runtime handle.
func (r *Runtime) Instantiate(ctx context.Context, componentBytes []byte, opts ...Option) (*Instance, error) {
	if r == nil || r.engine == nil {
		return nil, fmt.Errorf("component: nil or inactive component runtime")
	}
	return instance.Instantiate(ctx, engine.Wrap(r.engine), componentBytes, opts...)
}
