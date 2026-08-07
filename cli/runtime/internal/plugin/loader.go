//go:build !wago_minimal

package plugin

import (
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/cli/internal/ui"
)

func loadPluginRuntime(cfg *wago.RuntimeConfig, list string) *wago.Runtime {
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	manifest, err := activePluginConfigs()
	if err != nil {
		ui.Fatal("plugins: %v", err)
	}
	selected := append([]wago.PluginConfig(nil), manifest...)
	have := make(map[string]bool, len(manifest))
	for _, item := range manifest {
		have[strings.TrimPrefix(item.Name, "github.com/")] = true
	}
	for _, name := range strings.Split(list, ",") {
		id := strings.TrimPrefix(strings.TrimSpace(name), "github.com/")
		if id == "" || have[id] {
			continue
		}
		have[id] = true
		selected = append(selected, wago.PluginConfig{Name: id})
	}
	if len(selected) != 0 {
		if err := rt.LoadPlugins(selected); err != nil {
			ui.Fatal("plugins: %v", err)
		}
	}
	return rt
}

func activePluginConfigs() ([]wago.PluginConfig, error) {
	environment, err := resolvePluginEnvironment()
	if err != nil {
		return nil, err
	}
	if environment.scope == "bare" || environment.scope == "plain" {
		return nil, nil
	}
	intents, err := project.PluginIntents(environment.manifestDir)
	if err != nil {
		return nil, err
	}
	configs := make([]wago.PluginConfig, len(intents))
	for index, intent := range intents {
		capabilities := make([]wago.PluginCapability, len(intent.Capabilities))
		for capabilityIndex, capability := range intent.Capabilities {
			capabilities[capabilityIndex] = wago.PluginCapability(capability)
		}
		budgets := make(map[wago.PluginCapability]wago.CapabilityBudget, len(intent.Budgets))
		for capability, budget := range intent.Budgets {
			budgets[wago.PluginCapability(capability)] = wago.CapabilityBudget{
				MaxInstances: budget.MaxInstances, MaxMemoryBytes: budget.MaxMemoryBytes,
			}
		}
		if len(budgets) == 0 {
			budgets = nil
		}
		configs[index] = wago.PluginConfig{
			Name: intent.Name, Capabilities: capabilities, Budgets: budgets,
			Before: intent.Before, After: intent.After, Config: intent.Config,
		}
	}
	return configs, nil
}
