package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// movePathUsing keeps the cheap atomic rename when both paths share a mount.
// Across mounts it copies into a destination-local staging path, then performs
// the final rename there so the installed path never exposes a partial copy.
func movePathUsing(source, target string, rename pathRenamer, crossDevice func(error) bool) error {
	err := rename(source, target)
	if err == nil || crossDevice == nil || !crossDevice(err) {
		return err
	}
	info, statErr := os.Lstat(source)
	if statErr != nil {
		return statErr
	}
	if info.IsDir() {
		return copyDirectoryThenRename(source, target, info.Mode(), rename)
	}
	if info.Mode().IsRegular() {
		return copyFileThenRename(source, target, info.Mode(), rename)
	}
	return fmt.Errorf("move %s: unsupported file mode %s", source, info.Mode())
}

func copyFileThenRename(source, target string, mode fs.FileMode, rename pathRenamer) (err error) {
	staged, err := os.CreateTemp(filepath.Dir(target), ".wago-install-")
	if err != nil {
		return err
	}
	stagedPath := staged.Name()
	defer func() {
		if staged != nil {
			_ = staged.Close()
		}
		_ = os.Remove(stagedPath)
	}()

	input, err := os.Open(source)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(staged, input)
	inputErr := input.Close()
	if copyErr != nil {
		return copyErr
	}
	if inputErr != nil {
		return inputErr
	}
	if err := staged.Sync(); err != nil {
		return err
	}
	if err := staged.Chmod(mode.Perm()); err != nil {
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	staged = nil
	if err := rename(stagedPath, target); err != nil {
		return err
	}
	return os.Remove(source)
}

func copyDirectoryThenRename(source, target string, mode fs.FileMode, rename pathRenamer) error {
	staged, err := os.MkdirTemp(filepath.Dir(target), ".wago-source-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staged)
	if err := copyDirectoryContents(source, staged); err != nil {
		return err
	}
	if err := os.Chmod(staged, mode.Perm()); err != nil {
		return err
	}
	if err := rename(staged, target); err != nil {
		return err
	}
	return os.RemoveAll(source)
}

func copyDirectoryContents(source, target string) error {
	type directoryMode struct {
		path string
		mode fs.FileMode
	}
	var directories []directoryMode
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == "." {
			return err
		}
		destination := filepath.Join(target, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			if err := os.Mkdir(destination, 0o700); err != nil {
				return err
			}
			directories = append(directories, directoryMode{path: destination, mode: info.Mode()})
			return nil
		case entry.Type()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, destination)
		case info.Mode().IsRegular():
			return copyRegularFile(path, destination, info.Mode())
		default:
			return fmt.Errorf("copy %s: unsupported file mode %s", path, info.Mode())
		}
	})
	if err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := os.Chmod(directories[index].path, directories[index].mode.Perm()); err != nil {
			return err
		}
	}
	return nil
}

func copyRegularFile(source, target string, mode fs.FileMode) (err error) {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := output.Close(); err == nil {
			err = closeErr
		}
	}()
	if _, err = io.Copy(output, input); err != nil {
		return err
	}
	if err = output.Sync(); err != nil {
		return err
	}
	return output.Chmod(mode.Perm())
}
