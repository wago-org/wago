//go:build linux && amd64 && wago_guardpage

package runtime

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

// Guard-page trap handler (EXPERIMENTAL). When linear memory is backed by a
// PROT_NONE reservation (NewJobMemoryGuarded) and the JIT omits bounds checks
// (amd64.ElideBoundsChecks), an out-of-range access faults with SIGSEGV/SIGBUS.
// We install our own handler (pure asm, no cgo) that derives everything it needs
// from the FAULTING THREAD's own state — so there is no per-call shared state and
// guarded calls run fully in parallel:
//
//   - The fault address is classified against a registry of live reservations
//     (guardRegions). A fault outside every reservation chains to Go's saved
//     handler, so genuine Go faults still crash/panic.
//   - For a fault inside a reservation, the handler reads the wasm frame's saved
//     linMem ([RBP-16]) and trap pointer ([RBP-24]) — wago's ABI stores both
//     there. It only acts if [RBP-16] matches that reservation's linMem base,
//     which rejects the astronomically-unlikely case of a wild non-wasm pointer
//     landing inside a live reservation.
//   - It then writes TrapLinMemOutOfBounds to the frame's *trap and rewrites only
//     the saved RIP to nativeTrapExit (a `leave; ret`), unwinding one wasm frame
//     into wago's existing post-call trap-propagation path back to Call.
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
	guardRegions    [maxGuardRegions]guardRegion // scanned locklessly by the handler
	guardRegionMu   sync.Mutex                   // serialises registry mutation only
	guardTrapExitPC uintptr                      // entry of nativeTrapExit (set at install)
	guardOldHandler uintptr                      // Go's previous SIGSEGV/SIGBUS handler

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

// nativeTrapExit is the `leave; ret` longjmp landing pad. Never called from Go.
func nativeTrapExit()

// asm symbol-address getters (raw ABI0 entry points for the kernel/sigaction).
func addrGuardSigHandler() uintptr
func addrGuardSigRestorer() uintptr
func addrNativeTrapExit() uintptr

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
	guardTrapExitPC = addrNativeTrapExit()
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
func (e *Engine) CallGuarded(code uintptr, serArgs, linMem, trap, results []byte, j *JobMemory) error {
	if j.reserveBase == 0 {
		return fmt.Errorf("CallGuarded requires NewJobMemoryGuarded")
	}
	enterNative(code, slicePtr(serArgs), slicePtr(linMem), slicePtr(trap), slicePtr(results), e.stackTop)
	if len(trap) >= 4 {
		if tc := TrapCode(uint32(trap[0]) | uint32(trap[1])<<8 | uint32(trap[2])<<16 | uint32(trap[3])<<24); tc != TrapNone {
			return &TrapError{Code: tc}
		}
	}
	return nil
}
