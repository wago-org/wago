//go:build !tinygo

package wago

func retainRuntimeCloseTask(*runtimeCloseTask)  {}
func releaseRuntimeCloseTask(*runtimeCloseTask) {}
