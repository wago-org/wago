package plugin

import (
	pluginbuild "github.com/wago-org/wago/cli/manager/internal/plugin/build"
	managerversion "github.com/wago-org/wago/cli/manager/internal/version"
	"github.com/wago-org/wago/internal/wagopaths"
)

// Plugin builds are always standard runtimes. The manager may orchestrate them
// regardless of which runtime profile is currently selected.
func runnerProfile() string  { return "standard" }
func runnerBuildTag() string { return "wago_runtime" }

func pluginRuntimeVersion() string {
	dirs := wagopaths.DirsFor(managerVersion())
	if active := managerversion.ActiveVersion(dirs); active != "" {
		return active
	}
	return managerVersion()
}

func pluginBuildDefaultVariant() string {
	dirs := wagopaths.DirsFor(managerVersion())
	return string(managerversion.ActiveBuild(dirs))
}

func pluginBuildConfig() pluginbuild.Config {
	return pluginbuild.Config{
		RuntimeVersion: pluginRuntimeVersion(),
		Profile:        runnerProfile(),
		BuildTag:       runnerBuildTag(),
	}
}
