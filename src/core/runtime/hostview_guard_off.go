//go:build (linux || darwin || windows) && (amd64 || arm64) && !wago_guardpage

package runtime

func growGuardedHostView(*JobMemory, int) error { return nil }
