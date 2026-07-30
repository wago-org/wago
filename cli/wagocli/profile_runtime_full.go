//go:build !wago_manager && !wago_minimal

package wagocli

import (
	"sort"
	"strings"

	"github.com/wago-org/wago"
)

func prepareRunnerInvocation(args []string) {
	if usesProjectBuild(args) {
		maybeReexecLocal()
	}
}

func runProfileFlags() []Flag {
	return []Flag{{Name: "plugin", Arg: "<names>", Help: "comma-separated extra plugins to enable, on top of wago.json (see: wago plugin list)"}}
}

func prepareRunPlugins() { maybeReexecForPlugins() }

func loadRunRuntime(cfg *wago.RuntimeConfig, plugins string) *wago.Runtime {
	return loadPluginRuntime(cfg, plugins)
}

func versionPluginSummary() string {
	compiled := compiledPluginSummary()
	_, configured, scope := activePluginSet()
	if len(configured) == 0 {
		return compiled
	}
	sort.Strings(configured)
	summary := strings.Join(configured, ", ") + " (" + scope + ")"
	if compiled != "none" {
		summary += "; compiled: " + compiled
	}
	return summary
}
