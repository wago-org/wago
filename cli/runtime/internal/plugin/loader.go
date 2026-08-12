//go:build !wago_minimal

package plugin

import (
	"context"
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
	defer linked.RUnlock()
	return clonePluginSet(linked.set)
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
	return wago.PluginSet{
		Providers:  append([]wago.PluginProvider(nil), set.Providers...),
		Selections: append([]wago.PluginSelection(nil), set.Selections...),
	}
}
