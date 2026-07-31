package runtime

import (
	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	runtimeplugin "github.com/wago-org/wago/cli/runtime/internal/plugin"
)

// commandEnvironment adapts the compiled runtime profile to command packages.
// Command parsing and execution flow remain in each commands/.../command.go.
type commandEnvironment struct{}

func (commandEnvironment) ProfileFlags() []command.Flag {
	return runtimeplugin.Flags()
}

func (commandEnvironment) LoadRuntime(config *wago.RuntimeConfig, plugins string) *wago.Runtime {
	return runtimeplugin.LoadRuntime(config, plugins)
}

func (commandEnvironment) ScopeLabel() string {
	return runtimeplugin.ScopeLabel()
}
