//go:build amd64 && (!linux || tinygo || wago_target_tinygo)

package amd64

func newOperandArena() *operandArena { return nil }
