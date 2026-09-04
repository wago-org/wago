package self

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago/internal/managedrelease"
	"github.com/wago-org/wago/internal/wagopaths"
)

type Mode string

const (
	Full    Mode = "full"
	Partial Mode = "partial"
	Minimal Mode = "minimal"
)

func ParseMode(value string) (Mode, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(value)))
	switch mode {
	case Full, Partial, Minimal:
		return mode, nil
	default:
		return "", fmt.Errorf("unknown uninstall mode %q (want: full, partial, or minimal)", value)
	}
}

func Targets(dirs wagopaths.Dirs, executable string, mode Mode) []string {
	executable = managedrelease.Launcher(executable)
	candidates := managedrelease.RemovalTargets(executable)
	switch mode {
	case Full:
		if root := selectedWagoRoot(dirs, executable); root != "" {
			candidates = append(candidates, root)
		} else {
			// Linux's default XDG layout has no single Wago root.
			candidates = append(candidates, dirs.Data, dirs.Config, filepath.Dir(dirs.Cache))
		}
		candidates = append(candidates, InstalledSourcePath())
	case Partial:
		candidates = append(candidates, dirs.Versions, dirs.Config, filepath.Dir(dirs.Cache), InstalledSourcePath())
	}
	if completion := fishCompletionPath(); completion != "" {
		if _, err := os.Stat(completion); err == nil {
			candidates = append(candidates, completion)
		}
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

func selectedWagoRoot(dirs wagopaths.Dirs, executable string) string {
	if root := strings.TrimSpace(os.Getenv("WAGO_HOME")); root != "" {
		return root
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	root := filepath.Join(home, ".wago")
	if pathContains(root, executable) || pathContains(root, dirs.Data) {
		return root
	}
	if _, err := os.Stat(root); err == nil {
		return root
	}
	return ""
}

func InstalledSourcePath() string {
	if source := managedrelease.Source(); source != "" {
		return source
	}
	source := os.Getenv("WAGO_SRC_DIR")
	if source != "" {
		if _, err := os.Stat(source); err == nil {
			return source
		}
	} else if home, err := os.UserHomeDir(); err == nil && home != "" {
		source = filepath.Join(home, ".wago", "src")
		if _, err := os.Stat(source); err == nil {
			return source
		}
	}
	return ""
}

func InstallerPathConfigs() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, err
	}
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" {
		xdgConfig = filepath.Join(home, ".config")
	}
	zdotdir := os.Getenv("ZDOTDIR")
	if zdotdir == "" {
		zdotdir = home
	}
	candidates := []string{
		filepath.Join(zdotdir, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(xdgConfig, "fish", "config.fish"),
		filepath.Join(xdgConfig, "nushell", "env.nu"),
	}
	var configs []string
	seen := make(map[string]bool)
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		data, err := os.ReadFile(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if hasInstallerShellBlock(data) {
			configs = append(configs, candidate)
		}
	}
	return configs, nil
}

func RemoveInstallerPathBlocks(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(data), "\n")
	filtered := make([]string, 0, len(lines))
	changed := false
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSuffix(strings.TrimSuffix(lines[i], "\n"), "\r")
		if strings.HasPrefix(line, "# Wago PATH: ") || line == "# Wago completions" {
			changed = true
			if len(filtered) > 0 && strings.TrimSpace(filtered[len(filtered)-1]) == "" {
				filtered = filtered[:len(filtered)-1]
			}
			if i+1 < len(lines) && (isInstallerPathCommand(lines[i+1]) || isCompletionCommand(lines[i+1])) {
				i++
			}
			continue
		}
		filtered = append(filtered, lines[i])
	}
	if !changed {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(filtered, "")), info.Mode().Perm())
}

func RemoveManagedPath(path string) error {
	clean := filepath.Clean(path)
	if !safeManagedPath(clean) {
		return fmt.Errorf("refusing unsafe path %q", path)
	}
	return os.RemoveAll(clean)
}

// Keep the coordinator and its parent directories linked until all destructive
// work finishes. Retiring it earlier would let a new publisher bypass the lock.
func removeManagedPathKeepingLock(path, lockPath string) error {
	clean := filepath.Clean(path)
	if !safeManagedPath(clean) {
		return fmt.Errorf("refusing unsafe path %q", path)
	}
	if pathContains(clean, lockPath) && pathContains(lockPath, clean) {
		return nil
	}
	if !pathContains(clean, lockPath) {
		return os.RemoveAll(clean)
	}
	entries, err := os.ReadDir(clean)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := removeManagedPathKeepingLock(filepath.Join(clean, entry.Name()), lockPath); err != nil {
			return err
		}
	}
	return nil
}

func emptyCleanupDirs(lockPath string, targets []string, installationDir string) []string {
	var dirs []string
	for dir := filepath.Dir(lockPath); ; dir = filepath.Dir(dir) {
		covered := dir == installationDir
		for _, target := range targets {
			covered = covered || pathContains(target, dir)
		}
		if !covered || !safeManagedPath(dir) {
			break
		}
		dirs = append(dirs, dir)
	}
	return dirs
}

func removeEmptyInstallationDir(path string) error {
	clean := filepath.Clean(path)
	if !safeManagedPath(clean) {
		return fmt.Errorf("refusing unsafe path %q", path)
	}
	entries, err := os.ReadDir(clean)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || len(entries) != 0 {
		return err
	}
	if err := os.Remove(clean); err != nil && !os.IsNotExist(err) {
		// A concurrent writer owns the directory now; preserve its contents.
		if entries, readErr := os.ReadDir(clean); readErr == nil && len(entries) != 0 {
			return nil
		}
		return err
	}
	return nil
}

func hasInstallerShellBlock(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "# Wago PATH: ") || line == "# Wago completions" {
			return true
		}
	}
	return false
}

func isCompletionCommand(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, ". '") && strings.HasSuffix(line, "'") && strings.Contains(line, "/.wago/completions/wago.")
}

func fishCompletionPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		root = filepath.Join(home, ".config")
	}
	return filepath.Join(root, "fish", "completions", "wago.fish")
}

func isInstallerPathCommand(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "export PATH=") ||
		strings.HasPrefix(line, "fish_add_path --path ") ||
		strings.HasPrefix(line, "$env.PATH = ($env.PATH | prepend ")
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func safeManagedPath(path string) bool {
	clean := filepath.Clean(path)
	home, _ := os.UserHomeDir()
	return clean != "" &&
		clean != "." &&
		clean != filepath.VolumeName(clean)+string(filepath.Separator) &&
		(home == "" || clean != filepath.Clean(home))
}
