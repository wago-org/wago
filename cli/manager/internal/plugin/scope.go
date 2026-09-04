package plugin

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/internal/wagopaths"
)

// resolvePluginEnvironment is the single scope seam for plugin consumers.
// A local manifest is isolated and wins by default; otherwise every Wago
// version reads the shared global intent while compiling its own artifact.
func resolvePluginEnvironment() (pluginEnvironment, error) {
	dirs := wagopaths.DirsFor(pluginRuntimeVersion())
	manifestDir := sharedGlobalPluginDir(dirs)
	scope, err := project.ResolveScope(".", manifestDir)
	if err != nil {
		return pluginEnvironment{}, err
	}
	if scope.Name == "bare" {
		return pluginEnvironment{scope: "bare"}, nil
	}
	buildDir, err := buildDirFor(scope.Name != "local")
	if err != nil {
		return pluginEnvironment{}, err
	}
	return pluginEnvironment{
		scope: scope.Name, manifestDir: scope.ManifestDir, buildDir: buildDir, dependencies: scope.Dependencies,
	}, nil
}

func pluginBuildVariantWithDefault(fallback string) string {
	switch value := strings.ToLower(strings.TrimSpace(os.Getenv("WAGO_RUNTIME_BUILD"))); value {
	case string(wagopaths.BuildNormal), string(wagopaths.BuildTiny):
		return value
	}
	return fallback
}

func pluginBuildProfile() string {
	switch value := strings.ToLower(strings.TrimSpace(os.Getenv("WAGO_RUNTIME_PROFILE"))); value {
	case string(wagopaths.ProfileStandard):
		return value
	}
	return runnerProfile()
}

func localPluginBuildDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	version, build := pluginActiveRuntime()
	return filepath.Join(wd, ".wago", "builds", version, pluginBuildProfile(), pluginBuildVariantWithDefault(string(build))), nil
}

func globalPluginBuildDir() string {
	version, build := pluginActiveRuntime()
	dirs := wagopaths.DirsFor(version)
	return filepath.Join(dirs.Versions, version, pluginBuildProfile(), pluginBuildVariantWithDefault(string(build)), "plugins")
}

func buildDirFor(global bool) (string, error) {
	if global {
		return globalPluginBuildDir(), nil
	}
	return localPluginBuildDir()
}

func depsSource(global bool) (string, error) {
	if !global {
		return ".", nil
	}
	dirs := wagopaths.DirsFor(pluginRuntimeVersion())
	return sharedGlobalPluginDir(dirs), nil
}
