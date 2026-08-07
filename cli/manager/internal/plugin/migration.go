package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago/cli/internal/project"
	managerversion "github.com/wago-org/wago/cli/manager/internal/version"
	"github.com/wago-org/wago/internal/wagopaths"
)

func sharedGlobalPluginDir(d wagopaths.Dirs) string {
	return d.Data
}

// migrateLegacyGlobalPlugins upgrades the old per-version global manifest to
// shared user intent. The legacy files remain intact as a recoverable fallback;
// compiled plugin binaries are deliberately not copied.
func migrateLegacyGlobalPlugins(d wagopaths.Dirs, version string) error {
	destination := sharedGlobalPluginDir(d)
	if _, err := os.Stat(filepath.Join(destination, project.File)); err == nil {
		return nil
	}

	candidates := []string{version, managerversion.ActiveVersion(d)}
	if path, active, _, _, ok := managerversion.ActiveRunner(d); ok {
		candidates = append(candidates, managerversion.RuntimeRelease(path, active))
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		source := filepath.Join(d.Versions, candidate, "plugins")
		if _, err := os.Stat(filepath.Join(source, project.File)); err != nil {
			continue
		}
		if err := os.MkdirAll(destination, 0o755); err != nil {
			return err
		}
		for _, name := range []string{project.File, "wago-lock.json"} {
			data, err := os.ReadFile(filepath.Join(source, name))
			if os.IsNotExist(err) && name != project.File {
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
