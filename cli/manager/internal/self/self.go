package self

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/wago-org/wago/cli/internal/tui"
	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	selfreplace "github.com/wago-org/wago/cli/manager/internal/self/replace"
	managerversion "github.com/wago-org/wago/cli/manager/internal/version"
	"github.com/wago-org/wago/internal/atomicfile"
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
		return resolved
	}
	return path
}

var (
	resolveManagerUpdate  = managerversion.ResolveManagerUpdate
	installManagerPayload = managerversion.InstallManagerPayload
	syncManagerSource     = managerversion.SyncInstalledSource
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
	staged, err := createSelfUpdateStage(executable)
	if err != nil {
		fatal("self update: prepare replacement: %v", err)
	}
	if err := install(resolved, staged, sourceOnly, progress); err != nil {
		_ = os.Remove(staged)
		fatal("self update: %v", err)
	}
	if source := managedSourceForUpdate(executable); source != "" {
		if err := syncSource(resolved, source, progress); err != nil {
			_ = os.Remove(staged)
			fatal("self update: sync plugin build source: %v", err)
		}
	}
	deferred, err := selfreplace.Executable(executable, staged)
	if err != nil {
		_ = os.Remove(staged)
		fatal("self update: %v", err)
	}
	if deferred {
		progress.Finish("Wago " + managerversion.DisplayRelease(resolved) + " will be active after restart")
		return
	}
	progress.Finish("Updated Wago to " + managerversion.DisplayRelease(resolved))
	printDetail(progress.Writer(), "location", displayPath(executable))
}

func managedSourceForUpdate(executable string) string {
	source := InstalledSourcePath()
	if source == "" {
		return ""
	}
	if os.Getenv("WAGO_SRC_DIR") != "" || pathContains(filepath.Dir(source), executable) {
		return source
	}
	return ""
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

	for _, config := range shellConfigs {
		if err := RemoveInstallerPathBlocks(config); err != nil {
			fatal("self uninstall: clean PATH in %s: %v", displayPath(config), err)
		}
	}
	if deferred, err := selfreplace.ScheduleTargetRemoval(executable, targets); err != nil {
		fatal("self uninstall: schedule removal: %v", err)
	} else if deferred {
		fmt.Fprintln(out, cyan("✓"), "Wago cleanup will finish after the manager exits")
		return
	}
	stageTargets := targets
	installationDir := ""
	if mode == Full {
		installationDir = filepath.Dir(executable)
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
		if err := RemoveManagedPath(target); err != nil {
			fatal("self uninstall: remove %s: %v", displayPath(target), err)
		}
	}
	deferred, err := selfreplace.Remove(removalExecutable)
	if err != nil {
		fatal("self uninstall: remove %s: %v", displayPath(executable), err)
	}
	if installationDir != "" {
		if err := removeEmptyInstallationDir(installationDir); err != nil {
			fatal("self uninstall: remove empty installation directory %s: %v", displayPath(installationDir), err)
		}
	}
	if deferred {
		fmt.Fprintln(out, cyan("✓"), "Wago cleanup complete; the manager will be removed after restart")
		return
	}
	fmt.Fprintf(out, "%s Uninstalled Wago (%s)\n", cyan("✓"), mode)
}
