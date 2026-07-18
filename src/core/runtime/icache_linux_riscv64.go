//go:build linux && riscv64

package runtime

import (
	"fmt"
	"syscall"
	"unsafe"
)

const sysRISCVFlushICache = 259

func syncInstructionCache(mem []byte, codeLen int) error {
	if len(mem) == 0 || codeLen == 0 {
		return nil
	}
	start := uintptr(unsafe.Pointer(&mem[0]))
	_, _, errno := syscall.RawSyscall(sysRISCVFlushICache, start, start+uintptr(codeLen), 0)
	if errno != 0 {
		return fmt.Errorf("riscv_flush_icache: %w", errno)
	}
	return nil
}
