//go:build !amd64

package wago

func (in *Instance) prepareNativeStructHandles(uint32)           {}
func (in *Instance) prepareNativeArrayAllocation(uint32, uint32) {}
