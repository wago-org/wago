//go:build unix

package build

import (
	"bytes"
	"io"
	"os"
	"syscall"
)

type buildOutputMetadata struct {
	set              bool
	uid, gid         int
	accessACL        []byte
	accessACLPresent bool
	securityLabels   []buildOutputSecurityLabel
}

type buildOutputSecurityLabel struct {
	name    string
	value   []byte
	present bool
}

func sameBuildOutputMetadata(left, right buildOutputMetadata) bool {
	if left.set != right.set || left.uid != right.uid || left.gid != right.gid ||
		left.accessACLPresent != right.accessACLPresent || !bytes.Equal(left.accessACL, right.accessACL) ||
		len(left.securityLabels) != len(right.securityLabels) {
		return false
	}
	for index := range left.securityLabels {
		leftLabel, rightLabel := left.securityLabels[index], right.securityLabels[index]
		if leftLabel.name != rightLabel.name || leftLabel.present != rightLabel.present ||
			!bytes.Equal(leftLabel.value, rightLabel.value) {
			return false
		}
	}
	return true
}

func captureBuildOutputMetadata(path string, info os.FileInfo, expected buildFileIdentity) (buildOutputMetadata, error) {
	if info == nil {
		return buildOutputMetadata{}, nil
	}
	accessACL, accessACLPresent, securityLabels, openedInfo, err := captureBuildAccessMetadata(path, expected)
	if err != nil {
		return buildOutputMetadata{}, err
	}
	stat, ok := openedInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return buildOutputMetadata{}, formatBuildError("inspect output ownership: unexpected file info %T", openedInfo.Sys())
	}
	return buildOutputMetadata{
		set: true, uid: int(stat.Uid), gid: int(stat.Gid),
		accessACL: accessACL, accessACLPresent: accessACLPresent, securityLabels: securityLabels,
	}, nil
}

func applyBuildOutputMetadata(writer io.Writer, metadata buildOutputMetadata) error {
	if !metadata.set {
		return nil
	}
	file, ok := writer.(*os.File)
	if !ok {
		return formatBuildError("preserve output ownership: unexpected writer %T", writer)
	}
	info, err := file.Stat()
	if err != nil {
		return buildErrorE("inspect staged output ownership: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return formatBuildError("inspect staged output ownership: unexpected file info %T", info.Sys())
	}
	if int(stat.Uid) != metadata.uid || int(stat.Gid) != metadata.gid {
		// Atomic replacement creates a new inode owned by the builder. Restore the
		// established owner before its ACL: chown may recalculate or strip ACL data.
		// This syscall is deliberately authoritative instead of duplicating Unix
		// capability and credential preflights: rejection still occurs on the
		// private stage before rename, preserving the established artifact.
		if err := file.Chown(metadata.uid, metadata.gid); err != nil {
			return buildErrorE("atomic replacement requires permission to preserve existing ownership: %w", err)
		}
	}
	if err := applyBuildAccessMetadata(file, metadata.accessACL, metadata.accessACLPresent, metadata.securityLabels); err != nil {
		return buildErrorE("preserve output access metadata: %w", err)
	}
	return nil
}
