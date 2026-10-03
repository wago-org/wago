// Package cache owns inspection and cleanup of regenerable Wago state.
package cache

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/internal/wagopaths"
)

type Selection struct {
	Downloads bool
	Builds    bool
}

type Result struct {
	Removed int
	Bytes   int64
}

func DownloadDir(dirs wagopaths.Dirs) string { return filepath.Dir(dirs.Cache) }
func LocalBuildDir() string                  { return filepath.Join(".wago", "builds") }

func Paths(dirs wagopaths.Dirs, selection Selection) []string {
	var paths []string
	if selection.Downloads {
		paths = append(paths, DownloadDir(dirs))
	}
	if selection.Builds {
		paths = append(paths, LocalBuildDir())
	}
	return paths
}

func Measure(dirs wagopaths.Dirs, selection Selection) (int64, error) {
	total, err := Size(Paths(dirs, selection))
	if err != nil || !selection.Builds {
		return total, err
	}
	// Global build sizing uses the same handle-contained traversal as cleanup;
	// returning discovered path strings would reintroduce a validation/use race.
	plugins, err := sizePluginBuilds(dirs.Versions)
	return total + plugins, err
}

func Size(paths []string) (int64, error) {
	var total int64
	for _, root := range paths {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.Type().IsRegular() {
				info, err := entry.Info()
				if err != nil {
					return err
				}
				total += info.Size()
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return 0, err
		}
	}
	return total, nil
}

func Clean(dirs wagopaths.Dirs, selection Selection) (Result, error) {
	var paths []string
	if selection.Downloads {
		paths = append(paths, DownloadDir(dirs))
	}
	if selection.Builds {
		paths = append(paths, LocalBuildDir())
	}
	bytes, err := Size(paths)
	if err != nil {
		return Result{}, err
	}
	result := Result{Bytes: bytes}
	for _, path := range paths {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return result, err
		}
		if err := os.RemoveAll(path); err != nil {
			return result, err
		}
		result.Removed++
	}
	if selection.Builds {
		// Plugin publication holds the global project mutation lock while it
		// parks the prior build for rollback. Join that lock before opening the
		// versions tree so cleanup cannot erase a parked backup through an
		// already-open directory handle. WithMutation also recovers any
		// interrupted metadata journal before deleting derived build output.
		dataDir := dirs.Data
		if dataDir == "" {
			dataDir = filepath.Dir(dirs.Versions)
		}
		var plugins Result
		err := project.WithMutation(context.Background(), dataDir, func(*project.Mutation) error {
			var cleanErr error
			// Handle-relative traversal still protects against unrelated path
			// swaps outside the cooperating plugin publication protocol.
			plugins, cleanErr = cleanPluginBuilds(dirs.Versions)
			return cleanErr
		})
		// A later cleanup failure must not hide files already removed by the
		// handle-contained walker.
		result.Bytes += plugins.Bytes
		result.Removed += plugins.Removed
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func Prune(dirs wagopaths.Dirs, olderThan time.Duration) (Result, error) {
	cutoff := time.Now().Add(-olderThan)
	installed := installedNames(dirs.Versions)
	var candidates []string
	artifactRoot := filepath.Join(dirs.Cache, "modules")
	err := filepath.WalkDir(artifactRoot, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".wago") {
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if info.ModTime().Before(cutoff) {
				candidates = append(candidates, path)
			}
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return Result{}, err
	}
	entries, err := os.ReadDir(DownloadDir(dirs))
	if err != nil && !os.IsNotExist(err) {
		return Result{}, err
	}
	for _, entry := range entries {
		if entry.Name() == dirs.Version || installed[entry.Name()] {
			continue
		}
		if oldEntry(DownloadDir(dirs), entry, cutoff) {
			candidates = append(candidates, filepath.Join(DownloadDir(dirs), entry.Name()))
		}
	}
	localEntries, _ := os.ReadDir(LocalBuildDir())
	for _, entry := range localEntries {
		if entry.Name() != dirs.Version && oldEntry(LocalBuildDir(), entry, cutoff) {
			candidates = append(candidates, filepath.Join(LocalBuildDir(), entry.Name()))
		}
	}
	versionEntries, _ := os.ReadDir(dirs.Versions)
	for _, entry := range versionEntries {
		if strings.HasPrefix(entry.Name(), ".wago-") && oldEntry(dirs.Versions, entry, cutoff) {
			candidates = append(candidates, filepath.Join(dirs.Versions, entry.Name()))
		}
	}
	bytes, err := Size(candidates)
	if err != nil {
		return Result{}, err
	}
	result := Result{Bytes: bytes}
	for _, candidate := range candidates {
		if err := os.RemoveAll(candidate); err != nil {
			return result, err
		}
		result.Removed++
	}
	return result, nil
}

func oldEntry(root string, entry fs.DirEntry, cutoff time.Time) bool {
	info, err := entry.Info()
	return err == nil && info.ModTime().Before(cutoff) && filepath.Clean(filepath.Join(root, entry.Name())) != filepath.Clean(root)
}

func installedNames(root string) map[string]bool {
	result := map[string]bool{}
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			result[entry.Name()] = true
		}
	}
	return result
}

func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value, suffix := float64(bytes), "KiB"
	for _, candidate := range []string{"KiB", "MiB", "GiB", "TiB"} {
		suffix = candidate
		value /= unit
		if value < unit {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", value, suffix)
}
