//go:build (linux || darwin) && arm64

// Package arm64spike is a throwaway P1 go/no-go experiment: it proves that Go can
// map AArch64 machine code executable and call into it with NO cgo, using the
// same off-heap "foreign stack" + register-save trampoline technique the real
// amd64 runtime uses (src/core/runtime/trampoline_amd64.s). If this executes
// correctly under qemu-user (or on real arm64), the entire arm64 port is viable;
// if not, the runtime design must change before any codegen work. Once the real
// enterNative arm64 twin lands, this package is deleted.
package arm64spike

import (
	"sync"
	"syscall"
	"unsafe"
)

func mmapRW(n int) ([]byte, error) {
	if n <= 0 {
		n = 4096
	}
	n = (n + 4095) &^ 4095
	return syscall.Mmap(-1, 0, n, syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_ANON|syscall.MAP_PRIVATE)
}

// mmapExec copies code into a fresh mapping and flips it to R-X (W^X), returning
// the executable bytes. Portable syscall API — identical to the amd64 runtime's
// mmapExec, just arch-gated.
func mmapExec(code []byte) ([]byte, error) {
	mem, err := mmapRW(len(code))
	if err != nil {
		return nil, err
	}
	copy(mem, code)
	if err := syscall.Mprotect(mem, syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		_ = syscall.Munmap(mem)
		return nil, err
	}
	return mem, nil
}

// MapExec maps code executable (W^X). Exported for backend beachhead tests.
func MapExec(code []byte) ([]byte, error) { return mmapExec(code) }

var (
	spikeStackOnce sync.Once
	spikeStackTop  uintptr
)

// Call2 executes the mapped function at entry with two integer args (X0, X1) via
// the foreign-stack trampoline and returns X0. Exported so the railshot/arm64
// backend beachhead can run its compiled code through the proven no-cgo path.
func Call2(entry, a0, a1 uintptr) uintptr {
	spikeStackOnce.Do(func() {
		stack, err := mmapRW(256 * 1024)
		if err != nil {
			panic("arm64spike: foreign stack mmap: " + err.Error())
		}
		spikeStackTop = uintptr(unsafe.Pointer(&stack[0])) + uintptr(len(stack))
	})
	return enterNativeSpike(entry, a0, a1, spikeStackTop)
}

// enterNativeSpike switches to the off-heap foreign stack whose top (highest
// address, 16-byte aligned) is foreignStackTop, saves the Go callee-saved
// registers + g + goroutine SP, calls the AArch64 code at `code` with a0 in X0
// and a1 in X1 (AAPCS64), then restores the Go context and returns X0.
// Implemented in spike_arm64.s.
func enterNativeSpike(code, a0, a1, foreignStackTop uintptr) uintptr

// Call3 is like Call2 but also establishes X26 = linMem (the linear-memory base
// the wago ABI keeps pinned) before entering native code.
func Call3(entry, a0, a1, linMem uintptr) uintptr {
	spikeStackOnce.Do(func() {
		stack, err := mmapRW(256 * 1024)
		if err != nil {
			panic("arm64spike: foreign stack mmap: " + err.Error())
		}
		spikeStackTop = uintptr(unsafe.Pointer(&stack[0])) + uintptr(len(stack))
	})
	return enterNativeMem(entry, a0, a1, linMem, spikeStackTop)
}

func enterNativeMem(code, a0, a1, linMem, foreignStackTop uintptr) uintptr

// MapRW maps n bytes read-write (for a linear-memory buffer in tests).
func MapRW(n int) ([]byte, error) { return mmapRW(n) }
