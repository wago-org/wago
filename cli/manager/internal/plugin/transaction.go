package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/internal/atomicfile"
	"github.com/wago-org/wago/internal/filelock"
)

var replacePluginFile = atomicfile.ReplaceFile

func withPluginMutationLock(ctx context.Context, manifestDir string, fn func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return err
	}
	lockPath := filepath.Join(manifestDir, ".wago", "plugin-transaction.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return err
	}
	lock, err := filelock.Acquire(ctx, lockPath)
	if err != nil {
		return fmt.Errorf("lock plugin transaction: %w", err)
	}
	return errors.Join(fn(), lock.Close())
}

type fileSnapshot struct {
	path    string
	data    []byte
	existed bool
	mode    os.FileMode
}

func snapshotFile(path string) (fileSnapshot, error) {
	snapshot := fileSnapshot{path: path, mode: 0o644}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return snapshot, fmt.Errorf("transaction target %s is not a regular file", path)
	}
	snapshot.data, err = os.ReadFile(path)
	if err != nil {
		return snapshot, err
	}
	snapshot.existed, snapshot.mode = true, info.Mode().Perm()
	return snapshot, nil
}

func restoreSnapshot(snapshot fileSnapshot) error {
	if !snapshot.existed {
		if err := os.Remove(snapshot.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return atomicfile.ReplaceFile(snapshot.path, atomicfile.Options{Mode: snapshot.mode, Sync: true}, func(writer io.Writer) error {
		_, err := writer.Write(snapshot.data)
		return err
	})
}

// publishPluginTransaction makes a fully staged build, manifest, and lockfile
// visible as one recoverable transaction. Each file replacement is atomic; if
// any publication step fails, all previously visible state is restored before
// returning the error.
func publishPluginTransaction(manifestDir, buildDir, stagedBuildDir string, manifestData, lockData []byte) error {
	manifestPath, lockPath := project.Path(manifestDir), project.LockPath(manifestDir)
	manifestBefore, err := snapshotFile(manifestPath)
	if err != nil {
		return err
	}
	lockBefore, err := snapshotFile(lockPath)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(stagedBuildDir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("staged plugin build is not a directory: %s", stagedBuildDir)
	}
	if err := os.MkdirAll(filepath.Dir(buildDir), 0o755); err != nil {
		return err
	}
	backup, err := os.MkdirTemp(filepath.Dir(buildDir), ".wago-plugin-previous-*")
	if err != nil {
		return err
	}
	if err := os.Remove(backup); err != nil {
		return err
	}
	hadBuild := false
	if info, statErr := os.Lstat(buildDir); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin build target %s is not a directory", buildDir)
		}
		if err := os.Rename(buildDir, backup); err != nil {
			return fmt.Errorf("stage previous plugin build: %w", err)
		}
		hadBuild = true
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	buildPublished := false
	rollback := func(cause error) error {
		var rollbackErrs []error
		if buildPublished {
			if err := os.RemoveAll(buildDir); err != nil {
				rollbackErrs = append(rollbackErrs, err)
			}
		}
		if hadBuild {
			if err := os.Rename(backup, buildDir); err != nil {
				rollbackErrs = append(rollbackErrs, err)
			}
		}
		if err := restoreSnapshot(manifestBefore); err != nil {
			rollbackErrs = append(rollbackErrs, err)
		}
		if err := restoreSnapshot(lockBefore); err != nil {
			rollbackErrs = append(rollbackErrs, err)
		}
		return errors.Join(append([]error{cause}, rollbackErrs...)...)
	}
	if err := os.Rename(stagedBuildDir, buildDir); err != nil {
		if hadBuild {
			_ = os.Rename(backup, buildDir)
		}
		return fmt.Errorf("publish staged plugin build: %w", err)
	}
	buildPublished = true
	write := func(path string, data []byte) error {
		return replacePluginFile(path, atomicfile.Options{Mode: 0o644, Sync: true}, func(writer io.Writer) error {
			_, err := writer.Write(data)
			return err
		})
	}
	if err := write(manifestPath, manifestData); err != nil {
		return rollback(fmt.Errorf("publish %s: %w", project.File, err))
	}
	if err := write(lockPath, lockData); err != nil {
		return rollback(fmt.Errorf("publish %s: %w", project.LockFile, err))
	}
	if hadBuild {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("remove previous plugin build: %w", err)
		}
	}
	return nil
}
