package main

import (
	"os"
	"path/filepath"
)

// Reinstall cleanup runs after paired publication and preserves the launcher,
// immutable releases, and legacy source still used by running old managers.
func (i *installer) cleanReinstallData(mode string) error {
	paths := []string{filepath.Join(i.dataDir, "versions"), i.configDir, i.cacheDir}
	if mode == "full" {
		paths = append(paths, i.dataDir, filepath.Join(i.home, ".wago"))
	}
	protected := []string{filepath.Clean(i.binDir), filepath.Clean(i.srcDir)}
	var remove func(string) error
	remove = func(path string) error {
		path = filepath.Clean(path)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		contains := false
		for _, keep := range protected {
			inside, err := cleanupPathContains(keep, path)
			if err != nil {
				return err
			}
			if inside {
				return nil
			}
			ancestor, err := cleanupPathContains(path, keep)
			if err != nil {
				return err
			}
			contains = contains || ancestor
		}
		if !contains {
			return safeRemove(path, i.home)
		}
		// Preserve aliases through which the configured launcher/source is
		// reached. Do not traverse a directory link during cleanup.
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		entries, err := os.ReadDir(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := remove(filepath.Join(path, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	for _, path := range uniquePaths(paths) {
		if err := remove(path); err != nil {
			return err
		}
	}
	return nil
}

// Compare existing directories by identity along both configured and resolved
// ancestry. Keep the configured route too: an alias inside the cleanup root can
// lead to protected data outside it. SameFile handles Windows casing and volumes.
// Missing protected directories contain no data that cleanup can remove.
func cleanupPathContains(root, path string) (bool, error) {
	rootInfo, err := os.Stat(root)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false, err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	paths := []string{path}
	if resolved != path {
		paths = append(paths, resolved)
	}
	for _, path := range paths {
		for {
			info, err := os.Stat(path)
			if err != nil {
				return false, err
			}
			if os.SameFile(rootInfo, info) {
				return true, nil
			}
			parent := filepath.Dir(path)
			if parent == path {
				break
			}
			path = parent
		}
	}
	return false, nil
}
