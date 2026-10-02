//go:build !(linux && amd64) && !((linux || darwin) && arm64)

package wago

type gcHostSavedControl [0]uint64

// Keep the token shape uniform across platforms so shared root-management code
// compiles unchanged. Unsupported platforms always return the zero token.
type gcHostActivationToken struct {
	state *gcPublicState
	index uint8
}

func (*Instance) pushGCHostActivation(uintptr, uint32) gcHostActivationToken {
	return gcHostActivationToken{}
}

func (*Instance) popGCHostActivation(gcHostActivationToken) {}
