//go:build linux && arm64 && wago_guardpage

package runtime

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// The asm handler (sigtrap_arm64.s, dotrap) hardcodes the trap-cell-pointer
// basedata displacement as -104. These two assertions fail to compile if
// abi.TrapCellPtrOffset ever changes, forcing the asm to be updated in lockstep.
const (
	_ = uint(abi.TrapCellPtrOffset - 104)
	_ = uint(104 - abi.TrapCellPtrOffset)
)

// Guard-page trap handler (EXPERIMENTAL). This is the arm64 twin of the amd64
// handler, but arm64 has only the handler-jump ABI: linMem is pinned in X26 and
// the trap-cell pointer lives in basedata at [linMem-TrapCellPtrOffset].
type guardRegion struct {
	start  uintptr
	end    uintptr
	linMem uintptr
	_      uintptr
}

const maxGuardRegions = 256

var (
	guardRegions               [maxGuardRegions]guardRegion
	guardRegionMu              sync.Mutex
	guardTrapExitHandlerJumpPC uintptr
	guardOldHandler            uintptr

	guardMu        sync.Mutex
	guardInstalled bool
)

func registerGuardRegion(start, end, linMem uintptr) error {
	guardRegionMu.Lock()
	defer guardRegionMu.Unlock()
	for i := range guardRegions {
		if guardRegions[i].start == 0 {
			guardRegions[i].linMem = linMem
			guardRegions[i].end = end
			guardRegions[i].start = start
			return nil
		}
	}
	return fmt.Errorf("guard region table full (%d)", maxGuardRegions)
}

func unregisterGuardRegion(start uintptr) {
	guardRegionMu.Lock()
	defer guardRegionMu.Unlock()
	for i := range guardRegions {
		if guardRegions[i].start == start {
			guardRegions[i].start = 0
			guardRegions[i].end = 0
			guardRegions[i].linMem = 0
			return
		}
	}
}

func init() { guardCloseHook = unregisterGuardRegion }

func nativeTrapExitHandlerJump()

func addrGuardSigHandler() uintptr
func addrNativeTrapExitHandlerJump() uintptr

func guardSigHandler()

type kernelSigaction struct {
	handler  uintptr
	flags    uint64
	restorer uintptr
	mask     uint64
}

const (
	_SA_SIGINFO = 0x00000004
	_SA_ONSTACK = 0x08000000
)

func rtSigaction(sig uintptr, act, old *kernelSigaction) error {
	_, _, errno := syscall.Syscall6(syscall.SYS_RT_SIGACTION, sig,
		uintptr(unsafe.Pointer(act)), uintptr(unsafe.Pointer(old)), 8, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func InstallGuardTrapHandler() error {
	guardMu.Lock()
	defer guardMu.Unlock()
	if guardInstalled {
		return nil
	}
	guardTrapExitHandlerJumpPC = addrNativeTrapExitHandlerJump()
	act := kernelSigaction{
		handler: addrGuardSigHandler(),
		flags:   _SA_SIGINFO | _SA_ONSTACK,
	}
	var old kernelSigaction
	if err := rtSigaction(uintptr(syscall.SIGSEGV), &act, &old); err != nil {
		return fmt.Errorf("install SIGSEGV handler: %w", err)
	}
	guardOldHandler = old.handler
	var oldBus kernelSigaction
	if err := rtSigaction(uintptr(syscall.SIGBUS), &act, &oldBus); err != nil {
		return fmt.Errorf("install SIGBUS handler: %w", err)
	}
	guardInstalled = true
	return nil
}

func (e *Engine) CallGuarded(code uintptr, serArgs, linMem, trap, results []byte, j *JobMemory) error {
	if j.reserveBase == 0 {
		return fmt.Errorf("CallGuarded requires NewJobMemoryGuarded")
	}
	linMemBase := j.LinMemBase()
	if len(trap) >= 4 {
		trap[0], trap[1], trap[2], trap[3] = 0, 0, 0, 0
		j.putU64(abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	}
	enterNative(code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop)
	if len(trap) >= 4 {
		if tc := TrapCode(uint32(trap[0]) | uint32(trap[1])<<8 | uint32(trap[2])<<16 | uint32(trap[3])<<24); tc != TrapNone {
			return &TrapError{Code: tc}
		}
	}
	return nil
}
