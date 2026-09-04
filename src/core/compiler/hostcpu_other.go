//go:build !darwin && !linux

package compiler

func hostCPUBrand() string { return "" }
