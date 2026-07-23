//go:build !wago_guardpage && ((linux && (amd64 || arm64)) || (darwin && arm64))

package runtime

func growGuardedHostView(*JobMemory, int) error { return nil }
