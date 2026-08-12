//go:build !wago_minimal

package plugin

import (
	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
)

func Flags() []command.Flag {
	return []command.Flag{
		{Name: "local", Bool: true, Help: "use this project's plugins (default when wago.json exists)"},
		{Name: "global", Short: "g", Bool: true, Help: "use the shared user-wide plugins"},
		{Name: "bare", Bool: true, Help: "run without local or global plugins"},
	}
}

func LoadRuntime(cfg *wago.RuntimeConfig, guestArgs []string) *wago.Runtime {
	return loadPluginRuntime(cfg, guestArgs)
}

func Summary() string {
	return compiledPluginSummary()
}
