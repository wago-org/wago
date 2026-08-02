//go:build windows && (amd64 || arm64) && wago_guardpage

package runtime

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

const (
	_ = uint(abi.TrapCellPtrOffset - 104)
	_ = uint(104 - abi.TrapCellPtrOffset)
	_ = uint(unsafe.Sizeof(guardRegion{}) - 32)
	_ = uint(32 - unsafe.Sizeof(guardRegion{}))
	_ = uint(TrapLinMemOutOfBounds - 3)
	_ = uint(3 - TrapLinMemOutOfBounds)
	_ = uint(TrapLinMemCouldNotExtend - 4)
	_ = uint(4 - TrapLinMemCouldNotExtend)
)

var (
	procAddVectoredExceptionHandler = kernel32.NewProc("AddVectoredExceptionHandler")
	ntdll                           = syscall.NewLazyDLL("ntdll.dll")
	procNtAllocateVirtualMemory     = ntdll.NewProc("NtAllocateVirtualMemory")
	guardTrapExitHandlerJumpPC      uintptr
	guardNtAllocateVirtualMemoryPC  uintptr
)

func installWindowsExceptionHandler() error {
	if err := procNtAllocateVirtualMemory.Find(); err != nil {
		return fmt.Errorf("resolve NtAllocateVirtualMemory: %w", err)
	}
	guardNtAllocateVirtualMemoryPC = procNtAllocateVirtualMemory.Addr()
	guardTrapExitHandlerJumpPC = addrNativeTrapExitHandlerJump()
	handle, _, callErr := procAddVectoredExceptionHandler.Call(1, addrGuardExceptionHandler())
	if handle == 0 {
		return fmt.Errorf("AddVectoredExceptionHandler: %w", callErr)
	}
	return nil
}

// Both handlers are assembly-only: entering Go from a vectored exception that
// interrupted enterNative's foreign stack corrupts the runtime syscall frame.
func guardExceptionHandler()
func addrGuardExceptionHandler() uintptr
func nativeTrapExitHandlerJump()
func addrNativeTrapExitHandlerJump() uintptr
