//go:build darwin

package build

import (
	"os"
	"syscall"

	"github.com/wago-org/wago/internal/atomicfile"
)

func captureNewBuildOutputMetadata(path string) (metadata buildOutputMetadata, resultErr error) {
	resultErr = atomicfile.InspectNewFileMetadata(path, 0o644, func(file *os.File) error {
		info, err := file.Stat()
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return &os.PathError{Op: "inspect new output ownership", Path: path, Err: syscall.EINVAL}
		}
		acl, present, labels, err := captureNewBuildAccessMetadata(file)
		if err != nil {
			return err
		}
		metadata = buildOutputMetadata{
			set: true, uid: int(stat.Uid), gid: int(stat.Gid),
			accessACL: acl, accessACLPresent: present, securityLabels: labels,
		}
		return nil
	})
	return metadata, resultErr
}
