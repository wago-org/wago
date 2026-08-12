// Package inspect implements side-effect-free plugin inspection.
package inspect

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/handoff"
	"github.com/wago-org/wago/cli/internal/pluginmenu"
	"github.com/wago-org/wago/cli/internal/ui"
	runtimeplugin "github.com/wago-org/wago/cli/runtime/internal/plugin"
)

func Command() *command.Cmd {
	cmd := handoff.PluginInspectCommand()
	cmd.Run = run
	return cmd
}

func run(c *command.Ctx) {
	id := c.Optional("[plugin-id]")
	if id == "" {
		if automation.NoInput() {
			ui.Usage("plugin inspect: --no-input requires [plugin-id]")
		}
		id = selectPlugin()
		if id == "" {
			return
		}
	}
	definition, ok := runtimeplugin.Definition(id)
	if !ok {
		ui.Fatal("plugin inspect: unknown plugin %q (see: wago plugin list)", id)
	}
	plan, err := runtimeplugin.Inspect()
	if err != nil {
		ui.Fatal("plugin inspect: %v", err)
	}
	entry := findPlanEntry(plan, id)
	report := runtimeplugin.BuildReport(definition, entry)
	if automation.JSON() {
		ui.PrintJSON(report)
		return
	}
	printDefinition(definition, entry)
}

func printDefinition(definition wago.PluginDefinition, entry *wago.PluginPlanEntry) {
	header := ui.Bold(definition.ID)
	if definition.Version != "" {
		header += " " + definition.Version
	}
	if definition.Stability != "" {
		header += "  " + ui.Dim(string(definition.Stability))
	}
	fmt.Println(header)
	if definition.Description != "" {
		fmt.Printf("  %s\n", definition.Description)
	}
	detail := func(key, value string) {
		if value != "" {
			fmt.Printf("  %s %s\n", ui.Dim(fmt.Sprintf("%-14s", key+":")), value)
		}
	}
	detail("repository", definition.Provenance.Repository)
	detail("license", definition.Provenance.License)
	detail("authors", strings.Join(definition.Provenance.Authors, ", "))
	if len(definition.Requires) != 0 {
		values := make([]string, len(definition.Requires))
		for index, requirement := range definition.Requires {
			values[index] = requirement.ID + " " + requirement.Version
		}
		sort.Strings(values)
		detail("requires", strings.Join(values, ", "))
	}
	if len(definition.Authorities) != 0 {
		fmt.Printf("  %s\n", ui.Dim("authorities:"))
		for _, request := range definition.Authorities {
			fmt.Printf("    %s %s — %s%s\n", ui.Cyan(string(request.Name)), ui.Dim(string(request.Mode)), request.Reason, scopeLabel(request.Scope))
		}
	}
	if entry != nil && len(entry.Contracts) != 0 {
		fmt.Printf("  %s\n", ui.Dim("contract bindings:"))
		for _, binding := range entry.Contracts {
			providers := strings.Join(binding.Providers, ", ")
			if providers == "" {
				providers = "none (optional)"
			}
			fmt.Printf("    %s@%d -> %s\n", binding.ID, binding.Major, providers)
		}
	}
}

func scopeLabel(scope wago.AuthorityScope) string {
	var values []string
	if len(scope.Modules) != 0 {
		values = append(values, "modules: "+strings.Join(scope.Modules, ", "))
	}
	if scope.MaxInstances != 0 {
		values = append(values, fmt.Sprintf("max instances: %d", scope.MaxInstances))
	}
	if scope.MaxMemoryBytes != 0 {
		values = append(values, fmt.Sprintf("max memory: %d bytes", scope.MaxMemoryBytes))
	}
	if len(values) == 0 {
		return ""
	}
	return " (" + strings.Join(values, "; ") + ")"
}

func findPlanEntry(plan *wago.PluginPlan, id string) *wago.PluginPlanEntry {
	if plan == nil {
		return nil
	}
	for index := range plan.Plugins {
		if plan.Plugins[index].ID == id {
			return &plan.Plugins[index]
		}
	}
	return nil
}

func selectPlugin() string {
	definitions := runtimeplugin.Definitions()
	if len(definitions) == 0 {
		ui.Fatal("plugin inspect: no plugins enabled")
	}
	packages := make([]pluginmenu.Package, 0, len(definitions))
	for _, definition := range definitions {
		packages = append(packages, pluginmenu.Package{Name: definition.ID, Version: definition.Version})
	}
	selected, ok := pluginmenu.Select("Select installed plugin", packages)
	if !ok {
		fmt.Println("Cancelled.")
		return ""
	}
	return selected
}
