package self

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/wago-org/wago/cli/internal/tui"
	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	selfreplace "github.com/wago-org/wago/cli/manager/internal/self/replace"
	managerversion "github.com/wago-org/wago/cli/manager/internal/version"
	"github.com/wago-org/wago/internal/atomicfile"
	"github.com/wago-org/wago/internal/filelock"
	"github.com/wago-org/wago/internal/managedrelease"
	"github.com/wago-org/wago/internal/wagopaths"
)

func selfUninstallModePicker() *tui.Picker {
	return tui.NewPicker("Choose uninstall mode", []tui.Item{
		{
			Label: "Full", Value: string(Full),
			Description: "Remove everything, including plugins and settings",
		},
		{
			Label: "Partial", Value: string(Partial),
			Description: "Remove Wago; keep global plugins for reinstall",
		},
		{
			Label: "Minimal", Value: string(Minimal),
			Description: "Remove the Wago command and PATH only",
		},
	})
}

func selfUninstallConfirmationPicker() *tui.Picker {
	return tui.NewPicker("Continue?", []tui.Item{
		{Label: "Yes", Value: "yes"},
		{Label: "No", Value: "no"},
	})
}

func confirmSelfUninstall(in io.Reader, out io.Writer) bool {
	return confirmSelfUninstallInteractive(in, out, tui.StdinIsTTY())
}

func confirmSelfUninstallInteractive(in io.Reader, out io.Writer, interactive bool) bool {
	if interactive {
		p := selfUninstallConfirmationPicker()
		submitted, cancelled := tui.Run(p)
		return submitted && !cancelled && p.Selected() == "yes"
	}
	return managerversion.PromptYesNo(in, out, "Continue?")
}

func requestedSelfUninstallMode(value string, yes bool) (Mode, bool) {
	if value != "" {
		mode, err := ParseMode(value)
		if err != nil {
			fatal("self uninstall: %v", err)
		}
		return mode, true
	}
	if yes || !tui.StdinIsTTY() {
		return Full, true
	}
	p := selfUninstallModePicker()
	submitted, cancelled := tui.Run(p)
	if !submitted || cancelled {
		return "", false
	}
	mode, err := ParseMode(p.Selected())
	if err != nil {
		return "", false
	}
	return mode, true
}

func selfExecutablePath() string {
	path, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return managedrelease.Launcher(resolved)
	}
	return managedrelease.Launcher(path)
}

var (
	resolveManagerUpdate  = managerversion.ResolveManagerUpdate
	installManagerPayload = managerversion.InstallManagerPayload
	syncManagerSource     = managerversion.SyncInstalledSource
	verifyManagerRelease  = func(binary string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, binary, "self", "--help").CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, output)
		}
		return nil
	}
)

func selfUpdate(current, executable string, force bool) {
	selfUpdateUsing(current, executable, force, resolveManagerUpdate, installManagerPayload, syncManagerSource)
}

func selfUpdateContext(ctx context.Context, current, executable string, force bool) {
	selfUpdateUsing(current, executable, force,
		func(channel string, progress *managerprogress.Progress) (string, bool, error) {
			return managerversion.ResolveManagerUpdateContext(ctx, channel, progress)
		},
		func(resolved, destination string, sourceOnly bool, progress *managerprogress.Progress) error {
			return managerversion.InstallManagerPayloadContext(ctx, resolved, destination, sourceOnly, progress)
		},
		func(resolved, destination string, progress *managerprogress.Progress) error {
			return managerversion.SyncInstalledSourceContext(ctx, resolved, destination, progress)
		},
	)
}

func selfUpdateUsing(
	current, executable string,
	force bool,
	resolve func(string, *managerprogress.Progress) (string, bool, error),
	install func(string, string, bool, *managerprogress.Progress) error,
	syncSource func(string, string, *managerprogress.Progress) error,
) {
	progress := managerprogress.NewProgress(os.Stderr)
	progress.Title("Updating Wago")
	channel := Channel(current)

	resolved, sourceOnly, err := resolve(channel, progress)
	if err != nil {
		fatal("self update: %v", err)
	}
	if !force && managerversion.SameRelease(current, resolved) {
		progress.Finish("Wago is already up to date (" + managerversion.DisplayRelease(resolved) + ")")
		return
	}
	launcher := managedrelease.Launcher(executable)
	release, err := managedrelease.Prepare(launcher, resolved, func(binary, source string) error {
		if err := install(resolved, binary, sourceOnly, progress); err != nil {
			return err
		}
		return syncSource(resolved, source, progress)
	}, verifyManagerRelease)
	if err != nil {
		fatal("self update: stage release: %v", err)
	}
	// This manager already supports dispatch. The existing executable becomes
	// the stable launcher; its old binary and legacy source remain rollback state.
	if err := managedrelease.Publish(release, nil, nil); err != nil {
		fatal("self update: publish release (pair retained at %s): %v", release.Directory, err)
	}
	progress.Finish("Updated Wago to " + managerversion.DisplayRelease(resolved))
	printDetail(progress.Writer(), "location", displayPath(launcher))
}

func createSelfUpdateStage(executable string) (string, error) {
	file, err := atomicfile.CreateTemp(executable)
	if err != nil {
		return "", err
	}
	staged := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(staged)
		return "", err
	}
	return staged, nil
}

func selfUninstall(
	dirs wagopaths.Dirs,
	executable string,
	mode Mode,
	yes bool,
	in io.Reader,
	out io.Writer,
) {
	executable = managedrelease.Launcher(executable)
	targets := Targets(dirs, executable, mode)
	shellConfigs, err := InstallerPathConfigs()
	if err != nil {
		fatal("self uninstall: inspect shell configuration: %v", err)
	}
	fmt.Fprintf(out, "%s %s\n", bold("Uninstall Wago"), dim("("+string(mode)+")"))
	for _, target := range targets {
		printDetail(out, "remove", displayPath(target))
	}
	for _, config := range shellConfigs {
		printDetail(out, "clean PATH", displayPath(config))
	}
	fmt.Fprintln(out, dim("Projects and their wago.json files will not be changed."))
	if !yes && !confirmSelfUninstall(in, out) {
		fmt.Fprintln(out, "Cancelled.")
		return
	}
	lockPath := managedrelease.PublicationLockPath(executable)
	lock, err := filelock.Acquire(context.Background(), lockPath)
	if err != nil {
		fatal("self uninstall: lock publication: %v", err)
	}
	defer lock.Close()
	// Publication may have completed while the confirmation was open.
	targets = append(Targets(dirs, executable, mode), managedrelease.CleanupPaths(executable)...)
	installationDir := ""
	if mode == Full {
		installationDir = filepath.Dir(executable)
	}
	emptyDirs := emptyCleanupDirs(lockPath, targets, installationDir)

	for _, config := range shellConfigs {
		if err := RemoveInstallerPathBlocks(config); err != nil {
			fatal("self uninstall: clean PATH in %s: %v", displayPath(config), err)
		}
	}
	if deferred, err := selfreplace.ScheduleTargetRemoval(executable, targets, lockPath, emptyDirs); err != nil {
		fatal("self uninstall: schedule removal: %v", err)
	} else if deferred {
		fmt.Fprintln(out, cyan("✓"), "Wago cleanup will finish after the manager exits")
		return
	}
	stageTargets := targets
	if mode == Full {
		stageTargets = append(append([]string(nil), targets...), installationDir)
	}
	removalExecutable, err := selfreplace.StageRemoval(executable, stageTargets)
	if err != nil {
		fatal("self uninstall: stage manager removal: %v", err)
	}
	for _, target := range targets {
		if filepath.Clean(target) == filepath.Clean(executable) {
			continue
		}
		if err := removeManagedPathKeepingLock(target, lockPath); err != nil {
			fatal("self uninstall: remove %s: %v", displayPath(target), err)
		}
	}
	deferred, err := selfreplace.Remove(removalExecutable)
	if err != nil {
		fatal("self uninstall: remove %s: %v", displayPath(executable), err)
	}
	if err := lock.Retire(lockPath); err != nil {
		fatal("self uninstall: retire publication lock: %v", err)
	}
	// Only empty-directory removal is allowed after retirement. A fresh
	// publisher may already be preparing a new installation on a new lock.
	for _, dir := range emptyDirs {
		if err := removeEmptyInstallationDir(dir); err != nil {
			fatal("self uninstall: remove empty installation directory %s: %v", displayPath(dir), err)
		}
	}
	if deferred {
		fmt.Fprintln(out, cyan("✓"), "Wago cleanup complete; the manager will be removed after restart")
		return
	}
	fmt.Fprintf(out, "%s Uninstalled Wago (%s)\n", cyan("✓"), mode)
}
