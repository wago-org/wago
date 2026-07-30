//go:build !wago_manager

package wagocli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wago-org/wago/internal/wagopaths"
)

type pluginEnvironment struct {
	scope        string
	manifestDir  string
	buildDir     string
	dependencies []string
}

// resolvePluginEnvironment is the single scope seam for plugin consumers.
// A local manifest is isolated and wins by default; otherwise every Wago
// version reads the shared global intent while compiling its own artifact.
func resolvePluginEnvironment() (pluginEnvironment, error) {
	if truthyEnv("WAGO_BARE") {
		return pluginEnvironment{scope: "bare"}, nil
	}
	local := truthyEnv(pluginScopeLocalEnv)
	if local || !truthyEnv(pluginScopeGlobalEnv) {
		if _, err := os.Stat(projectManifestPath(".")); local || err == nil {
			deps, readErr := projectDeps(".")
			if readErr != nil {
				return pluginEnvironment{}, readErr
			}
			buildDir, buildErr := buildDirFor(false)
			if buildErr != nil {
				return pluginEnvironment{}, buildErr
			}
			return pluginEnvironment{
				scope: "local", manifestDir: ".", buildDir: buildDir, dependencies: deps,
			}, nil
		}
	}

	dirs := wagopaths.DirsFor(versionString())
	if err := migrateLegacyGlobalPlugins(dirs, versionString()); err != nil {
		return pluginEnvironment{}, err
	}
	manifestDir := sharedGlobalPluginDir(dirs)
	deps, err := projectDeps(manifestDir)
	if err != nil {
		return pluginEnvironment{}, err
	}
	buildDir, err := buildDirFor(true)
	if err != nil {
		return pluginEnvironment{}, err
	}
	scope := "global"
	if len(deps) == 0 {
		scope = "plain"
	}
	return pluginEnvironment{
		scope: scope, manifestDir: manifestDir, buildDir: buildDir, dependencies: deps,
	}, nil
}

func pluginBuildVariant() string {
	switch value := strings.ToLower(strings.TrimSpace(os.Getenv("WAGO_RUNTIME_BUILD"))); value {
	case string(wagopaths.BuildNormal), string(wagopaths.BuildTiny):
		return value
	}
	if runtime.Compiler == "tinygo" {
		return string(wagopaths.BuildTiny)
	}
	return string(wagopaths.BuildNormal)
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
	return filepath.Join(wd, ".wago", "builds", versionString(), pluginBuildProfile(), pluginBuildVariant()), nil
}

func globalPluginBuildDir() string {
	dirs := wagopaths.DirsFor(versionString())
	return filepath.Join(dirs.Versions, versionString(), pluginBuildProfile(), pluginBuildVariant(), "plugins")
}
