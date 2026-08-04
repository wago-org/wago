//go:build !wago_minimal

package plugin

import (
	"sort"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
)

func Flags() []command.Flag {
	return []command.Flag{
		{Name: "plugin", Arg: "<names>", Help: "comma-separated extra plugins to enable, on top of wago.json (see: wago plugin list)"},
		{Name: "plugins", Arg: "<names>", Help: "alias for --plugin"},
		{Name: "local", Bool: true, Help: "use this project's plugins (default when wago.json exists)"},
		{Name: "global", Short: "g", Bool: true, Help: "use the shared user-wide plugins"},
		{Name: "bare", Bool: true, Help: "run without local or global plugins"},
	}
}

func LoadRuntime(cfg *wago.RuntimeConfig, plugins string) *wago.Runtime {
	return loadPluginRuntime(cfg, plugins)
}

func Summary() string {
	compiled := compiledPluginSummary()
	environment, err := resolvePluginEnvironment()
	if err != nil || len(environment.dependencies) == 0 {
		return compiled
	}
	configured := append([]string(nil), environment.dependencies...)
	sort.Strings(configured)
	summary := strings.Join(configured, ", ") + " (" + environment.scope + ")"
	if compiled != "none" {
		summary += "; compiled: " + compiled
	}
	return summary
}
