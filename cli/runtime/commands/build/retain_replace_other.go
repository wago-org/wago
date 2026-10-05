//go:build !windows && !darwin && !linux

package build

func retainBuildReplaceHandle(bool) bool { return false }
