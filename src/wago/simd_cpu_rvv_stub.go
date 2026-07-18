//go:build !linux || !riscv64

package wago

func detectRISCV64SIMDHostFeatures() bool { return false }
