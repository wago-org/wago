//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"fmt"
	"golang.org/x/sys/unix"
	"syscall"
)

func synchronizeExecutableCode(mem []byte) error {
	return synchronizeExecutableRange(slicePtr(mem), len(mem), syscall.Syscall)
}

type icacheSyscall func(uintptr, uintptr, uintptr, uintptr) (uintptr, uintptr, syscall.Errno)

// The syscall seam is local to this operation, allowing failure injection
// without changing publication behavior in another goroutine.
func synchronizeExecutableRange(start uintptr, length int, call icacheSyscall) error {
	if start == 0 || length <= 0 {
		return fmt.Errorf("runtime: empty RV64 cache synchronization range")
	}
	if uintptr(length) > ^uintptr(0)-start {
		return fmt.Errorf("runtime: RV64 cache range overflows")
	}
	// flags=0 synchronizes the entire process, including migration between harts.
	// Local FENCE.I cannot establish process-wide publication visibility.
	_, _, errno := call(unix.SYS_RISCV_FLUSH_ICACHE, start, start+uintptr(length), 0)
	if errno != 0 {
		return errno
	}
	return nil
}
