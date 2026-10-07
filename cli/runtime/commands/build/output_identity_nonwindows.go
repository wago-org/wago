//go:build !windows

package build

import (
	"os"
	"syscall"
)

type buildFileIdentity struct {
	dev, ino  uint64
	linkCount uint64
}

func captureBuildFileIdentity(_ string, _ bool, info os.FileInfo) (buildFileIdentity, error) {
	// Non-Windows FileInfo values retain stable device/inode identity, so reuse
	// the stat already needed for type and mode checks without another syscall.
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return buildFileIdentity{}, formatBuildError("identify file: unexpected file info %T", info.Sys())
	}
	return buildFileIdentity{dev: uint64(stat.Dev), ino: uint64(stat.Ino), linkCount: uint64(stat.Nlink)}, nil
}

func sameBuildFileIdentity(left, right buildFileIdentity) bool {
	return left.dev == right.dev && left.ino == right.ino
}

func validateBuildOutputPublication(path string, info os.FileInfo, identity buildFileIdentity) error {
	if identity.linkCount > 1 {
		// Renaming a new inode updates only one directory entry, unlike the old
		// in-place write shared by every hard link. Reject instead of reporting a
		// successful build while aliases continue serving stale bytes.
		return buildErrorS("output %s has multiple hard links", path)
	}
	if info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		// An in-place content write may clear privilege bits according to the
		// writer's credentials, while a replacement inode has unrelated mode-bit
		// semantics. Reject instead of silently retaining or stripping access bits.
		return buildErrorS("output %s has setuid, setgid, or sticky mode bits", path)
	}
	if err := validateBuildOutputPlatformMetadata(path, info); err != nil {
		return err
	}
	if err := validateBuildOutputWriteAccess(path, identity); err != nil {
		return err
	}
	return nil
}
