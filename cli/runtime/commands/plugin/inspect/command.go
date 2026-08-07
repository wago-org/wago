// Package inspect implements wago plugin inspect.
package inspect

import (
	"fmt"
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
	name := c.Optional("[name]")
	if name == "" {
		if automation.NoInput() {
			ui.Usage("plugin inspect: --no-input requires [name]")
		}
		name = selectPlugin()
		if name == "" {
			return
		}
	}
	extension, ok := wago.NewExtension(name)
	if !ok {
		ui.Fatal("plugin inspect: unknown plugin %q (see: wago plugin list)", name)
	}
	result := runtimeplugin.BuildReport(name, extension)
	if automation.JSON() {
		ui.PrintJSON(result)
		return
	}
	info := result.ExtensionInfo
	header := fmt.Sprintf("%s  %s %s  %s", ui.Bold(name), ui.Dim(info.ID), info.Version, ui.Dim(string(info.Stability)))
	if info.Private {
		header += "  " + ui.Dim("· private")
	}
	fmt.Println(header)
	if info.Description != "" {
		fmt.Printf("  %s\n", info.Description)
	}
	detail := func(key, value string) {
		if value != "" {
			fmt.Printf("  %s %s\n", ui.Dim(fmt.Sprintf("%-13s", key+":")), value)
		}
	}
	detail("homepage", info.Homepage)
	detail("repository", info.Repository)
	detail("license", info.License)
	detail("authors", strings.Join(info.Authors, ", "))
	detail("tags", strings.Join(info.Tags, ", "))
	detail("compatibility", runtimeplugin.CompatibilityDetail(info.Compat))
	if len(result.Capabilities) > 0 {
		detail("capabilities", strings.Join(result.Capabilities, ", "))
	}
	if len(result.RequiresCapabilities) > 0 {
		detail("requires grants", strings.Join(result.RequiresCapabilities, ", "))
	}
	if len(info.Requires) > 0 {
		detail("requires", strings.Join(info.Requires, ", "))
	}
	if len(result.Imports) == 0 {
		return
	}
	fmt.Printf("  %s\n", ui.Dim("imports:"))
	for _, spec := range result.Imports {
		line := fmt.Sprintf("    %s  %s", ui.Cyan(spec.Module+"."+spec.Name), ui.Dim(runtimeplugin.Signature(spec.Params, spec.Results)))
		if spec.Capability != "" {
			line += "  " + ui.Dim("["+spec.Capability+"]")
		}
		fmt.Println(line)
		if spec.Docs != "" {
			fmt.Printf("        %s\n", ui.Dim(spec.Docs))
		}
	}
}

func selectPlugin() string {
	names := wago.RegisteredPluginNames()
	if len(names) == 0 {
		ui.Fatal("plugin inspect: no plugins enabled")
	}
	packages := make([]pluginmenu.Package, 0, len(names))
	for _, name := range names {
		extension, ok := wago.NewExtension(name)
		if !ok {
			continue
		}
		packages = append(packages, pluginmenu.Package{Name: name, Version: extension.Info().Version})
	}
	selected, ok := pluginmenu.Select("Select installed plugin", packages)
	if !ok {
		fmt.Println("Cancelled.")
		return ""
	}
	return selected
}
