//go:build wago_guardpage && linux && (amd64 || arm64)

package runtime

import (
	"syscall"
	"unsafe"
)

func growGuardedHostView(j *JobMemory, logicalBytes int) error {
	committed := len(j.mem) - j.linOff
	if logicalBytes <= committed {
		return nil
	}
	start := j.reserveBase + uintptr(j.linOff+committed)
	length := uintptr(logicalBytes - committed)
	if _, _, errno := syscall.Syscall(syscall.SYS_MPROTECT, start, length, syscall.PROT_READ|syscall.PROT_WRITE); errno != 0 {
		return errno
	}
	j.mem = unsafe.Slice(&j.mem[0], j.linOff+logicalBytes)
	return nil
}
