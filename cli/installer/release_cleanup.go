package main

import (
	"fmt"
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
	protection, err := captureCleanupProtection(protected)
	if err != nil {
		return err
	}
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
		target, err := os.Stat(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if protection.isRoot(target) {
			return nil
		}
		contains := protection.isAncestor(target)
		if !contains {
			if err := protection.validate(); err != nil {
				return err
			}
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
		inside := false
		// Only initial cleanup roots can begin inside a protected tree. Recursive
		// traversal stops at that tree's root, so it need not walk ancestry again.
		for _, keep := range protected {
			contains, err := cleanupPathContains(keep, path)
			if err != nil {
				return err
			}
			inside = inside || contains
		}
		if inside {
			continue
		}
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

// Cache the configured and resolved ancestor identities once per operation.
// Compare current boundary identities to this snapshot; no repeated filesystem
// ancestry walk is needed for each child of a deep installation directory.
type cleanupProtection struct {
	paths     []string
	roots     []os.FileInfo
	ancestors []os.FileInfo
}

func captureCleanupProtection(paths []string) (cleanupProtection, error) {
	p := cleanupProtection{paths: paths, roots: make([]os.FileInfo, len(paths))}
	seen := make(map[string]bool)
	for i, path := range paths {
		root, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return p, err
		}
		p.roots[i] = root
		absolute, err := filepath.Abs(path)
		if err != nil {
			return p, err
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return p, err
		}
		for _, route := range []string{absolute, resolved} {
			for !seen[route] {
				seen[route] = true
				info, err := os.Stat(route)
				if err != nil {
					return p, err
				}
				p.ancestors = append(p.ancestors, info)
				parent := filepath.Dir(route)
				if parent == route {
					break
				}
				route = parent
			}
		}
	}
	return p, nil
}
func (p cleanupProtection) isRoot(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	for _, root := range p.roots {
		if root != nil && os.SameFile(root, info) {
			return true
		}
	}
	return false
}
func (p cleanupProtection) isAncestor(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	for _, ancestor := range p.ancestors {
		if os.SameFile(ancestor, info) {
			return true
		}
	}
	return false
}
func (p cleanupProtection) validate() error {
	for i, path := range p.paths {
		current, err := os.Stat(path)
		if p.roots[i] == nil && os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if p.roots[i] == nil || !os.SameFile(current, p.roots[i]) {
			return fmt.Errorf("protected cleanup path changed: %s", path)
		}
	}
	return nil
}
