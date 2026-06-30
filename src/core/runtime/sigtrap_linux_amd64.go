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
// We install our own handler (pure asm, no cgo) that:
//   - checks the fault address is inside the linear-memory reservation; if not,
//     it chains to Go's saved handler so genuine Go faults still crash/panic;
//   - otherwise writes TrapLinMemOutOfBounds to the call's *trap buffer and
//     rewrites only the signal's saved RIP to nativeTrapExit (a `leave; ret`
//     stub). On return it runs that stub on the faulting frame, unwinding one
//     wasm frame into wago's existing post-call trap-propagation path — which
//     carries the trap up through any nesting back to Call, exactly like an
//     explicit-check trap.
//
// This mirrors WARP's memorySignalHandler (PC/addr range check + ucontext
// rewrite) but in a Go asm stub installed via raw rt_sigaction.
//
// Globals are read by the asm handler. They describe the single in-flight
// guarded Call; CallGuarded serialises calls with a mutex, so the spike is
// single-threaded. A production version would key these off the faulting
// thread (e.g. via the ucontext / per-M state) instead.
var (
	guardReserveStart uintptr // [start,end) of the linear-memory reservation
	guardReserveEnd   uintptr
	guardTrapPtr      uintptr // the Call's *trap buffer (u32 written on fault)
	guardTrapExitPC   uintptr // entry of nativeTrapExit (leave;ret stub)
	guardOldHandler   uintptr // Go's previous SIGSEGV/SIGBUS handler, for chaining

	guardMu        sync.Mutex
	guardInstalled bool
)

// nativeTrapExit is the trampoline epilogue, used as the signal handler's
// longjmp landing pad. Never called directly from Go — only resumed into.
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

// CallGuarded runs guard-page-mode native code: it points the trap handler at
// this call's reservation, save area, and trap buffer, then enters native code.
// An out-of-bounds access faults into the handler and surfaces as a *TrapError.
// Serialised (see the guard globals).
func (e *Engine) CallGuarded(code uintptr, serArgs, linMem, trap, results []byte, j *JobMemory) error {
	base, length := j.ReserveRange()
	if base == 0 {
		return fmt.Errorf("CallGuarded requires NewJobMemoryGuarded")
	}
	guardMu.Lock()
	defer guardMu.Unlock()
	guardReserveStart = base
	guardReserveEnd = base + length
	guardTrapPtr = slicePtr(trap)
	enterNative(code, slicePtr(serArgs), slicePtr(linMem), slicePtr(trap), slicePtr(results), e.stackTop)
	if len(trap) >= 4 {
		if tc := TrapCode(uint32(trap[0]) | uint32(trap[1])<<8 | uint32(trap[2])<<16 | uint32(trap[3])<<24); tc != TrapNone {
			return &TrapError{Code: tc}
		}
	}
	return nil
}
