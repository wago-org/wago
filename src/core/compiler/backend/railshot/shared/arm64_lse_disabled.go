//go:build !arm64 || tinygo

package shared

func ARM64SupportsLSE() bool { return false }
