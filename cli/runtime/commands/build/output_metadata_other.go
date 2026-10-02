//go:build !unix && !windows

package build

import (
	"errors"
	"io"
	"os"
)

type buildOutputMetadata struct {
	exists bool
}

func sameBuildOutputMetadata(left, right buildOutputMetadata) bool {
	return left == right
}

func captureBuildOutputMetadata(_ string, info os.FileInfo, _ buildFileIdentity) (buildOutputMetadata, error) {
	return buildOutputMetadata{exists: info != nil}, nil
}

func captureNewBuildOutputMetadata(string) (buildOutputMetadata, error) {
	return buildOutputMetadata{}, nil
}

func applyBuildOutputMetadata(_ io.Writer, metadata buildOutputMetadata) error {
	if metadata.exists {
		// Replacing an inode without a supported ownership API would silently
		// change established access; retain the old artifact instead.
		return errors.New("preserving existing output ownership is unsupported on this platform")
	}
	return nil
}
