//go:build !wago_minimal

package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/ui"
)

var linked struct {
	sync.RWMutex
	set wago.PluginSet
}

// Configure receives the explicit provider catalog and reviewed selections at
// generated-program entry. It is process input, not provider self-registration.
func Configure(set wago.PluginSet) {
	linked.Lock()
	linked.set = clonePluginSet(set)
	linked.Unlock()
}

func PluginSet() wago.PluginSet {
	linked.RLock()
	set := linked.set
	linked.RUnlock()
	// Configure publishes an owned, immutable snapshot, so the expensive deep
	// copy can happen after releasing the lock without racing a replacement.
	return clonePluginSet(set)
}

func Definitions() []wago.PluginDefinition {
	set := PluginSet()
	definitions := make([]wago.PluginDefinition, 0, len(set.Providers))
	selected := make(map[string]bool, len(set.Selections))
	for _, selection := range set.Selections {
		selected[selection.ID] = true
	}
	for _, provider := range set.Providers {
		if selected[provider.Definition.ID] {
			definitions = append(definitions, provider.Definition)
		}
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ID < definitions[j].ID })
	return definitions
}

func Definition(id string) (wago.PluginDefinition, bool) {
	for _, definition := range Definitions() {
		if definition.ID == id {
			return definition, true
		}
	}
	return wago.PluginDefinition{}, false
}

func loadPluginRuntime(cfg *wago.RuntimeConfig, guestArgs []string) *wago.Runtime {
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg), wago.WithGuestArguments(guestArgs))
	set := PluginSet()
	if len(set.Selections) != 0 {
		if err := rt.LoadPlugins(context.Background(), set); err != nil {
			ui.Fatal("plugins: %v", err)
		}
	}
	return rt
}

func Inspect() (*wago.PluginPlan, error) {
	set := PluginSet()
	if len(set.Selections) == 0 {
		return &wago.PluginPlan{}, nil
	}
	return wago.InspectPluginPlan(set)
}

func Verify() error {
	if _, err := Inspect(); err != nil {
		return fmt.Errorf("verify linked PluginSet: %w", err)
	}
	return nil
}

func clonePluginSet(set wago.PluginSet) wago.PluginSet {
	// PluginSet is copied only at process-catalog boundaries. Deep ownership is
	// required here because definitions and reviewed selections contain mutable
	// maps, slices, and RawMessages that must never alias the linked global set.
	clone := wago.PluginSet{
		Providers:  append([]wago.PluginProvider(nil), set.Providers...),
		Selections: append([]wago.PluginSelection(nil), set.Selections...),
	}
	for index, provider := range set.Providers {
		clone.Providers[index].Definition = clonePluginDefinition(provider.Definition)
	}
	for index, selection := range set.Selections {
		clone.Selections[index] = clonePluginSelection(selection)
	}
	return clone
}

func clonePluginDefinition(definition wago.PluginDefinition) wago.PluginDefinition {
	clone := definition
	clone.Compatibility.Engines = cloneStringMap(definition.Compatibility.Engines)
	clone.Compatibility.Platforms = append([]string(nil), definition.Compatibility.Platforms...)
	clone.Provenance.Authors = append([]string(nil), definition.Provenance.Authors...)
	clone.Requires = append([]wago.PluginRequirement(nil), definition.Requires...)
	clone.Authorities = append([]wago.AuthorityRequest(nil), definition.Authorities...)
	for index := range clone.Authorities {
		clone.Authorities[index].Scope.Modules = append([]string(nil), definition.Authorities[index].Scope.Modules...)
	}
	clone.ConfigSchema = append(json.RawMessage(nil), definition.ConfigSchema...)
	clone.Provides = append([]wago.ContractSpec(nil), definition.Provides...)
	clone.Consumes = append([]wago.ContractRequirement(nil), definition.Consumes...)
	return clone
}

func clonePluginSelection(selection wago.PluginSelection) wago.PluginSelection {
	clone := selection
	clone.Dependencies = cloneStringMap(selection.Dependencies)
	clone.Grants = append([]wago.AuthorityGrant(nil), selection.Grants...)
	for index := range clone.Grants {
		clone.Grants[index].Scope.Modules = append([]string(nil), selection.Grants[index].Scope.Modules...)
	}
	clone.Contracts = append([]wago.ContractBinding(nil), selection.Contracts...)
	for index := range clone.Contracts {
		clone.Contracts[index].Providers = append([]string(nil), selection.Contracts[index].Providers...)
	}
	clone.Config = append(json.RawMessage(nil), selection.Config...)
	return clone
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	clone := make(map[string]string, len(input))
	for key, value := range input {
		clone[key] = value
	}
	return clone
}
