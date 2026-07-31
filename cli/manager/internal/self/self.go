package self

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	selfreplace "github.com/wago-org/wago/cli/manager/internal/self/replace"
	"github.com/wago-org/wago/cli/manager/internal/tui"
	managerversion "github.com/wago-org/wago/cli/manager/internal/version"
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
	installSelfRuntime = managerversion.InstallRunnerPayload
	syncSelfSource     = managerversion.SyncInstalledSource
)

func updateActiveRuntimeForSelf(dirs wagopaths.Dirs, channel, resolved string, progress *managerprogress.Progress) (bool, error) {
	target := RuntimeTarget(managerversion.ActiveVersion(dirs), channel, resolved)
	if target == "" {
		return false, nil
	}
	profile, build := managerversion.ActiveProfile(dirs), managerversion.ActiveBuild(dirs)
	dest := dirs.RuntimeBinary(target, string(profile), string(build))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return false, fmt.Errorf("prepare runtime: %w", err)
	}
	if err := installSelfRuntime(resolved, profile, build, dest, false, progress); err != nil {
		return false, fmt.Errorf("update runtime: %w", err)
	}
	source, err := selfManagedSourcePath()
	if err != nil {
		return false, fmt.Errorf("locate plugin build source: %w", err)
	}
	if err := syncSelfSource(resolved, source, progress); err != nil {
		return false, fmt.Errorf("update plugin build source: %w", err)
	}
	if err := managerversion.SetActiveInstallation(dirs, target, profile, build); err != nil {
		return false, fmt.Errorf("select updated runtime: %w", err)
	}
	return true, nil
}

func selfUpdate(current, executable string) {
	progress := managerprogress.NewProgress(os.Stderr)
	progress.Title("Updating Wago")
	staged := executable + ".new"
	_ = os.Remove(staged)
	channel := Channel(current)
	dirs := wagopaths.DirsFor(current)

	resolved, err := managerversion.InstallManagerUpdate(channel, staged, progress)
	if err != nil {
		_ = os.Remove(staged)
		fatal("self update: %v", err)
	}
	if _, err := updateActiveRuntimeForSelf(dirs, channel, resolved, progress); err != nil {
		_ = os.Remove(staged)
		fatal("self update: %v", err)
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

func selfManagedSourcePath() (string, error) {
	if source := os.Getenv("WAGO_SRC_DIR"); source != "" {
		return source, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", fmt.Errorf("home directory is empty")
	}
	return filepath.Join(home, ".wago", "src"), nil
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
	for _, target := range targets {
		if filepath.Clean(target) == filepath.Clean(executable) {
			continue
		}
		if err := RemoveManagedPath(target); err != nil {
			fatal("self uninstall: remove %s: %v", displayPath(target), err)
		}
	}
	deferred, err := selfreplace.Remove(executable)
	if err != nil {
		fatal("self uninstall: remove %s: %v", displayPath(executable), err)
	}
	if deferred {
		fmt.Fprintln(out, cyan("✓"), "Wago cleanup complete; the manager will be removed after restart")
		return
	}
	fmt.Fprintf(out, "%s Uninstalled Wago (%s)\n", cyan("✓"), mode)
}
