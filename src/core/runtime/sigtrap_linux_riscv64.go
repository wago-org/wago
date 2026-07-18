//go:build linux && riscv64 && wago_guardpage

package runtime

import (
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// The assembly handler hardcodes the basedata and Linux signal-context offsets
// asserted below. Keep these checks beside the Go mirrors so a layout edit fails
// at compile time instead of corrupting a signal frame at runtime.
type riscv64SignalStack struct {
	sp    uintptr
	flags int32
	_     int32
	size  uintptr
}

type riscv64SignalRegs struct {
	pc, ra, sp, gp, tp     uint64
	t0, t1, t2, s0, s1     uint64
	a0, a1, a2, a3, a4, a5 uint64
	a6, a7                 uint64
	s2, s3, s4, s5, s6, s7 uint64
	s8, s9, s10, s11       uint64
	t3, t4, t5, t6         uint64
}

type riscv64SignalContext struct {
	regs riscv64SignalRegs
	fp   [528]byte
}

type riscv64UContext struct {
	flags    uint64
	link     uintptr
	stack    riscv64SignalStack
	mask     [16]uint64
	pad      [8]byte
	mcontext riscv64SignalContext
}

const (
	riscv64MContextOffset = unsafe.Offsetof(riscv64UContext{}.mcontext)
	riscv64SavedPCOffset  = riscv64MContextOffset + unsafe.Offsetof(riscv64SignalContext{}.regs) + unsafe.Offsetof(riscv64SignalRegs{}.pc)
	riscv64SavedS9Offset  = riscv64MContextOffset + unsafe.Offsetof(riscv64SignalContext{}.regs) + unsafe.Offsetof(riscv64SignalRegs{}.s9)

	_ = uint(abi.TrapCellPtrOffset - 104)
	_ = uint(104 - abi.TrapCellPtrOffset)
	_ = uint(riscv64MContextOffset - 176)
	_ = uint(176 - riscv64MContextOffset)
	_ = uint(riscv64SavedPCOffset - 176)
	_ = uint(176 - riscv64SavedPCOffset)
	_ = uint(riscv64SavedS9Offset - 376)
	_ = uint(376 - riscv64SavedS9Offset)
)

// guardRegion is read directly by sigtrap_riscv64.s. start is published last
// and cleared first; the handler executes a full FENCE after observing it.
type guardRegion struct {
	start  uintptr
	end    uintptr
	linMem uintptr
	_      uintptr
}

const maxGuardRegions = 256

var (
	guardRegions        [maxGuardRegions]guardRegion
	guardRegionMu       sync.Mutex
	guardTrapExitPC     uintptr
	guardOldSEGVHandler uintptr
	guardOldBUSHandler  uintptr
	guardMu             sync.Mutex
	guardInstalled      bool
)

func registerGuardRegion(start, end, linMem uintptr) error {
	guardRegionMu.Lock()
	defer guardRegionMu.Unlock()
	for i := range guardRegions {
		if atomic.LoadUintptr(&guardRegions[i].start) == 0 {
			guardRegions[i].linMem = linMem
			guardRegions[i].end = end
			atomic.StoreUintptr(&guardRegions[i].start, start)
			return nil
		}
	}
	return fmt.Errorf("guard region table full (%d)", maxGuardRegions)
}

func unregisterGuardRegion(start uintptr) {
	guardRegionMu.Lock()
	defer guardRegionMu.Unlock()
	for i := range guardRegions {
		if atomic.LoadUintptr(&guardRegions[i].start) == start {
			atomic.StoreUintptr(&guardRegions[i].start, 0)
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

// Linux/riscv64's kernel sigaction has no sa_restorer field.
type kernelSigaction struct {
	handler uintptr
	flags   uint64
	mask    uint64
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

// InstallGuardTrapHandler installs the process-wide RV64 SIGSEGV/SIGBUS
// classifier. It is idempotent; every actual fault is classified from the
// faulting thread's S9 linear-memory register and the live reservation table.
func InstallGuardTrapHandler() error {
	guardMu.Lock()
	defer guardMu.Unlock()
	if guardInstalled {
		return nil
	}
	guardTrapExitPC = addrNativeTrapExitHandlerJump()
	act := kernelSigaction{
		handler: addrGuardSigHandler(),
		flags:   _SA_SIGINFO | _SA_ONSTACK,
	}
	var oldSEGV kernelSigaction
	if err := rtSigaction(uintptr(syscall.SIGSEGV), &act, &oldSEGV); err != nil {
		return fmt.Errorf("install SIGSEGV handler: %w", err)
	}
	guardOldSEGVHandler = oldSEGV.handler
	var oldBUS kernelSigaction
	if err := rtSigaction(uintptr(syscall.SIGBUS), &act, &oldBUS); err != nil {
		return fmt.Errorf("install SIGBUS handler: %w", err)
	}
	guardOldBUSHandler = oldBUS.handler
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
