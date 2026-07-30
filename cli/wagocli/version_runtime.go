//go:build !wago_manager

package wagocli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/wagopaths"
)

func printVersion() {
	executable := runnerExecutablePath()
	active := activeInstallationForExecutable(executable)

	fmt.Printf("%s\n", bold("Wago"))
	printVersionDetail("channel", diagnosticChannel(active.version, versionString()))
	printVersionDetail("release", versionString())
	printVersionDetail("profile", runnerProfile())
	if active.build != "" {
		printVersionDetail("build", string(active.build))
	}
	printVersionDetail("platform", runtime.GOOS+"/"+runtime.GOARCH)
	printVersionDetail("toolchain", runtime.Compiler+" "+runtime.Version())
	if managerVersion := os.Getenv("WAGO_MANAGER_VERSION"); managerVersion != "" {
		manager := managerVersion
		if path := os.Getenv("WAGO_MANAGER_EXECUTABLE"); path != "" {
			manager += "  " + path
		}
		printVersionDetail("manager", manager)
	}
	printVersionDetail("runtime", executable)
	printVersionDetail("plugins", versionPluginSummary())
	printVersionDetail("features", fmt.Sprint(wago.SupportedFeatures()))
	guardPages := "unavailable"
	if wago.GuardPageSupported() {
		guardPages = "available"
	}
	printVersionDetail("guard pages", guardPages)
}

type diagnosticInstallation struct {
	version string
	profile wagopaths.Profile
	build   wagopaths.Build
}

func activeInstallationForExecutable(executable string) diagnosticInstallation {
	dirs := wagopaths.DirsFor(versionString())
	path, version, profile, build, ok := activeRunner(dirs)
	if !ok || !sameExecutable(path, executable) {
		return diagnosticInstallation{}
	}
	return diagnosticInstallation{version: version, profile: profile, build: build}
}

func sameExecutable(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	if leftErr == nil && rightErr == nil {
		return os.SameFile(leftInfo, rightInfo)
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func runnerExecutablePath() string {
	path, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func compiledPluginSummary() string {
	names := wago.RegisteredPluginNames()
	if len(names) == 0 {
		return "none"
	}
	plugins := make([]string, 0, len(names))
	for _, name := range names {
		label := name
		if extension, ok := wago.NewExtension(name); ok {
			if version := strings.TrimSpace(extension.Info().Version); version != "" {
				label += "@" + version
			}
		}
		plugins = append(plugins, label)
	}
	sort.Strings(plugins)
	return strings.Join(plugins, ", ")
}
