//go:build !windows

package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func platformDirs(home, wagoRoot string, explicitHome bool) (data, config, cache string) {
	if explicitHome || runtime.GOOS == "darwin" {
		if explicitHome {
			return filepath.Join(wagoRoot, "data"), filepath.Join(wagoRoot, "config"), filepath.Join(wagoRoot, "cache")
		}
		return wagoRoot, filepath.Join(wagoRoot, "config"), filepath.Join(wagoRoot, "cache")
	}
	return filepath.Join(envOr("XDG_DATA_HOME", filepath.Join(home, ".local", "share")), "wago"),
		filepath.Join(envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "wago"),
		filepath.Join(envOr("XDG_CACHE_HOME", filepath.Join(home, ".cache")), "wago")
}

func shellConfig(home, shellName string) (string, bool) {
	switch shellName {
	case "zsh":
		return filepath.Join(envOr("ZDOTDIR", home), ".zshrc"), true
	case "bash":
		if runtime.GOOS == "darwin" {
			profile, rc := filepath.Join(home, ".bash_profile"), filepath.Join(home, ".bashrc")
			if _, err := os.Stat(profile); err == nil {
				return profile, true
			}
			if _, err := os.Stat(rc); errors.Is(err, os.ErrNotExist) {
				return profile, true
			}
			return rc, true
		}
		return filepath.Join(home, ".bashrc"), true
	case "fish":
		return filepath.Join(envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "fish", "config.fish"), true
	case "nu":
		return filepath.Join(envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "nushell", "env.nu"), true
	default:
		return "", false
	}
}

func pathTargets(home string) []pathTarget {
	current := filepath.Base(os.Getenv("SHELL"))
	names := []string{current, "zsh", "bash", "fish", "nu"}
	seen := map[string]bool{}
	var targets []pathTarget
	for _, shellName := range names {
		if shellName == "" || seen[shellName] {
			continue
		}
		seen[shellName] = true
		if shellName != current {
			if _, err := exec.LookPath(shellName); err != nil {
				continue
			}
		}
		configFile, ok := shellConfig(home, shellName)
		if !ok {
			continue
		}
		targets = append(targets, pathTarget{
			label:       shellName,
			description: displayPath(configFile, home),
			shell:       shellName,
			configFile:  configFile,
			current:     shellName == current,
		})
	}
	return targets
}

func addPath(binDir, configFile, shellName string) (bool, error) {
	marker := "# Wago PATH: " + binDir
	if data, err := os.ReadFile(configFile); err == nil && strings.Contains(string(data), marker) {
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(configFile), 0o755); err != nil {
		return false, err
	}
	file, err := os.OpenFile(configFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	prefix := ""
	if info.Size() > 0 {
		prefix = "\n"
	}
	quoted := strings.ReplaceAll(binDir, "'", "'\\''")
	line := "export PATH='" + quoted + "':\"$PATH\""
	if shellName == "fish" {
		line = "fish_add_path --path '" + quoted + "'"
	} else if shellName == "nu" {
		line = "$env.PATH = ($env.PATH | prepend '" + quoted + "')"
	}
	_, err = fmt.Fprintf(file, "%s%s\n%s\n", prefix, marker, line)
	return false, err
}

func cleanPlatformInstall(mode, home, binDir, srcDir, dataDir, configDir, cacheDir string) error {
	if mode == "minimal" {
		return nil
	}
	paths := []string{filepath.Join(binDir, "wago"), filepath.Join(dataDir, "versions"), configDir, cacheDir, srcDir}
	if mode == "full" {
		paths = append(paths, dataDir, filepath.Join(home, ".wago"))
		if filepath.Base(dataDir) == "data" && filepath.Dir(configDir) == filepath.Dir(dataDir) && filepath.Dir(cacheDir) == filepath.Dir(dataDir) {
			paths = append(paths, filepath.Dir(dataDir))
		}
	}
	for _, path := range uniquePaths(paths) {
		if err := safeRemove(path, home); err != nil {
			return err
		}
	}
	for _, path := range []string{
		filepath.Join(envOr("ZDOTDIR", home), ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "fish", "config.fish"),
		filepath.Join(envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "nushell", "env.nu"),
	} {
		if err := removePathBlock(path, binDir); err != nil {
			return err
		}
	}
	return nil
}

func removePathBlock(path, binDir string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	marker := "# Wago PATH: " + binDir
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == marker {
			if len(lines) > 0 && lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			if scanner.Scan() {
				// The installer owns exactly the line following its marker.
			}
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func safeRemove(path, home string) error {
	path = filepath.Clean(path)
	if path == "." || path == string(os.PathSeparator) || path == filepath.Clean(home) {
		return fmt.Errorf("refusing to remove unsafe path %q", path)
	}
	return os.RemoveAll(path)
}

func uniquePaths(paths []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(path)
		if path != "." && !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}
	return result
}

func pathContains(binDir string) bool {
	for _, path := range filepath.SplitList(os.Getenv("PATH")) {
		if path == binDir {
			return true
		}
	}
	return false
}

func shellFromConfig(configFile string) string {
	switch filepath.Base(configFile) {
	case ".zshrc":
		return "zsh"
	case ".bashrc", ".bash_profile":
		return "bash"
	case "config.fish":
		return "fish"
	}
	return ""
}

func sourceCommand(configFile string) string {
	return "source " + displayPath(configFile, envOr("HOME", ""))
}
