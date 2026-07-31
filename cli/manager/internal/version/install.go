package version

import (
	"fmt"
	"os"
	"path/filepath"

	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	"github.com/wago-org/wago/internal/wagopaths"
)

func vmInstall(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build, use string) {
	installVersion(d, ver, profile, build, true, true, use)
}

func vmInstallForSwitch(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) {
	installVersion(d, ver, profile, build, false, false, "no")
}

func installVersion(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build, offer, showLocation bool, use string) {
	installName := canaryCommitVersion(ver)
	dest := d.RuntimeBinary(installName, string(profile), string(build))
	if installedPath, _, _, installed := installedRuntime(d, installName, profile, build); installed {
		// A rolling channel (canary/nightly) re-fetches even when present — the
		// name is stable but the build behind it moves. Only an immutable release
		// short-circuits, since re-downloading identical bytes is pointless.
		if !isRollingChannel(ver) {
			fmt.Printf("%s %s is already installed\n", cyan("✓"), installedWagoLabel(installName, installName, profile, build))
			if showLocation {
				printDetail(os.Stdout, "location", displayPath(installedPath))
			}
			if offer {
				finishVersionInstall(d, installName, profile, build, use)
			}
			return
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fatal("version install: %v", err)
	}
	progress := managerprogress.NewProgress(os.Stderr)
	progress.Title("Setting Up")
	resolved, sourceOnly, err := resolveRunnerVersion(ver, progress)
	if err != nil {
		fatal("version install: %v", err)
	}
	if err := installRunnerPayload(resolved, profile, build, dest, sourceOnly, progress); err != nil {
		fatal("version install: %v", err)
	}
	progress.Finish("Installed " + installedWagoLabel(installName, canaryCommitVersion(resolved), profile, build))
	if showLocation {
		printDetail(progress.Writer(), "location", displayPath(dest))
	}
	if offer {
		finishVersionInstall(d, installName, profile, build, use)
	}
}

func vmInstallRequested(d wagopaths.Dirs, args []string, latest, nightly, canary bool, profileValue, buildValue, use string) {
	if len(args) > 1 || (len(args) == 1 && (latest || nightly || canary)) || (latest && (nightly || canary)) || (nightly && canary) {
		fatal("version install: choose one version or channel")
	}
	if _, err := requestedProfile(profileValue); err != nil {
		fatal("version install: %v", err)
	}
	if _, err := requestedBuild(buildValue); err != nil {
		fatal("version install: %v", err)
	}
	if len(args) == 0 && !latest && !nightly && !canary {
		vmBrowse(d, profileValue, buildValue, use)
		return
	}
	profile, build, ok := chooseInstallVariant(profileValue, buildValue)
	if !ok {
		return
	}
	if latest {
		vmInstall(d, latestRelease(), profile, build, use)
		return
	}
	if nightly {
		vmInstall(d, "nightly", profile, build, use)
		return
	}
	if canary {
		vmInstall(d, "canary", profile, build, use)
		return
	}
	vmInstall(d, args[0], profile, build, use)
}

func requestedProfile(value string) (wagopaths.Profile, error) {
	if value != "" {
		return wagopaths.ParseProfile(value)
	}
	return wagopaths.ProfileStandard, nil
}

func requestedBuild(value string) (wagopaths.Build, error) {
	if value != "" {
		return wagopaths.ParseBuild(value)
	}
	return wagopaths.BuildNormal, nil
}
