//go:build !linux || tinygo || (!amd64 && !arm64 && (!riscv64 || wago_target_tinygo))

package runtime

func compilerCodeBufferOps() *codeBufferOps { return nil }
