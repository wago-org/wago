//go:build (linux && (amd64 || arm64 || riscv64)) || (darwin && arm64 && !tinygo)

package runtime

import "syscall"

func munmapRange(base, length uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_MUNMAP, base, length, 0); errno != 0 {
		return errno
	}
	return nil
}
