//go:build linux && arm64 && wago_guardpage

package runtime

import (
	"fmt"
	goruntime "runtime"
	"sync"
	"sync/atomic"
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
	_ = uint(TrapLinMemOutOfBounds - 3)
	_ = uint(3 - TrapLinMemOutOfBounds)
	_ = uint(TrapLinMemCouldNotExtend - 4)
	_ = uint(4 - TrapLinMemCouldNotExtend)
)

// Guard-page trap handler (EXPERIMENTAL). This is the arm64 twin of the amd64
// handler, but arm64 has only the handler-jump ABI: linMem is pinned in X26 and
// the trap-cell pointer lives in basedata at [linMem-TrapCellPtrOffset].
type guardRegion struct {
	start       uintptr
	end         uintptr
	linMem      uintptr
	ownerLinMem uintptr
}

const maxGuardRegions = 256

var (
	guardRegions               [maxGuardRegions]guardRegion
	guardRegionMu              sync.Mutex
	guardTrapExitHandlerJumpPC uintptr
	guardOldSEGVHandler        uintptr
	guardOldBUSHandler         uintptr

	guardMu        sync.Mutex
	guardInstalled bool
)

func registerGuardRegion(start, end, linMem uintptr) error {
	guardRegionMu.Lock()
	defer guardRegionMu.Unlock()
	for i := range guardRegions {
		if guardRegions[i].start == 0 {
			guardRegions[i].linMem = linMem
			guardRegions[i].ownerLinMem = linMem
			guardRegions[i].end = end
			// Publish last; the ARM64 handler acquire-loads start before it
			// consumes the remaining fields.
			atomic.StoreUintptr(&guardRegions[i].start, start)
			return nil
		}
	}
	return fmt.Errorf("guard region table full (%d)", maxGuardRegions)
}

func setGuardRegionOwner(start, ownerLinMem uintptr) {
	guardRegionMu.Lock()
	defer guardRegionMu.Unlock()
	for i := range guardRegions {
		if guardRegions[i].start == start {
			atomic.StoreUintptr(&guardRegions[i].ownerLinMem, ownerLinMem)
			return
		}
	}
}

func unregisterGuardRegion(start uintptr) {
	guardRegionMu.Lock()
	defer guardRegionMu.Unlock()
	for i := range guardRegions {
		if guardRegions[i].start == start {
			// Disable first so the handler cannot begin a new match while the
			// remaining fields are cleared.
			atomic.StoreUintptr(&guardRegions[i].start, 0)
			guardRegions[i].end = 0
			guardRegions[i].linMem = 0
			guardRegions[i].ownerLinMem = 0
			return
		}
	}
}

func init() {
	guardCloseHook = unregisterGuardRegion
	guardOwnerHook = setGuardRegionOwner
}

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
	_SA_SIGINFO        = 0x00000004
	_SA_EXPOSE_TAGBITS = 0x00000800
	_SA_ONSTACK        = 0x08000000
	_SA_RESTART        = 0x10000000
	_SA_NODEFER        = 0x40000000
	_SA_RESETHAND      = 0x80000000
)

func rtSigaction(sig uintptr, act, old *kernelSigaction) error {
	_, _, errno := syscall.Syscall6(syscall.SYS_RT_SIGACTION, sig,
		uintptr(unsafe.Pointer(act)), uintptr(unsafe.Pointer(old)), 8, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func installLinuxSignalHandlers(act *kernelSigaction, call func(uintptr, *kernelSigaction, *kernelSigaction) error) error {
	var oldSEGV kernelSigaction
	if err := call(uintptr(syscall.SIGSEGV), nil, &oldSEGV); err != nil {
		return fmt.Errorf("read SIGSEGV handler: %w", err)
	}
	if oldSEGV.handler <= 1 {
		return fmt.Errorf("install SIGSEGV handler: previous disposition %#x is not chainable", oldSEGV.handler)
	}
	var oldBUS kernelSigaction
	if err := call(uintptr(syscall.SIGBUS), nil, &oldBUS); err != nil {
		return fmt.Errorf("read SIGBUS handler: %w", err)
	}
	if oldBUS.handler <= 1 {
		return fmt.Errorf("install SIGBUS handler: previous disposition %#x is not chainable", oldBUS.handler)
	}
	if oldSEGV.flags&_SA_RESETHAND != 0 || oldBUS.flags&_SA_RESETHAND != 0 {
		return fmt.Errorf("install signal handlers: one-shot prior disposition is not chainable")
	}
	segvAct, busAct := *act, *act
	segvAct.mask = oldSEGV.mask
	busAct.mask = oldBUS.mask
	segvAct.flags |= oldSEGV.flags & (_SA_EXPOSE_TAGBITS | _SA_RESTART | _SA_NODEFER)
	busAct.flags |= oldBUS.flags & (_SA_EXPOSE_TAGBITS | _SA_RESTART | _SA_NODEFER)

	guardOldSEGVHandler = oldSEGV.handler
	guardOldBUSHandler = oldBUS.handler
	if err := call(uintptr(syscall.SIGSEGV), &segvAct, nil); err != nil {
		guardOldSEGVHandler = 0
		guardOldBUSHandler = 0
		return fmt.Errorf("install SIGSEGV handler: %w", err)
	}
	if err := call(uintptr(syscall.SIGBUS), &busAct, nil); err != nil {
		rollback := call(uintptr(syscall.SIGSEGV), &oldSEGV, nil)
		guardOldSEGVHandler = 0
		guardOldBUSHandler = 0
		if rollback != nil {
			return fmt.Errorf("install SIGBUS handler: %w (restore SIGSEGV: %v)", err, rollback)
		}
		return fmt.Errorf("install SIGBUS handler: %w", err)
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
	if err := installLinuxSignalHandlers(&act, rtSigaction); err != nil {
		return err
	}
	guardInstalled = true
	return nil
}

func (e *Engine) CallGuarded(code uintptr, serArgs []byte, linMemBase uintptr, trap, results []byte, j *JobMemory) error {
	if j.reserveBase == 0 || linMemBase == 0 {
		return fmt.Errorf("CallGuarded requires NewJobMemoryGuarded")
	}
	if err := validateTrapBuffer(trap); err != nil {
		return err
	}
	clearTrapUnlessInterrupted(trap)
	j.putU64(abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	enterNative(code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop)
	goruntime.KeepAlive(serArgs)
	goruntime.KeepAlive(trap)
	goruntime.KeepAlive(results)
	goruntime.KeepAlive(j)
	goruntime.KeepAlive(e)
	if tc := TrapCode(loadTrap(trap)); tc != TrapNone {
		return trapErrorFromBuffer(tc, trap)
	}
	return nil
}
