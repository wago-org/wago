//go:build !linux || !arm64

package compiler

func hostHasARM64MOPS() bool { return false }
