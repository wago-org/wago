//go:build windows && (amd64 || arm64) && wago_guardpage

package runtime

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const (
	exceptionAccessViolation      = 0xc0000005
	exceptionContinueExecution    = ^uintptr(0)
	exceptionContinueSearch       = uintptr(0)
	exceptionInformationFaultAddr = 1
)

var procAddVectoredExceptionHandler = kernel32.NewProc("AddVectoredExceptionHandler")

type exceptionRecord struct {
	code       uint32
	flags      uint32
	record     uintptr
	address    uintptr
	parameters uint32
	_          uint32
	info       [15]uintptr
}

type exceptionPointers struct {
	record  *exceptionRecord
	context unsafe.Pointer
}

// These prefixes mirror the documented Windows CONTEXT layouts through the
// registers Wago reads or rewrites. Keeping the exception handler in Go lets
// the runtime callback trampoline move safely off the native Wago stack.
type contextAMD64 struct {
	home         [6]uint64
	contextFlags uint32
	mxCsr        uint32
	segments     [6]uint16
	eFlags       uint32
	debug        [6]uint64
	rax          uint64
	rcx          uint64
	rdx          uint64
	rbx          uint64
	rsp          uint64
	rbp          uint64
	rsi          uint64
	rdi          uint64
	r8           uint64
	r9           uint64
	r10          uint64
	r11          uint64
	r12          uint64
	r13          uint64
	r14          uint64
	r15          uint64
	rip          uint64
}

type contextARM64 struct {
	contextFlags uint32
	cpsr         uint32
	x            [31]uint64
	sp           uint64
	pc           uint64
}

var guardTrapExitHandlerJumpPC uintptr

func installWindowsExceptionHandler() error {
	guardTrapExitHandlerJumpPC = addrNativeTrapExitHandlerJump()
	callback := syscall.NewCallback(guardExceptionHandler)
	handle, _, callErr := procAddVectoredExceptionHandler.Call(1, callback)
	if handle == 0 {
		return fmt.Errorf("AddVectoredExceptionHandler: %w", callErr)
	}
	return nil
}

func guardExceptionHandler(raw uintptr) uintptr {
	pointers := (*exceptionPointers)(unsafe.Pointer(raw))
	if pointers == nil || pointers.record == nil || pointers.record.code != exceptionAccessViolation || pointers.record.parameters < 2 {
		return exceptionContinueSearch
	}
	fault := pointers.record.info[exceptionInformationFaultAddr]
	for i := range guardRegions {
		start := atomic.LoadUintptr(&guardRegions[i].start)
		if start == 0 || fault < start || fault >= guardRegions[i].end {
			continue
		}
		linMem := guardRegions[i].linMem
		var pinned uintptr
		if runtime.GOARCH == "amd64" {
			pinned = uintptr((*contextAMD64)(pointers.context).rbx)
		} else {
			pinned = uintptr((*contextARM64)(pointers.context).x[26])
		}
		if pinned != linMem {
			continue
		}
		off := fault - linMem
		curBytes := uintptr(*(*uint32)(unsafe.Pointer(linMem - 8)))
		if off < curBytes {
			page := fault &^ uintptr(wasmPageBytes-1)
			ptr, _, _ := procVirtualAlloc.Call(page, wasmPageBytes, memCommit, pageReadWrite)
			if ptr == page {
				return exceptionContinueExecution
			}
			setWindowsGuardTrap(pointers.context, linMem, TrapLinMemCouldNotExtend)
			return exceptionContinueExecution
		}
		setWindowsGuardTrap(pointers.context, linMem, TrapLinMemOutOfBounds)
		return exceptionContinueExecution
	}
	return exceptionContinueSearch
}

func setWindowsGuardTrap(context unsafe.Pointer, linMem uintptr, code TrapCode) {
	trap := *(*uintptr)(unsafe.Pointer(linMem - 104))
	if trap == 0 {
		return
	}
	*(*uint32)(unsafe.Pointer(trap)) = uint32(code)
	if runtime.GOARCH == "amd64" {
		(*contextAMD64)(context).rip = uint64(guardTrapExitHandlerJumpPC)
	} else {
		ctx := (*contextARM64)(context)
		ctx.x[9] = uint64(linMem)
		ctx.pc = uint64(guardTrapExitHandlerJumpPC)
	}
}

func nativeTrapExitHandlerJump()
func addrNativeTrapExitHandlerJump() uintptr
