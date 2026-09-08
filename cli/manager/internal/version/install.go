package version

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	"github.com/wago-org/wago/internal/wagopaths"
)

func vmInstallContext(ctx context.Context, d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build, use string) {
	installVersionContext(ctx, d, ver, profile, build, true, true, use)
}

func vmInstallForSwitch(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) {
	vmInstallForSwitchContext(context.Background(), d, ver, profile, build)
}

func vmInstallForSwitchContext(ctx context.Context, d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) {
	installVersionContext(ctx, d, ver, profile, build, false, false, "no")
}

func installVersion(d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build, offer, showLocation bool, use string) {
	installVersionContext(context.Background(), d, ver, profile, build, offer, showLocation, use)
}

func installVersionContext(ctx context.Context, d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build, offer, showLocation bool, use string) {
	installName := releaseAssetVersion(ver)
	if err := validateVersionStorageName(installName); err != nil {
		fatal("version install: %v", err)
	}
	lock, err := versionMutationLock(ctx, d, installName)
	if err != nil {
		fatal("version install: %v", err)
	}
	defer lock.Close()
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
			_ = lock.Close()
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
	resolved, sourceOnly, err := resolveRunnerVersionContext(ctx, ver, progress)
	if err != nil {
		fatal("version install: %v", err)
	}
	if err := installRunnerPayloadContext(ctx, resolved, profile, build, dest, sourceOnly, progress); err != nil {
		fatal("version install: %v", err)
	}
	progress.Finish("Installed " + installedWagoLabel(installName, resolved, profile, build))
	if showLocation {
		printDetail(progress.Writer(), "location", displayPath(dest))
	}
	_ = lock.Close()
	if offer {
		finishVersionInstall(d, installName, profile, build, use)
	}
}

func vmInstallRequestedContext(ctx context.Context, d wagopaths.Dirs, args []string, latest, nightly, canary bool, profileValue, buildValue, use string) {
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
		vmBrowseContext(ctx, d, profileValue, buildValue, use)
		return
	}
	profile, build, ok := chooseInstallVariant(profileValue, buildValue)
	if !ok {
		return
	}
	if latest {
		release, err := latestStableReleaseContext(ctx)
		if err != nil {
			fatal("version latest: %v", err)
		}
		vmInstallContext(ctx, d, release, profile, build, use)
		return
	}
	if nightly {
		vmInstallContext(ctx, d, "nightly", profile, build, use)
		return
	}
	if canary {
		vmInstallContext(ctx, d, "canary", profile, build, use)
		return
	}
	vmInstallContext(ctx, d, args[0], profile, build, use)
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
