package wagocli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago/internal/wagopaths"
)

func selfCommand() *Cmd {
	return &Cmd{
		Name:    "self",
		Summary: "update or uninstall the Wago manager",
		Children: []*Cmd{
			{
				Name:    "update",
				Summary: "update the Wago manager on its current release channel",
				Run: func(*Ctx) {
					selfUpdate(versionString(), selfExecutablePath())
				},
			},
			{
				Name:    "uninstall",
				Summary: "remove Wago and all managed runtimes",
				Flags:   []Flag{{Name: "yes", Short: "y", Bool: true, Help: "skip the confirmation prompt"}},
				Run: func(c *Ctx) {
					selfUninstall(wagopaths.DirsFor(versionString()), selfExecutablePath(), c.Bool("yes"), os.Stdin, os.Stdout)
				},
			},
		},
	}
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

func selfUpdateChannel(current string) string {
	if channel := channelRelease(current); channel != "" {
		return channel
	}
	if isRollingChannel(current) {
		return current
	}
	if strings.HasPrefix(current, "v") {
		return "latest"
	}
	return "canary"
}

func selfUpdate(current, executable string) {
	progress := newInstallProgress(os.Stderr)
	progress.title("Updating Wago")
	staged := executable + ".new"
	_ = os.Remove(staged)
	resolved, err := installManagerUpdate(selfUpdateChannel(current), staged, progress)
	if err != nil {
		_ = os.Remove(staged)
		fatal("self update: %v", err)
	}
	deferred, err := replaceSelfExecutable(executable, staged)
	if err != nil {
		_ = os.Remove(staged)
		fatal("self update: %v", err)
	}
	if deferred {
		progress.finish("Wago " + releasePickerLabel(resolved) + " will be active after restart")
		return
	}
	progress.finish("Updated Wago to " + releasePickerLabel(resolved))
	printDetail(progress.out, "location", displayPath(executable))
}

func selfUninstall(dirs wagopaths.Dirs, executable string, yes bool, in io.Reader, out io.Writer) {
	targets := selfUninstallTargets(dirs, executable)
	fmt.Fprintln(out, bold("Uninstall Wago"))
	for _, target := range targets {
		printDetail(out, "remove", displayPath(target))
	}
	fmt.Fprintln(out, dim("Projects and their wago.json files will not be changed."))
	if !yes && !confirmNoDefault(in, out, "Continue?") {
		fmt.Fprintln(out, "Cancelled.")
		return
	}

	for _, target := range targets {
		if filepath.Clean(target) == filepath.Clean(executable) {
			continue
		}
		if err := removeManagedPath(target); err != nil {
			fatal("self uninstall: remove %s: %v", displayPath(target), err)
		}
	}
	deferred, err := removeSelfExecutable(executable)
	if err != nil {
		fatal("self uninstall: remove %s: %v", displayPath(executable), err)
	}
	if deferred {
		fmt.Fprintln(out, cyan("✓"), "Wago data removed; the manager will be removed after restart")
		return
	}
	fmt.Fprintln(out, cyan("✓"), "Uninstalled Wago")
}

func selfUninstallTargets(dirs wagopaths.Dirs, executable string) []string {
	candidates := []string{dirs.Data, dirs.Config, filepath.Dir(dirs.Cache)}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".wago", "src"))
	}
	candidates = append(candidates, executable)

	var targets []string
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if !safeManagedPath(candidate) {
			continue
		}
		covered := false
		for i := 0; i < len(targets); {
			switch {
			case pathContains(targets[i], candidate):
				covered = true
				i = len(targets)
			case pathContains(candidate, targets[i]):
				targets = append(targets[:i], targets[i+1:]...)
			default:
				i++
			}
		}
		if !covered {
			targets = append(targets, candidate)
		}
	}
	return targets
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func removeManagedPath(path string) error {
	clean := filepath.Clean(path)
	if !safeManagedPath(clean) {
		return fmt.Errorf("refusing unsafe path %q", path)
	}
	return os.RemoveAll(clean)
}

func safeManagedPath(path string) bool {
	clean := filepath.Clean(path)
	home, _ := os.UserHomeDir()
	return clean != "" &&
		clean != "." &&
		clean != filepath.VolumeName(clean)+string(filepath.Separator) &&
		(home == "" || clean != filepath.Clean(home))
}

func confirmNoDefault(in io.Reader, out io.Writer, prompt string) bool {
	fmt.Fprintf(out, "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
