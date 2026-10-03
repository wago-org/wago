//go:build linux || darwin

package cache

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func cleanPluginBuilds(root string) (Result, error) {
	return openPluginBuildRoot(root, cleanBuildLevel)
}

func sizePluginBuilds(root string) (int64, error) {
	result, err := openPluginBuildRoot(root, sizeBuildLevel)
	return result.Bytes, err
}

func openPluginBuildRoot(root string, visit func(*os.File, int) (Result, error)) (Result, error) {
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("open versions cache without following links: %w", err)
	}
	rootDir := os.NewFile(uintptr(fd), root)
	defer rootDir.Close()
	return visit(rootDir, 0)
}

func cleanBuildLevel(dir *os.File, depth int) (Result, error) {
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return Result{}, err
	}
	var result Result
	for _, entry := range entries {
		if depth == 3 && entry.Name() != "plugins" {
			continue
		}
		if depth == 3 {
			// Ancestor links are never traversed, but the final cache leaf is
			// itself owned state. Remove a link or malformed file as an object.
			bytes, removed, removeErr := removeEntryAt(dir, entry.Name())
			// Account deletion work even if a concurrent actor removes the final
			// leaf before our retry can unlink it.
			result.Bytes += bytes
			if removeErr != nil {
				return result, removeErr
			}
			if removed {
				result.Removed++
			}
			continue
		}
		child, err := openDirAt(dir, entry.Name())
		if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			continue
		}
		if err != nil {
			return result, err
		}
		childResult, visitErr := cleanBuildLevel(child, depth+1)
		closeErr := child.Close()
		result.Removed += childResult.Removed
		result.Bytes += childResult.Bytes
		if visitErr != nil || closeErr != nil {
			return result, errors.Join(visitErr, closeErr)
		}
	}
	return result, nil
}

func sizeBuildLevel(dir *os.File, depth int) (Result, error) {
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return Result{}, err
	}
	var result Result
	for _, entry := range entries {
		if depth == 3 && entry.Name() != "plugins" {
			continue
		}
		if depth == 3 {
			var stat unix.Stat_t
			if err := unix.Fstatat(int(dir.Fd()), entry.Name(), &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				if errors.Is(err, unix.ENOENT) {
					continue
				}
				return result, err
			}
			if stat.Mode&unix.S_IFMT == unix.S_IFREG {
				result.Bytes += stat.Size
				continue
			}
			if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
				continue
			}
		}
		child, err := openDirAt(dir, entry.Name())
		if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			continue
		}
		if err != nil {
			return result, err
		}
		if depth == 3 {
			bytes, visitErr := sizeOpenDir(child)
			closeErr := child.Close()
			result.Bytes += bytes
			if visitErr != nil || closeErr != nil {
				return result, errors.Join(visitErr, closeErr)
			}
			continue
		}
		childResult, visitErr := sizeBuildLevel(child, depth+1)
		closeErr := child.Close()
		result.Bytes += childResult.Bytes
		if visitErr != nil || closeErr != nil {
			return result, errors.Join(visitErr, closeErr)
		}
	}
	return result, nil
}

func sizeOpenDir(dir *os.File) (int64, error) {
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		var stat unix.Stat_t
		if err := unix.Fstatat(int(dir.Fd()), entry.Name(), &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			if errors.Is(err, unix.ENOENT) {
				continue
			}
			return total, err
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFREG {
			total += stat.Size
			continue
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
			continue
		}
		child, err := openDirAt(dir, entry.Name())
		if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			continue
		}
		if err != nil {
			return total, err
		}
		bytes, visitErr := sizeOpenDir(child)
		closeErr := child.Close()
		total += bytes
		if visitErr != nil || closeErr != nil {
			return total, errors.Join(visitErr, closeErr)
		}
	}
	return total, nil
}

func openDirAt(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func removeOpenDir(dir *os.File) (int64, error) {
	var total int64
	for pass := 0; pass < 8; pass++ {
		if _, err := unix.Seek(int(dir.Fd()), 0, 0); err != nil {
			return total, err
		}
		entries, err := dir.ReadDir(-1)
		if err != nil {
			return total, err
		}
		if len(entries) == 0 {
			return total, nil
		}
		for _, entry := range entries {
			bytes, _, err := removeEntryAt(dir, entry.Name())
			total += bytes
			if err != nil {
				return total, err
			}
		}
	}
	return total, errors.New("plugin cache changed repeatedly during cleanup")
}

func removeEntryAt(parent *os.File, name string) (int64, bool, error) {
	// A concurrent writer can repopulate or replace a directory after its
	// contents are removed, forcing an ENOTEMPTY/ENOTDIR retry. Retain bytes
	// removed by earlier attempts so a later success reports the full cleanup.
	var total int64
	for attempt := 0; attempt < 8; attempt++ {
		var stat unix.Stat_t
		if err := unix.Fstatat(int(parent.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			if errors.Is(err, unix.ENOENT) {
				return total, false, nil
			}
			return total, false, err
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
			child, err := openDirAt(parent, name)
			if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
				continue
			}
			if err != nil {
				return total, false, err
			}
			bytes, removeErr := removeOpenDir(child)
			total += bytes
			closeErr := child.Close()
			if removeErr != nil || closeErr != nil {
				return total, false, errors.Join(removeErr, closeErr)
			}
			if err := unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR); err == nil {
				return total, true, nil
			} else if errors.Is(err, unix.ENOENT) {
				return total, true, nil
			} else if errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.ENOTEMPTY) {
				continue
			} else {
				return total, false, err
			}
		}
		if err := unix.Unlinkat(int(parent.Fd()), name, 0); err == nil || errors.Is(err, unix.ENOENT) {
			if stat.Mode&unix.S_IFMT == unix.S_IFREG {
				return total + stat.Size, true, nil
			}
			return total, true, nil
		} else if errors.Is(err, unix.EISDIR) || errors.Is(err, unix.EPERM) {
			continue
		} else {
			return total, false, err
		}
	}
	return total, false, fmt.Errorf("cache entry %q changed repeatedly during cleanup", name)
}
