//go:build !linux || wago_lean

package run

// SuperviseWatchedChild is unavailable outside standard Linux builds.
func SuperviseWatchedChild() {}
