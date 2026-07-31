// Package status reports the manager, runtime, project, and plugin state that
// determines how the next Wago invocation will run.
package status

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/cli/internal/ui"
	managerversion "github.com/wago-org/wago/cli/manager/internal/version"
	"github.com/wago-org/wago/internal/wagopaths"
)

type Report struct {
	ManagerVersion string
	ManagerPath    string
	RuntimeVersion string
	RuntimeProfile string
	RuntimeBuild   string
	RuntimePath    string
	Scope          string
	ProjectDir     string
	ManifestPath   string
	LockPath       string
	LockState      string
	Plugins        int
}

func Inspect(dirs wagopaths.Dirs, managerVersion, managerPath string) (Report, error) {
	report := Report{ManagerVersion: managerVersion, ManagerPath: managerPath, Scope: "global", LockState: "not needed"}
	if path, version, profile, build, ok := managerversion.ActiveRunner(dirs); ok {
		report.RuntimePath, report.RuntimeVersion = path, version
		report.RuntimeProfile, report.RuntimeBuild = string(profile), string(build)
	}
	scope, err := project.ResolveScope(".", dirs.Data)
	if err != nil {
		return Report{}, err
	}
	report.Scope = project.ScopeLabel(scope)
	if scope.Name == "bare" {
		return report, nil
	}
	report.ProjectDir, err = filepath.Abs(scope.ManifestDir)
	if err != nil {
		return Report{}, err
	}
	report.ManifestPath = project.Path(scope.ManifestDir)
	report.LockPath = project.LockPath(scope.ManifestDir)
	requirements, err := project.Requirements(scope.ManifestDir)
	if err != nil {
		return Report{}, err
	}
	report.Plugins = len(requirements)
	if len(requirements) == 0 {
		return report, nil
	}
	lock, err := project.ReadLock(scope.ManifestDir)
	if err != nil {
		report.LockState = "invalid"
		return report, nil
	}
	report.LockState = "up to date"
	for _, requirement := range requirements {
		if lock.Packages[requirement.ID].Version == "" {
			report.LockState = "needs update"
			break
		}
	}
	return report, nil
}

func Print(out io.Writer, report Report) {
	fmt.Fprintln(out, ui.Bold("Wago status"))
	ui.Detail(out, "manager", value(report.ManagerVersion, report.ManagerPath))
	if report.RuntimeVersion == "" {
		ui.Detail(out, "runtime", "none selected")
	} else {
		ui.Detail(out, "runtime", fmt.Sprintf("%s (%s/%s)", report.RuntimeVersion, report.RuntimeProfile, report.RuntimeBuild))
		ui.Detail(out, "location", ui.DisplayPath(report.RuntimePath))
	}
	ui.Detail(out, "scope", report.Scope)
	if report.ManifestPath != "" {
		ui.Detail(out, "directory", ui.DisplayPath(report.ProjectDir))
		ui.Detail(out, "project", ui.DisplayPath(report.ManifestPath))
		ui.Detail(out, "plugins", fmt.Sprintf("%d enabled", report.Plugins))
		ui.Detail(out, "lockfile", report.LockState)
	}
}

func value(version, path string) string {
	if path == "" {
		return version
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	return version + "  " + ui.DisplayPath(path)
}

func ExecutablePath() string {
	path, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}
