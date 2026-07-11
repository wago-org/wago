//go:build darwin && arm64 && !tinygo

package runtime

import (
	"syscall"
	"unsafe"
)

const madvZero = 11

func madviseDontNeed(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_MADVISE,
		uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), madvZero); errno == 0 {
		return nil
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_MADVISE,
		uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), syscall.MADV_DONTNEED); errno != 0 {
		return errno
	}
	return nil
}
