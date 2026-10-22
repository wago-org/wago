//go:build linux && (amd64 || arm64)

package runtime

func synchronizeExecutableCode([]byte) error { return nil }
