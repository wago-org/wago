//go:build !linux || !amd64

package wago

type gcHostActivationToken struct{}

func (*Instance) pushGCHostActivation(uintptr, uint32) gcHostActivationToken {
	return gcHostActivationToken{}
}

func (*Instance) popGCHostActivation(gcHostActivationToken) {}
