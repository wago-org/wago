// Package workers provides Wago's neutral worker primitives as a composable
// plugin. It deliberately does not define actors, PIDs, guest mailboxes,
// signals, monitors, or supervision policy.
package workers

import "github.com/wago-org/wago"

const PluginName = "workers"

// Plugin activates one extension-scoped worker service. Embed or retain a
// Plugin when building a higher-level actor extension, then use Service after
// the containing extension has been registered with a Runtime.
type Plugin struct {
	service *wago.Workers
}

// New returns a fresh workers plugin. A Plugin must not be shared by runtimes.
func New() *Plugin { return &Plugin{} }

func (*Plugin) Info() wago.ExtensionInfo {
	return wago.ExtensionInfo{
		ID:          "wago.workers",
		Name:        "Workers",
		Version:     "0.1.0",
		Description: "Bounded, extension-scoped WebAssembly worker primitives",
		Stability:   wago.Experimental,
		Repository:  "https://github.com/wago-org/wago",
		License:     "Apache-2.0",
		Tags:        []string{"workers", "concurrency", "plugin-foundation"},
		RequiresCapabilities: []wago.PluginCapability{
			wago.PluginManagedInstances,
		},
		Compat: wago.Compatibility{Engines: map[string]string{
			"wago": ">=0.1.0",
		}},
	}
}

// Register activates the plugin's worker service transactionally when
// Runtime.Use commits the extension registration.
func (p *Plugin) Register(reg *wago.Registry) error {
	var err error
	p.service, err = wago.NewWorkers(reg)
	return err
}

// Service returns the plugin-owned worker service. Before successful runtime
// registration its operational methods return wago.ErrWorkersInactive.
func (p *Plugin) Service() *wago.Workers {
	if p == nil {
		return nil
	}
	return p.service
}

func init() {
	wago.RegisterExtension(PluginName, func() wago.Extension { return New() })
}
