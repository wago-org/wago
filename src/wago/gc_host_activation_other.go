//go:build !(linux && amd64) && !((linux || darwin) && arm64)

package wago

type gcHostSavedControl [0]uint64

type gcHostActivationToken struct{}

func (*Instance) pushGCHostActivation(uintptr, uint32) gcHostActivationToken {
	return gcHostActivationToken{}
}

func (*Instance) popGCHostActivation(gcHostActivationToken) {}
