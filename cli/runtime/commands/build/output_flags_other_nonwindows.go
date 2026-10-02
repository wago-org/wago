//go:build !darwin && !windows

package build

import "os"

func validateBuildOutputPlatformMetadata(_ string, _ os.FileInfo) error {
	return nil
}
