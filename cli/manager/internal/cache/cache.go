// Package cache owns inspection and cleanup of regenerable Wago state.
package cache

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

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
		matches, _ := filepath.Glob(filepath.Join(dirs.Versions, "*", "*", "*", "plugins"))
		paths = append(paths, matches...)
	}
	return paths
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
	paths := Paths(dirs, selection)
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
