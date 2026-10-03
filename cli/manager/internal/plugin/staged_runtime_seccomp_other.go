//go:build linux && !amd64 && !arm64

package plugin

func stagedRuntimeSeccompArchitecture() (uint32, []uint32, []uint32, bool) {
	return 0, nil, nil, false
}
