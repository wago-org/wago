package wagocli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago/internal/wagopaths"
)

const versionPluginManifest = "wago.json"

var runnerVersionOutput = func(path string) ([]byte, error) {
	return exec.Command(path, "--version").Output()
}

func sharedGlobalPluginDir(d wagopaths.Dirs) string {
	return d.Data
}

// migrateLegacyGlobalPlugins upgrades the old per-version global manifest to
// shared user intent. The legacy files remain intact as a recoverable fallback;
// compiled plugin binaries are deliberately not copied.
func migrateLegacyGlobalPlugins(d wagopaths.Dirs, version string) error {
	destination := sharedGlobalPluginDir(d)
	if _, err := os.Stat(filepath.Join(destination, versionPluginManifest)); err == nil {
		return nil
	}

	candidates := []string{version, activeVersion(d)}
	if path, active, _, _, ok := activeRunner(d); ok {
		candidates = append(candidates, runnerRelease(path, active))
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		source := filepath.Join(d.Versions, candidate, "plugins")
		if _, err := os.Stat(filepath.Join(source, versionPluginManifest)); err != nil {
			continue
		}
		if err := os.MkdirAll(destination, 0o755); err != nil {
			return err
		}
		for _, name := range []string{versionPluginManifest, "wago-lock.json"} {
			data, err := os.ReadFile(filepath.Join(source, name))
			if os.IsNotExist(err) && name != versionPluginManifest {
				continue
			}
			if err != nil {
				return fmt.Errorf("migrate global %s: %w", name, err)
			}
			if err := os.WriteFile(filepath.Join(destination, name), data, 0o644); err != nil {
				return fmt.Errorf("migrate global %s: %w", name, err)
			}
		}
		return nil
	}
	return nil
}

func migrateActiveGlobalPlugins(d wagopaths.Dirs) {
	if err := migrateLegacyGlobalPlugins(d, activeVersion(d)); err != nil {
		fmt.Fprintf(os.Stderr, "%s could not migrate global plugins: %v\n", dim("wago:"), err)
	}
}

func runnerRelease(path, fallback string) string {
	if current, err := os.Executable(); err == nil {
		currentInfo, currentErr := os.Stat(current)
		pathInfo, pathErr := os.Stat(path)
		if currentErr == nil && pathErr == nil && os.SameFile(currentInfo, pathInfo) {
			return fallback
		}
	}
	output, err := runnerVersionOutput(path)
	if err != nil {
		return fallback
	}
	return releaseFromVersionOutput(output, fallback)
}

func releaseFromVersionOutput(output []byte, fallback string) string {
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "release") {
			return fields[1]
		}
	}
	if len(lines) != 0 {
		fields := strings.Fields(lines[0])
		if len(fields) >= 2 && strings.EqualFold(fields[0], "wago") {
			return fields[1]
		}
	}
	return fallback
}
