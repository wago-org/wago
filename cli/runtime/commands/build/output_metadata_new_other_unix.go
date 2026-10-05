//go:build unix && !darwin

package build

func captureNewBuildOutputMetadata(string) (buildOutputMetadata, error) {
	return buildOutputMetadata{}, nil
}
