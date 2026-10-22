//go:build !riscv64

package runtime

func nativeTrapOrigin([]byte) (uintptr, bool) { return 0, false }
