package version

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/handoff"
	"github.com/wago-org/wago/cli/internal/ui"
)

func Print(release, defaultProfile, defaultBuild, plugins string) {
	executable := runnerExecutablePath()
	launch := handoff.FromEnvironment()
	guardPages := "unavailable"
	if wago.GuardPageSupported() {
		guardPages = "available"
	}
	if automation.JSON() {
		ui.PrintJSON(map[string]any{
			"channel": diagnosticChannel(launch.RuntimeChannel, release), "release": release,
			"profile": fallback(launch.RuntimeProfile, defaultProfile), "build": fallback(launch.RuntimeBuild, defaultBuild),
			"platform": runtime.GOOS + "/" + runtime.GOARCH, "toolchain": runtime.Compiler + " " + runtime.Version(),
			"managerVersion": launch.ManagerVersion, "managerPath": launch.ManagerExecutable,
			"runtimePath": executable, "plugins": plugins, "features": fmt.Sprint(wago.SupportedFeatures()), "guardPages": guardPages,
		})
		return
	}

	fmt.Printf("%s\n", ui.Bold("Wago"))
	printVersionDetail("channel", diagnosticChannel(launch.RuntimeChannel, release))
	printVersionDetail("release", release)
	printVersionDetail("profile", fallback(launch.RuntimeProfile, defaultProfile))
	printVersionDetail("build", fallback(launch.RuntimeBuild, defaultBuild))
	printVersionDetail("platform", runtime.GOOS+"/"+runtime.GOARCH)
	printVersionDetail("toolchain", runtime.Compiler+" "+runtime.Version())
	if launch.ManagerVersion != "" {
		manager := launch.ManagerVersion
		if launch.ManagerExecutable != "" {
			manager += "  " + launch.ManagerExecutable
		}
		printVersionDetail("manager", manager)
	}
	printVersionDetail("runtime", executable)
	printVersionDetail("plugins", plugins)
	printVersionDetail("features", fmt.Sprint(wago.SupportedFeatures()))
	printVersionDetail("guard pages", guardPages)
}

func fallback(value, defaultValue string) string {
	if value != "" {
		return value
	}
	return defaultValue
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

func printVersionDetail(label, value string) {
	ui.Detail(os.Stdout, label, value)
}
