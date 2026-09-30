//go:build darwin && (amd64 || arm64) && !tinygo

package runtime

import (
	"syscall"
	_ "unsafe"
)

// syscall6 enters a dynamically imported libSystem function through the Go
// runtime's foreign-call boundary. Darwin guard faults and cold Mach-thread
// interruption share this declaration.
//
//go:linkname syscall6 syscall.syscall6
func syscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)
