//go:build linux || darwin

package artifactcache

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type cacheRoot struct {
	file *os.File
	path string
}

func openCacheRoot(path string) (*cacheRoot, error) {
	fd, err := openCacheDirectory(path)
	if err != nil {
		return nil, err
	}
	return &cacheRoot{file: os.NewFile(uintptr(fd), path), path: path}, nil
}

func (root *cacheRoot) close() error {
	return root.file.Close()
}

// scan uses directory handles so a concurrent symlink replacement cannot move
// traversal outside the cache root. Batches avoid sorting or retaining a whole
// directory, and the depth cap bounds simultaneously open descriptors.
func (root *cacheRoot) scan(visit func(string, string, os.FileInfo) error) error {
	return root.scanDirectory(root.file, "", 0, visit)
}

func (root *cacheRoot) scanDirectory(directory *os.File, relative string, depth int, visit func(string, string, os.FileInfo) error) error {
	if depth > maxCacheDirectoryDepth {
		return errors.New("cache directory nesting exceeds limit")
	}
	for {
		entries, readErr := directory.Readdir(128)
		for _, info := range entries {
			if info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			name := info.Name()
			relativePath := filepath.Join(relative, name)
			if info.IsDir() {
				fd, err := openCacheDirectoryAt(int(directory.Fd()), name)
				if err != nil {
					return err
				}
				child := os.NewFile(uintptr(fd), filepath.Join(root.path, relativePath))
				err = root.scanDirectory(child, relativePath, depth+1, visit)
				closeErr := child.Close()
				if err != nil {
					return err
				}
				if closeErr != nil {
					return closeErr
				}
				continue
			}
			if info.Mode().IsRegular() && info.Size() >= 0 && strings.HasSuffix(name, ".wago") {
				if err := visit(relativePath, filepath.Join(root.path, relativePath), info); err != nil {
					return err
				}
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func (root *cacheRoot) remove(relativePath string) error {
	parts, ok := cachePathParts(relativePath)
	if !ok {
		return errors.New("invalid cache entry path")
	}
	fd, err := duplicateCacheDescriptor(int(root.file.Fd()))
	if err != nil {
		return err
	}
	defer func() { _ = closeCacheDescriptor(fd) }()
	for _, name := range parts[:len(parts)-1] {
		next, err := openCacheDirectoryAt(fd, name)
		if err != nil {
			return err
		}
		if err := closeCacheDescriptor(fd); err != nil {
			closeCacheDescriptor(next)
			return err
		}
		fd = next
	}
	return unlinkCacheEntryAt(fd, parts[len(parts)-1])
}

func cachePathParts(path string) ([]string, bool) {
	if filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, false
	}
	parts := strings.Split(path, string(filepath.Separator))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, false
		}
	}
	return parts, len(parts) != 0
}
