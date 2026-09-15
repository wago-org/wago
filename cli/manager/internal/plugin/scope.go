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
	return capturePluginRuntime().resolveEnvironment()
}

func (selection pluginRuntimeSelection) resolveEnvironment() (pluginEnvironment, error) {
	manifestDir := sharedGlobalPluginDir(selection.dirs)
	scope, err := project.ResolveScope(".", manifestDir)
	if err != nil {
		return pluginEnvironment{}, err
	}
	if scope.Name == "bare" {
		return pluginEnvironment{scope: "bare", selection: selection}, nil
	}
	buildDir, err := selection.buildDirFor(scope.Name != "local")
	if err != nil {
		return pluginEnvironment{}, err
	}
	return pluginEnvironment{
		selection: selection, scope: scope.Name, manifestDir: scope.ManifestDir, buildDir: buildDir, dependencies: scope.Dependencies,
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

func (selection pluginRuntimeSelection) buildDirFor(global bool) (string, error) {
	if global {
		return filepath.Join(selection.dirs.Versions, selection.version, selection.profile, selection.build, "plugins"), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, ".wago", "builds", selection.version, selection.profile, selection.build), nil
}

func (selection pluginRuntimeSelection) depsSource(global bool) (string, error) {
	if !global {
		return ".", nil
	}
	return sharedGlobalPluginDir(selection.dirs), nil
}

func localPluginBuildDir() (string, error)    { return capturePluginRuntime().buildDirFor(false) }
func buildDirFor(global bool) (string, error) { return capturePluginRuntime().buildDirFor(global) }
func depsSource(global bool) (string, error)  { return capturePluginRuntime().depsSource(global) }
