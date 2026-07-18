//go:build linux && (amd64 || arm64)

package runtime

func syncInstructionCache(_ []byte, _ int) error { return nil }
