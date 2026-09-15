//go:build windows

package managedrelease

import (
	"errors"
	"syscall"
)

func processIdentity(pid int) (uint64, bool, error) {
	if pid <= 0 {
		return 0, false, nil
	}
	handle, err := syscall.OpenProcess(0x1000, false, uint32(pid)) // PROCESS_QUERY_LIMITED_INFORMATION
	if errors.Is(err, syscall.Errno(87)) {
		return 0, false, nil
	} // exited PID
	if err != nil {
		return 0, false, err
	}
	defer syscall.CloseHandle(handle)
	var created, exited, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return 0, false, err
	}
	identity := uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	alive := exited.HighDateTime == 0 && exited.LowDateTime == 0
	return identity, alive, nil
}
