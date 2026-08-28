//go:build windows

package artifactcache

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

type cacheRoot struct {
	file      *os.File
	path      string
	finalPath string
}

func openCacheRoot(path string) (*cacheRoot, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	finalPath, err := finalHandlePath(windows.Handle(file.Fd()))
	if err != nil {
		file.Close()
		return nil, err
	}
	return &cacheRoot{file: file, path: path, finalPath: finalPath}, nil
}

func (root *cacheRoot) close() error {
	return root.file.Close()
}

func (root *cacheRoot) scan(visit func(string, string, os.FileInfo) error) error {
	return root.scanDirectory(root.path, "", 0, visit)
}

func (root *cacheRoot) scanDirectory(directory, relative string, depth int, visit func(string, string, os.FileInfo) error) error {
	if depth > maxCacheDirectoryDepth {
		return errors.New("cache directory nesting exceeds limit")
	}
	opened, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer opened.Close()
	for {
		entries, readErr := opened.Readdir(128)
		for _, info := range entries {
			if info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			name := info.Name()
			relativePath := filepath.Join(relative, name)
			path := filepath.Join(root.path, relativePath)
			if info.IsDir() {
				if err := root.scanDirectory(path, relativePath, depth+1, visit); err != nil {
					return err
				}
				continue
			}
			if info.Mode().IsRegular() && info.Size() >= 0 && strings.HasSuffix(name, ".wago") {
				if err := visit(relativePath, path, info); err != nil {
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
	if filepath.IsAbs(relativePath) || filepath.Clean(relativePath) != relativePath {
		return errors.New("invalid cache entry path")
	}
	path, err := windows.UTF16PtrFromString(filepath.Join(root.path, relativePath))
	if err != nil {
		return err
	}
	const deleteAccess = 0x00010000
	handle, err := windows.CreateFile(path, deleteAccess|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	finalPath, err := finalHandlePath(handle)
	if err != nil {
		return err
	}
	rootPrefix := strings.TrimRight(root.finalPath, `\`) + `\`
	if !strings.HasPrefix(strings.ToLower(finalPath), strings.ToLower(rootPrefix)) {
		return errors.New("cache entry resolves outside cache root")
	}
	disposition := [4]byte{1}
	return windows.SetFileInformationByHandle(handle, windows.FileDispositionInfo, &disposition[0], uint32(len(disposition)))
}

func finalHandlePath(handle windows.Handle) (string, error) {
	buffer := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return "", err
		}
		if n < uint32(len(buffer)) {
			return windows.UTF16ToString(buffer[:n]), nil
		}
		buffer = make([]uint16, n+1)
	}
}
