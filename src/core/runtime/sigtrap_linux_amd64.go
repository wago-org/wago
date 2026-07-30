//go:build linux && amd64 && wago_guardpage

package runtime

import (
	"fmt"
	"sync"
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
//   - For a fault inside a reservation, the handler recognizes either wasm ABI:
//     amd64 frameless frames keep linMem in RBX and the trap pointer at [RSP+0];
//     framed amd64 frames keep linMem at [RBP-16] and trap at [RBP-24]. It only
//     acts if the frame's linMem matches that reservation's linMem base, which
//     rejects the astronomically-unlikely case of a wild non-wasm pointer landing
//     inside a live reservation.
//   - It then writes TrapLinMemOutOfBounds to the frame's *trap and rewrites only
//     the saved RIP to the ABI-specific trap exit: amd64 restores the trampoline's
//     handler-jump re-entry SP and returns straight to enterNative; framed amd64
//     performs the old one-frame `leave; ret` unwind.
//
// This mirrors WARP's memorySignalHandler (addr/state check + ucontext rewrite)
// in a Go asm stub installed via raw rt_sigaction.

// guardRegion describes one live guarded reservation. Layout is read directly by
// the asm handler: start@0, end@8, linMem@16, size 32 bytes. A zero start means
// the slot is free.
type guardRegion struct {
	start  uintptr
	end    uintptr
	linMem uintptr
	_      uintptr // pad to 32 bytes for asm indexing
}

const maxGuardRegions = 256

var (
	guardRegions               [maxGuardRegions]guardRegion // scanned locklessly by the handler
	guardRegionMu              sync.Mutex                   // serialises registry mutation only
	guardTrapExitFramedPC      uintptr                      // entry of nativeTrapExitFramed
	guardTrapExitHandlerJumpPC uintptr                      // entry of nativeTrapExitHandlerJump
	guardOldHandler            uintptr                      // Go's previous SIGSEGV/SIGBUS handler

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
			guardRegions[i].end = end
			guardRegions[i].start = start // enable last
			return nil
		}
	}
	return fmt.Errorf("guard region table full (%d)", maxGuardRegions)
}

// unregisterGuardRegion frees a reservation's slot. start is cleared first so the
// handler immediately stops matching it.
func unregisterGuardRegion(start uintptr) {
	guardRegionMu.Lock()
	defer guardRegionMu.Unlock()
	for i := range guardRegions {
		if guardRegions[i].start == start {
			guardRegions[i].start = 0 // disable first
			guardRegions[i].end = 0
			guardRegions[i].linMem = 0
			return
		}
	}
}

func init() { guardCloseHook = unregisterGuardRegion }

// nativeTrapExitFramed and nativeTrapExitHandlerJump are signal-rewrite landing
// pads. Never called from Go.
func nativeTrapExitFramed()
func nativeTrapExitHandlerJump()

// asm symbol-address getters (raw ABI0 entry points for the kernel/sigaction).
func addrGuardSigHandler() uintptr
func addrGuardSigRestorer() uintptr
func addrNativeTrapExitFramed() uintptr
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
	_SA_SIGINFO  = 0x00000004
	_SA_RESTORER = 0x04000000
	_SA_ONSTACK  = 0x08000000
)

func rtSigaction(sig uintptr, act, old *kernelSigaction) error {
	_, _, errno := syscall.Syscall6(syscall.SYS_RT_SIGACTION, sig,
		uintptr(unsafe.Pointer(act)), uintptr(unsafe.Pointer(old)), 8 /*sigsetsize*/, 0, 0)
	if errno != 0 {
		return errno
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
	guardTrapExitFramedPC = addrNativeTrapExitFramed()
	guardTrapExitHandlerJumpPC = addrNativeTrapExitHandlerJump()
	act := kernelSigaction{
		handler:  addrGuardSigHandler(),
		flags:    _SA_SIGINFO | _SA_ONSTACK | _SA_RESTORER,
		restorer: addrGuardSigRestorer(),
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

// CallGuarded runs guard-page-mode native code. An out-of-bounds access faults
// into the handler and surfaces as a *TrapError. Thread-safe: all per-fault state
// is derived from the faulting frame + the reservation registry, so concurrent
// guarded calls (each with its own engine + guarded memory) run in parallel.
func (e *Engine) CallGuarded(code uintptr, serArgs []byte, linMemBase uintptr, trap, results []byte, j *JobMemory) error {
	if j.reserveBase == 0 || linMemBase == 0 {
		return fmt.Errorf("CallGuarded requires NewJobMemoryGuarded")
	}
	if len(trap) >= 4 {
		clearTrapUnlessInterrupted(trap)
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
