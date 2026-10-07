//go:build unix && !linux && !darwin

package build

import (
	"errors"
	"os"
)

func captureBuildAccessMetadata(_ string, _ buildFileIdentity) ([]byte, bool, []buildOutputSecurityLabel, os.FileInfo, error) {
	// Publishing a replacement without a native access-ACL implementation could
	// silently revoke service access. Existing outputs therefore fail closed.
	return nil, false, nil, nil, errors.New("preserving existing output access ACLs is unsupported on this platform")
}

func applyBuildAccessMetadata(_ *os.File, _ []byte, _ bool, _ []buildOutputSecurityLabel) error {
	return nil
}
