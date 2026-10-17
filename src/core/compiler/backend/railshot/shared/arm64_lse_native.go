//go:build arm64 && !darwin && !tinygo

package shared

import "golang.org/x/sys/cpu"

func ARM64SupportsLSE() bool { return cpu.ARM64.HasATOMICS }
