// Package list implements wago plugin list.
package list

import (
	"fmt"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/handoff"
	"github.com/wago-org/wago/cli/internal/ui"
	runtimeplugin "github.com/wago-org/wago/cli/runtime/internal/plugin"
)

type Environment interface{ ScopeLabel() string }

func Command(environment Environment) *command.Cmd {
	implementation := implementation{environment: environment}
	cmd := handoff.PluginListCommand()
	cmd.Run = implementation.Run
	return cmd
}

type implementation struct{ environment Environment }

func (cmd implementation) Run(*command.Ctx) {
	definitions := runtimeplugin.Definitions()
	if automation.JSON() {
		plan, err := runtimeplugin.Inspect()
		if err != nil {
			ui.Fatal("plugin list: %v", err)
		}
		reports := make([]runtimeplugin.Report, 0, len(definitions))
		for index := range definitions {
			entry := planEntry(plan, definitions[index].ID)
			reports = append(reports, runtimeplugin.BuildReport(definitions[index], entry))
		}
		ui.PrintJSON(reports)
		return
	}
	scope := cmd.environment.ScopeLabel()
	if len(definitions) == 0 {
		fmt.Printf("%s\n", ui.Dim("no plugins enabled ("+scope+")"))
		return
	}
	lines := []string{ui.Bold("Installed plugins (" + scope + ")"), ""}
	for _, definition := range definitions {
		label := ui.Cyan(definition.ID)
		if definition.Version != "" {
			label += ui.Dim("@" + definition.Version)
		}
		lines = append(lines, label)
	}
	fmt.Println(strings.Join(lines, "\n"))
}

func planEntry(plan *wago.PluginPlan, id string) *wago.PluginPlanEntry {
	for index := range plan.Plugins {
		if plan.Plugins[index].ID == id {
			return &plan.Plugins[index]
		}
	}
	return nil
}
