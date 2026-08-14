package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wago-org/wago/cli/internal/project"
)

var publishProjectMetadata = func(mutation *project.Mutation, manifestData, lockData []byte) error {
	return mutation.PublishEncodedMetadata(manifestData, lockData)
}

func withPluginMutationLock(ctx context.Context, manifestDir string, fn func(*project.Mutation) error) error {
	return project.WithMutation(ctx, manifestDir, fn)
}

// publishPluginTransaction makes a fully staged build, manifest, and lockfile
// visible under the project-wide mutation lock. Metadata is crash-recoverable;
// failures before its journal commit restore the prior generated build.
func publishPluginTransaction(mutation *project.Mutation, buildDir, stagedBuildDir string, manifestData, lockData []byte) error {
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
	rollbackBuild := func(cause error) error {
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
		return errors.Join(append([]error{cause}, rollbackErrs...)...)
	}
	if err := os.Rename(stagedBuildDir, buildDir); err != nil {
		if hadBuild {
			_ = os.Rename(backup, buildDir)
		}
		return fmt.Errorf("publish staged plugin build: %w", err)
	}
	buildPublished = true
	if err := publishProjectMetadata(mutation, manifestData, lockData); err != nil {
		if !project.TransactionCommitted(err) {
			return rollbackBuild(err)
		}
		if hadBuild {
			err = errors.Join(err, os.RemoveAll(backup))
		}
		return err
	}
	if hadBuild {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("remove previous plugin build: %w", err)
		}
	}
	return nil
}
