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

func pluginActiveRuntime() (string, wagopaths.Build) {
	dirs := wagopaths.DirsFor(managerVersion())
	version, _, build, err := managerversion.ActiveInstallation(dirs)
	if err != nil {
		fatal("read active installation: %v", err)
	}
	if version == "" {
		version = managerVersion()
	}
	return version, build
}

func pluginRuntimeVersion() string {
	version, _ := pluginActiveRuntime()
	return version
}

func pluginBuildDefaultVariant() string {
	_, build := pluginActiveRuntime()
	return string(build)
}

// pluginRuntimeSelection is captured once per operation. Path selection and
// compiler inputs must never read a later active-state generation.
type pluginRuntimeSelection struct {
	version, profile, build string
	dirs                    wagopaths.Dirs
}

func capturePluginRuntime() pluginRuntimeSelection {
	version, build := pluginActiveRuntime()
	return pluginRuntimeSelection{version: version, profile: pluginBuildProfile(),
		build: pluginBuildVariantWithDefault(string(build)), dirs: wagopaths.DirsFor(version)}
}

func (selection pluginRuntimeSelection) config() pluginbuild.Config {
	return pluginbuild.Config{RuntimeVersion: selection.version,
		Profile: selection.profile, BuildTag: runnerBuildTag()}
}

func pluginBuildConfig() pluginbuild.Config { return capturePluginRuntime().config() }
