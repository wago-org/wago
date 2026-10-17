//go:build !amd64 && !arm64

package wago

func (in *Instance) prepareNativeDeadStructBatch(uint32, uint32) {}

func (in *Instance) prepareNativeDeadArrayBatch(uint32, uint32, uint32) {}
