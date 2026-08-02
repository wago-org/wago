//go:build (linux || darwin || windows) && (amd64 || arm64) && (!wago_guardpage || windows || (darwin && amd64))

package runtime

func growGuardedHostView(*JobMemory, int) error { return nil }
