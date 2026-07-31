// Package version owns installed runtime state, release discovery, downloads,
// source fallbacks, and version-selection user interfaces.
package version

import (
	"io"

	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	"github.com/wago-org/wago/internal/wagopaths"
)

func ActiveVersion(d wagopaths.Dirs) string            { return activeVersion(d) }
func ActiveProfile(d wagopaths.Dirs) wagopaths.Profile { return activeProfile(d) }
func ActiveBuild(d wagopaths.Dirs) wagopaths.Build     { return activeBuild(d) }

func ActiveRunner(d wagopaths.Dirs) (string, string, wagopaths.Profile, wagopaths.Build, bool) {
	return activeRunner(d)
}

func InstalledRuntime(d wagopaths.Dirs, name string, profile wagopaths.Profile, build wagopaths.Build) (string, wagopaths.Profile, wagopaths.Build, bool) {
	return installedRuntime(d, name, profile, build)
}

func InstallManagerUpdate(channel, dest string, progress *managerprogress.Progress) (string, error) {
	return installManagerUpdate(channel, dest, progress)
}

func InstallRunnerPayload(ref string, profile wagopaths.Profile, build wagopaths.Build, dest string, sourceOnly bool, progress *managerprogress.Progress) error {
	return installRunnerPayload(ref, profile, build, dest, sourceOnly, progress)
}

func SyncInstalledSource(ref, dest string, progress *managerprogress.Progress) error {
	return syncInstalledSource(ref, dest, progress)
}

func SetActiveInstallation(d wagopaths.Dirs, name string, profile wagopaths.Profile, build wagopaths.Build) error {
	return setActiveInstallation(d, name, profile, build)
}

func SetActiveVersion(d wagopaths.Dirs, name string) error {
	return setActiveVersion(d, name)
}

func PromptYesNo(in io.Reader, out io.Writer, prompt string) bool {
	return promptYesNo(in, out, prompt)
}

func DisplayRelease(ref string) string {
	return releasePickerLabel(canaryCommitVersion(ref))
}
