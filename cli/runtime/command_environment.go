package runtime

import (
	"os"
	"path/filepath"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/runtime/internal/artifactcache"
	runtimeplugin "github.com/wago-org/wago/cli/runtime/internal/plugin"
	"github.com/wago-org/wago/internal/wagopaths"
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

func (commandEnvironment) ArtifactCache() artifactcache.Cache {
	executable, _ := os.Executable()
	return artifactcache.Cache{
		Dir:        filepath.Join(wagopaths.DirsFor(versionString()).Cache, "modules"),
		Executable: executable,
	}
}

func (commandEnvironment) ScopeLabel() string {
	return runtimeplugin.ScopeLabel()
}
