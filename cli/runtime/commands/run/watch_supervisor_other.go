//go:build !linux || wago_lean

package run

func maybeSuperviseWatchedChild() bool { return false }
