//go:build linux && riscv64

// Package riscv64spike proves wago's no-cgo foreign-stack JIT boundary on
// Linux/RV64 before the production runtime and railshot backend depend on it.
package riscv64spike

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

const (
	pageSize            = 4096
	foreignStackBytes   = 256 * 1024
	sysRISCVFlushICache = 259
)

func mmapRW(n int) ([]byte, error) {
	if n <= 0 {
		n = pageSize
	}
	n = (n + pageSize - 1) &^ (pageSize - 1)
	return syscall.Mmap(-1, 0, n, syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_ANON|syscall.MAP_PRIVATE)
}

func flushICache(mem []byte, codeLen int) error {
	if len(mem) == 0 || codeLen == 0 {
		return nil
	}
	start := uintptr(unsafe.Pointer(&mem[0]))
	_, _, errno := syscall.RawSyscall(sysRISCVFlushICache, start, start+uintptr(codeLen), 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// MapExec copies code into a fresh RW mapping, changes it to R-X, and performs
// Linux's process-visible RISC-V instruction-cache synchronization.
func MapExec(code []byte) ([]byte, error) {
	mem, err := mmapRW(len(code))
	if err != nil {
		return nil, err
	}
	copy(mem, code)
	if err := syscall.Mprotect(mem, syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		_ = syscall.Munmap(mem)
		return nil, err
	}
	if err := flushICache(mem, len(code)); err != nil {
		_ = syscall.Munmap(mem)
		return nil, fmt.Errorf("riscv_flush_icache: %w", err)
	}
	return mem, nil
}

// MapRW maps stable read-write memory for foreign stacks and execution tests.
func MapRW(n int) ([]byte, error) { return mmapRW(n) }

var (
	stackOnce sync.Once
	stackMu   sync.Mutex
	stackMem  []byte
	stackTop  uintptr
	stackErr  error
)

func ensureStack() uintptr {
	stackOnce.Do(func() {
		stackMem, stackErr = mmapRW(foreignStackBytes)
		if stackErr != nil {
			return
		}
		stackTop = uintptr(unsafe.Pointer(&stackMem[0])) + uintptr(len(stackMem))
		stackTop &^= 15
	})
	if stackErr != nil {
		panic("riscv64spike: foreign stack mmap: " + stackErr.Error())
	}
	return stackTop
}

// Call2 executes entry with A0=a0 and A1=a1 on the off-heap foreign stack and
// returns A0. The trampoline preserves the Go and psABI callee-saved context.
func Call2(entry, a0, a1 uintptr) uintptr {
	stackMu.Lock()
	defer stackMu.Unlock()
	return enterNativeSpike(entry, a0, a1, ensureStack())
}

// Call3 additionally pins linMem in S9/X25, the proposed wago RV64 linear-memory
// register. X26 (Go CTXT) and X27 (Go g) remain unavailable to generated code.
func Call3(entry, a0, a1, linMem uintptr) uintptr {
	stackMu.Lock()
	defer stackMu.Unlock()
	return enterNativeMem(entry, a0, a1, linMem, ensureStack())
}

func enterNativeSpike(code, a0, a1, foreignStackTop uintptr) uintptr
func enterNativeMem(code, a0, a1, linMem, foreignStackTop uintptr) uintptr
