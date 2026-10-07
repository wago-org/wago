//go:build !darwin && !linux && !windows

package build

func validateBuildOutputWriteAccess(_ string, _ buildFileIdentity) error {
	// Unsupported Unix targets already fail closed during ACL metadata capture.
	return nil
}
