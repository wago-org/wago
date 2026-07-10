//go:build darwin && arm64 && wago_guardpage

package runtime

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
	_ "unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// The assembly handler hardcodes these basedata/trap values. Compile-time
// assertions force the Go and assembly definitions to move together.
const (
	_ = uint(abi.TrapCellPtrOffset - 104)
	_ = uint(104 - abi.TrapCellPtrOffset)
	_ = uint(unsafe.Sizeof(darwinSigaction{}) - 16)
	_ = uint(16 - unsafe.Sizeof(darwinSigaction{}))
	_ = uint(TrapLinMemOutOfBounds - 3)
	_ = uint(3 - TrapLinMemOutOfBounds)
	_ = uint(TrapLinMemCouldNotExtend - 4)
	_ = uint(4 - TrapLinMemCouldNotExtend)
)

// darwinSigaction is Darwin's user-facing 16-byte struct sigaction. libSystem
// supplies its private signal trampoline when sigaction installs this form.
type darwinSigaction struct {
	Handler uintptr
	Mask    uint32
	Flags   int32
}

const (
	darwinSASigInfo = 0x40
	darwinSAOnStack = 0x1
	darwinSARestart = 0x2
)

var (
	guardTrapExitHandlerJumpPC uintptr
	guardOldSEGVHandler        uintptr
	guardOldBUSHandler         uintptr

	guardMu        sync.Mutex
	guardInstalled bool
)

// installDarwinSignalHandlers installs one process-wide handler for the two
// synchronous memory-fault signals. Non-wasm faults tail-chain to the saved
// Go/runtime handlers. A default or ignored prior disposition cannot be safely
// tail-called, so reject it rather than turning an unrelated fault into a loop.
func installDarwinSignalHandlers() error {
	act := darwinSigaction{
		Handler: addrGuardSigHandler(),
		Mask:    ^uint32(0),
		Flags:   darwinSASigInfo | darwinSAOnStack | darwinSARestart,
	}

	var oldSEGV darwinSigaction
	if errno := sigaction(syscall.SIGSEGV, nil, &oldSEGV); errno != 0 {
		return fmt.Errorf("read SIGSEGV handler: %w", errno)
	}
	if oldSEGV.Handler <= 1 {
		return fmt.Errorf("install SIGSEGV handler: previous disposition %#x is not chainable", oldSEGV.Handler)
	}
	var oldBUS darwinSigaction
	if errno := sigaction(syscall.SIGBUS, nil, &oldBUS); errno != 0 {
		return fmt.Errorf("read SIGBUS handler: %w", errno)
	}
	if oldBUS.Handler <= 1 {
		return fmt.Errorf("install SIGBUS handler: previous disposition %#x is not chainable", oldBUS.Handler)
	}

	// Publish both chain targets before either replacement becomes live. This
	// closes the installation window in which an unrelated fault could reach our
	// handler before its predecessor pointer was available.
	guardOldSEGVHandler = oldSEGV.Handler
	guardOldBUSHandler = oldBUS.Handler
	if errno := sigaction(syscall.SIGSEGV, &act, nil); errno != 0 {
		guardOldSEGVHandler = 0
		guardOldBUSHandler = 0
		return fmt.Errorf("install SIGSEGV handler: %w", errno)
	}
	if errno := sigaction(syscall.SIGBUS, &act, nil); errno != 0 {
		rollback := sigaction(syscall.SIGSEGV, &oldSEGV, nil)
		guardOldSEGVHandler = 0
		guardOldBUSHandler = 0
		if rollback != 0 {
			return fmt.Errorf("install SIGBUS handler: %w (restore SIGSEGV: %v)", errno, rollback)
		}
		return fmt.Errorf("install SIGBUS handler: %w", errno)
	}
	return nil
}

// Assembly entry points. The handler runs under the Darwin C signal ABI and
// must not call Go; the landing pad resumes enterNative's saved continuation.
func guardSigHandler()
func nativeTrapExitHandlerJump()
func addrGuardSigHandler() uintptr
func addrNativeTrapExitHandlerJump() uintptr

func sigaction(sig syscall.Signal, act, old *darwinSigaction) syscall.Errno {
	_, _, errno := syscall6(addrLibcSigactionTrampoline(), uintptr(sig), uintptr(unsafe.Pointer(act)), uintptr(unsafe.Pointer(old)), 0, 0, 0)
	return errno
}

//go:linkname syscall6 syscall.syscall6
func syscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)

func libcSigactionTrampoline()
func addrLibcSigactionTrampoline() uintptr

//go:cgo_import_dynamic libc_sigaction sigaction "/usr/lib/libSystem.B.dylib"
