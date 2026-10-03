//go:build windows

package cache

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func cleanPluginBuilds(root string) (Result, error) {
	return visitWindowsPluginBuilds(root, true)
}

func sizePluginBuilds(root string) (int64, error) {
	result, err := visitWindowsPluginBuilds(root, false)
	return result.Bytes, err
}

func visitWindowsPluginBuilds(root string, remove bool) (Result, error) {
	rootDir, err := openCacheObjectForVisit(root, false, remove)
	if os.IsNotExist(err) {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}
	defer rootDir.Close()
	rootName, err := finalCachePath(rootDir)
	if err != nil {
		return Result{}, err
	}
	return visitWindowsBuildLevel(rootDir, root, rootName, 0, remove)
}

func visitWindowsBuildLevel(dir *os.File, path, rootName string, depth int, remove bool) (Result, error) {
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return Result{}, err
	}
	var result Result
	for _, entry := range entries {
		if depth == 3 && entry.Name() != "plugins" {
			continue
		}
		childPath := filepath.Join(path, entry.Name())
		child, err := openCacheChild(dir, entry.Name(), childPath, remove && depth == 3, remove)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return result, err
		}
		contained, err := cacheHandleContained(child, rootName)
		if err != nil || !contained {
			_ = child.Close()
			if err != nil {
				return result, err
			}
			return result, fmt.Errorf("plugin cache escaped versions tree: %s", childPath)
		}
		info, err := cacheHandleInfo(child)
		if err != nil {
			_ = child.Close()
			return result, err
		}
		if depth == 3 {
			var bytes int64
			var visitErr error
			directory := info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
			reparse := info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
			if directory && !reparse {
				if remove {
					bytes, visitErr = removeWindowsDir(child, childPath, rootName)
					if visitErr == nil {
						visitErr = markCacheHandleForDeletion(child)
					}
				} else {
					bytes, visitErr = sizeWindowsDir(child, childPath, rootName)
				}
			} else if countedWindowsCacheFile(info) {
				bytes = int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow)
			}
			if remove && visitErr == nil && (reparse || !directory) {
				// Ancestor reparse points remain untraversed. At the final plugins
				// leaf, delete the opened link or malformed file itself.
				visitErr = markCacheHandleForDeletion(child)
				if visitErr != nil {
					// A failed handle-bound deletion did not remove a regular leaf.
					bytes = 0
				}
			}
			closeErr := child.Close()
			// Directory traversal may have deleted children before an error, so
			// retain those bytes even if the plugins leaf itself remains.
			result.Bytes += bytes
			if visitErr != nil || closeErr != nil {
				return result, errors.Join(visitErr, closeErr)
			}
			if remove {
				result.Removed++
			}
			continue
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			_ = child.Close()
			continue
		}
		childResult, visitErr := visitWindowsBuildLevel(child, childPath, rootName, depth+1, remove)
		closeErr := child.Close()
		result.Removed += childResult.Removed
		result.Bytes += childResult.Bytes
		if visitErr != nil || closeErr != nil {
			return result, errors.Join(visitErr, closeErr)
		}
	}
	return result, nil
}

func sizeWindowsDir(dir *os.File, path, rootName string) (int64, error) {
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		childPath := filepath.Join(path, entry.Name())
		child, err := openCacheChild(dir, entry.Name(), childPath, false, false)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return total, err
		}
		contained, err := cacheHandleContained(child, rootName)
		if err != nil || !contained {
			_ = child.Close()
			if err != nil {
				return total, err
			}
			return total, fmt.Errorf("plugin cache entry escaped versions tree: %s", childPath)
		}
		info, err := cacheHandleInfo(child)
		if err != nil {
			_ = child.Close()
			return total, err
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 && info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
			bytes, visitErr := sizeWindowsDir(child, childPath, rootName)
			total += bytes
			if visitErr != nil {
				_ = child.Close()
				return total, visitErr
			}
		} else if countedWindowsCacheFile(info) {
			total += int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow)
		}
		if err := child.Close(); err != nil {
			return total, err
		}
	}
	return total, nil
}

func removeWindowsDir(dir *os.File, path, rootName string) (int64, error) {
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		childPath := filepath.Join(path, entry.Name())
		child, err := openCacheChild(dir, entry.Name(), childPath, true, true)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return total, err
		}
		contained, err := cacheHandleContained(child, rootName)
		if err != nil || !contained {
			_ = child.Close()
			if err != nil {
				return total, err
			}
			return total, fmt.Errorf("plugin cache entry escaped versions tree: %s", childPath)
		}
		info, err := cacheHandleInfo(child)
		if err != nil {
			_ = child.Close()
			return total, err
		}
		var fileBytes int64
		if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 && info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
			bytes, removeErr := removeWindowsDir(child, childPath, rootName)
			total += bytes
			if removeErr != nil {
				_ = child.Close()
				return total, removeErr
			}
		} else if countedWindowsCacheFile(info) {
			fileBytes = int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow)
		}
		removeErr := markCacheHandleForDeletion(child)
		closeErr := child.Close()
		if removeErr == nil && countedWindowsCacheFile(info) {
			// Count a regular file only after its handle was marked for deletion.
			total += fileBytes
		}
		if removeErr != nil || closeErr != nil {
			return total, errors.Join(removeErr, closeErr)
		}
	}
	return total, nil
}

func countedWindowsCacheFile(info windows.ByHandleFileInformation) bool {
	// Match WalkDir and the Unix descriptor walker: link/reparse entries are
	// removable cache objects, but their target or metadata is not file bytes.
	return info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) == 0
}

func openCacheObjectForVisit(path string, remove, cleanup bool) (*os.File, error) {
	if cleanup {
		return openCacheObject(path, remove)
	}
	return openCacheObjectWithSharing(path, remove, true)
}

func openCacheObject(path string, remove bool) (*os.File, error) {
	return openCacheObjectWithSharing(path, remove, false)
}

func openCacheObjectWithSharing(path string, remove, shareDelete bool) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	access := uint32(windows.GENERIC_READ)
	if remove {
		access |= windows.DELETE
	}
	share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE)
	if shareDelete {
		share |= windows.FILE_SHARE_DELETE
	}
	// Cleanup denies direct renames of each held directory and target while
	// their handle-relative children are processed; sizing keeps delete
	// sharing because it does not publish or remove anything.
	handle, err := windows.CreateFile(name, access, share, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}

func openCacheChild(parent *os.File, name, path string, remove, cleanup bool) (*os.File, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `\/:`) {
		return nil, fmt.Errorf("invalid cache entry name %q", name)
	}
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	attributes := windows.OBJECT_ATTRIBUTES{
		RootDirectory: windows.Handle(parent.Fd()),
		ObjectName:    objectName,
	}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	// NtCreateFile does not accept generic access bits for directory opens and
	// requires SYNCHRONIZE with synchronous I/O. FILE_GENERIC_READ is the
	// concrete mask for both files and directories; Wine accepted GENERIC_READ
	// here, but native NT rejected it with STATUS_INVALID_PARAMETER.
	access := uint32(windows.FILE_GENERIC_READ)
	if remove {
		access |= windows.DELETE
	}
	share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE)
	if !cleanup {
		share |= windows.FILE_SHARE_DELETE
	}
	var handle windows.Handle
	var ioStatus windows.IO_STATUS_BLOCK
	// RootDirectory binds this lookup to the already-validated parent handle.
	// Replacing the parent's pathname can no longer redirect us to a newly
	// substituted tree between enumeration and handle-bound deletion.
	err = windows.NtCreateFile(&handle, access, &attributes, &ioStatus, nil, 0,
		share, windows.FILE_OPEN,
		windows.FILE_OPEN_REPARSE_POINT|windows.FILE_OPEN_FOR_BACKUP_INTENT|windows.FILE_SYNCHRONOUS_IO_NONALERT,
		0, 0)
	if err != nil {
		if status, ok := err.(windows.NTStatus); ok {
			err = status.Errno()
		}
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}

func cacheHandleInfo(file *os.File) (windows.ByHandleFileInformation, error) {
	var info windows.ByHandleFileInformation
	err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info)
	return info, err
}

func finalCachePath(file *os.File) (string, error) {
	buffer := make([]uint16, 512)
	for {
		length, err := windows.GetFinalPathNameByHandle(windows.Handle(file.Fd()), &buffer[0], uint32(len(buffer)), 0)
		if err == nil && length < uint32(len(buffer)) {
			return strings.TrimRight(windows.UTF16ToString(buffer[:length]), `\`), nil
		}
		if length == 0 || length > 32767 {
			if err != nil {
				return "", err
			}
			return "", errors.New("resolved cache path exceeds Windows path limit")
		}
		buffer = make([]uint16, length+1)
	}
}

func cacheHandleContained(file *os.File, root string) (bool, error) {
	path, err := finalCachePath(file)
	if err != nil {
		return false, err
	}
	return canonicalCachePathContained(path, root), nil
}

func canonicalCachePathContained(path, root string) bool {
	// Final handle paths use the filesystem's canonical spelling. Compare that
	// spelling exactly so case-sensitive siblings cannot alias the cache root.
	return path == root || len(path) > len(root) && path[len(root)] == '\\' && strings.HasPrefix(path, root)
}

func markCacheHandleForDeletion(file *os.File) error {
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS | windows.FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE)
	err := windows.SetFileInformationByHandle(windows.Handle(file.Fd()), windows.FileDispositionInfoEx,
		(*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)))
	if err == nil {
		return nil
	}
	deleteOnClose := byte(1)
	return windows.SetFileInformationByHandle(windows.Handle(file.Fd()), windows.FileDispositionInfo,
		&deleteOnClose, uint32(unsafe.Sizeof(deleteOnClose)))
}
