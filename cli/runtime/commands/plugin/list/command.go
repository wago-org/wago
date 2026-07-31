// Package list implements wago plugin list.
package list

import (
	"fmt"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/handoff"
	"github.com/wago-org/wago/cli/internal/pluginmenu"
	"github.com/wago-org/wago/cli/internal/ui"
	runtimeplugin "github.com/wago-org/wago/cli/runtime/internal/plugin"
)

type Environment interface {
	ScopeLabel() string
}

func Command(environment Environment) *command.Cmd {
	implementation := implementation{environment: environment}
	cmd := handoff.PluginListCommand()
	cmd.Run = implementation.Run
	return cmd
}

type implementation struct {
	environment Environment
}

func (cmd implementation) Run(c *command.Ctx) {
	names := wago.RegisteredPluginNames()
	if c.Bool("json") {
		reports := make([]runtimeplugin.Report, 0, len(names))
		for _, name := range names {
			if extension, ok := wago.NewExtension(name); ok {
				reports = append(reports, runtimeplugin.BuildReport(name, extension))
			}
		}
		ui.PrintJSON(reports)
		return
	}
	scope := cmd.environment.ScopeLabel()
	if len(names) == 0 {
		fmt.Printf("%s\n", ui.Dim("no plugins enabled ("+scope+")"))
		return
	}
	items := make([]item, 0, len(names))
	for _, name := range names {
		extension, ok := wago.NewExtension(name)
		if !ok {
			continue
		}
		items = append(items, item{name: name, version: extension.Info().Version})
	}
	fmt.Println(strings.Join(lines(scope, items), "\n"))
}

type item struct {
	name    string
	version string
}

func lines(scope string, items []item) []string {
	result := []string{ui.Bold("Installed plugins (" + scope + ")"), ""}
	groups := make(map[string][]item)
	var roots []string
	for _, current := range items {
		root := packageRoot(current.name)
		if _, ok := groups[root]; !ok {
			roots = append(roots, root)
		}
		groups[root] = append(groups[root], current)
	}
	for _, root := range roots {
		children := groups[root]
		version := children[0].version
		for _, child := range children {
			if child.name == root {
				version = child.version
				break
			}
		}
		result = append(result, entry(root, version))
		for _, child := range children {
			if child.name != root {
				result = append(result, " "+ui.Dim("-")+" "+entry(childName(root, child.name), child.version))
			}
		}
	}
	return result
}

func entry(name, version string) string {
	if version == "" {
		return ui.Cyan(name)
	}
	return ui.Cyan(name) + ui.Dim("@"+version)
}

func packageRoot(name string) string {
	return pluginmenu.Root(name)
}

func childName(root, name string) string {
	return pluginmenu.ChildLabel(root, name)
}
