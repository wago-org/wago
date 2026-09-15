//go:build tinygo

package compiler

// TinyGo cannot link x/sys/cpu's assembly probes on every supported target.
// Its native target therefore remains conservative and advertises no optional
// ISA features; baseline amd64 and arm64 code generation remains available.
func applyHostTargetFeatures(*Target) {}
