//go:build linux && amd64 && wago_guardpage

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

// The asm handler (sigtrap_amd64.s, dotrap_x64) hardcodes the trap-cell-pointer
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

// Guard-page trap handler (EXPERIMENTAL). When linear memory is backed by a
// PROT_NONE reservation (NewJobMemoryGuarded) and the JIT omits bounds checks,
// an out-of-range access faults with SIGSEGV/SIGBUS. We install our own handler
// (pure asm, no cgo) that derives everything it needs from the FAULTING THREAD's
// own state — so there is no per-call shared state and guarded calls run fully in
// parallel:
//
//   - The fault address is classified against a registry of live reservations
//     (guardRegions). A fault outside every reservation chains to Go's saved
//     handler, so genuine Go faults still crash/panic.
//   - For a fault inside a reservation, the handler requires the x64/WARP ABI's
//     pinned RBX linMem to match the reservation owner before dereferencing
//     basedata. A non-Wasm fault that happens to land inside a reservation chains
//     without treating an arbitrary RBP as a legacy frame pointer.
//   - It then writes TrapLinMemOutOfBounds, or TrapLinMemCouldNotExtend when a
//     lazy page commit fails, to the frame's *trap and rewrites only the saved RIP
//     to the ABI-specific trap exit: amd64 restores the trampoline's
//     handler-jump re-entry SP and returns straight to enterNative.
//
// This mirrors WARP's memorySignalHandler (addr/state check + ucontext rewrite)
// in a Go asm stub installed via raw rt_sigaction.

// guardRegion describes one live guarded reservation. Layout is read directly by
// the asm handler: start@0, end@8, linMem@16, ownerLinMem@24, size 32 bytes.
// A zero start means the slot is free.
type guardRegion struct {
	start       uintptr
	end         uintptr
	linMem      uintptr
	ownerLinMem uintptr
}

const maxGuardRegions = 256

var (
	guardRegions               [maxGuardRegions]guardRegion // scanned locklessly by the handler
	guardRegionMu              sync.Mutex                   // serialises registry mutation only
	guardTrapExitHandlerJumpPC uintptr                      // entry of nativeTrapExitHandlerJump
	guardOldSEGVHandler        uintptr                      // previous SIGSEGV handler
	guardOldBUSHandler         uintptr                      // previous SIGBUS handler

	guardMu        sync.Mutex
	guardInstalled bool
)

// registerGuardRegion adds a reservation to the table the handler scans. start is
// written last so the asm side never sees a half-initialised entry (x86 TSO).
func registerGuardRegion(start, end, linMem uintptr) error {
	guardRegionMu.Lock()
	defer guardRegionMu.Unlock()
	for i := range guardRegions {
		if guardRegions[i].start == 0 {
			guardRegions[i].linMem = linMem
			guardRegions[i].ownerLinMem = linMem
			guardRegions[i].end = end
			guardRegions[i].start = start // enable last
			return nil
		}
	}
	return fmt.Errorf("guard region table full (%d)", maxGuardRegions)
}

// unregisterGuardRegion frees a reservation's slot. start is cleared first so the
// handler immediately stops matching it.
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
			guardRegions[i].start = 0 // disable first
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

// nativeTrapExitHandlerJump is a signal-rewrite landing pad. Never called from Go.
func nativeTrapExitHandlerJump()

// asm symbol-address getters (raw ABI0 entry points for the kernel/sigaction).
func addrGuardSigHandler() uintptr
func addrGuardSigRestorer() uintptr
func addrNativeTrapExitHandlerJump() uintptr

// guardSigHandler / guardSigRestorer are implemented in sigtrap_amd64.s and are
// only ever invoked by the kernel as a signal handler / restorer.
func guardSigHandler()
func guardSigRestorer()

// kernelSigaction is the raw struct sigaction the kernel's rt_sigaction expects
// (not glibc's): handler, flags, restorer, mask.
type kernelSigaction struct {
	handler  uintptr
	flags    uint64
	restorer uintptr
	mask     uint64
}

const (
	_SA_SIGINFO        = 0x00000004
	_SA_EXPOSE_TAGBITS = 0x00000800
	_SA_RESTORER       = 0x04000000
	_SA_ONSTACK        = 0x08000000
	_SA_RESTART        = 0x10000000
	_SA_NODEFER        = 0x40000000
	_SA_RESETHAND      = 0x80000000
)

func rtSigaction(sig uintptr, act, old *kernelSigaction) error {
	_, _, errno := syscall.Syscall6(syscall.SYS_RT_SIGACTION, sig,
		uintptr(unsafe.Pointer(act)), uintptr(unsafe.Pointer(old)), 8 /*sigsetsize*/, 0, 0)
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

	// Publish both predecessors before either replacement can receive a fault.
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

// InstallGuardTrapHandler installs the guard-page SIGSEGV/SIGBUS handler
// (idempotent). Call once before any CallGuarded.
func InstallGuardTrapHandler() error {
	guardMu.Lock()
	defer guardMu.Unlock()
	if guardInstalled {
		return nil
	}
	guardTrapExitHandlerJumpPC = addrNativeTrapExitHandlerJump()
	act := kernelSigaction{
		handler:  addrGuardSigHandler(),
		flags:    _SA_SIGINFO | _SA_ONSTACK | _SA_RESTORER,
		restorer: addrGuardSigRestorer(),
	}
	if err := installLinuxSignalHandlers(&act, rtSigaction); err != nil {
		return err
	}
	guardInstalled = true
	return nil
}

// CallGuarded runs guard-page-mode native code. An out-of-bounds access faults
// into the handler and surfaces as a *TrapError. Thread-safe: all per-fault state
// is derived from the faulting frame + the reservation registry, so concurrent
// guarded calls (each with its own engine + guarded memory) run in parallel.
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
