//go:build linux && (amd64 || arm64) && !tinygo

package runtime

import (
	"fmt"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	maxInterruptActivations = 64
	maxExecutableCodeRanges = 4096
	interruptDeadlineRetry  = 50 * time.Microsecond

	interruptOutside   = uint32(0)
	interruptRuntime   = uint32(1)
	interruptWasm      = uint32(2)
	interruptUnwinding = uint32(3)
)

// interruptActivation is a signal-safe, fixed-address per-thread activation.
// The handler reads this table directly; fields and offsets are mirrored in
// interrupt_linux_amd64.s.
type interruptActivation struct {
	state  uint32
	tid    uint32
	trap   uintptr
	ack    uint32
	timer  int32
	armed  uint32
	_      uint32
	linMem uintptr
}

type executableCodeRange struct {
	start uintptr
	end   uintptr
}

var (
	interruptActivations     [maxInterruptActivations]interruptActivation
	executableCodeRanges     [maxExecutableCodeRanges]executableCodeRange
	executableCodeRangeLimit uint32
	executableCodeMu         sync.Mutex

	interruptDeadlines  = make(map[uintptr]int64)
	interruptDeadlineMu sync.Mutex

	interruptInstallOnce sync.Once
	interruptInstallErr  error
	interruptSignal      uint32
	interruptTrapPC      uintptr
	interruptOldHandler  uintptr
)

const (
	_ = uint(unsafe.Sizeof(interruptActivation{}) - 40)
	_ = uint(40 - unsafe.Sizeof(interruptActivation{}))
	_ = uint(unsafe.Offsetof(interruptActivation{}.ack) - 16)
	_ = uint(16 - unsafe.Offsetof(interruptActivation{}.ack))
	_ = uint(unsafe.Offsetof(interruptActivation{}.linMem) - 32)
	_ = uint(32 - unsafe.Offsetof(interruptActivation{}.linMem))
	_ = uint(unsafe.Sizeof(executableCodeRange{}) - 16)
	_ = uint(16 - unsafe.Sizeof(executableCodeRange{}))
	_ = uint(unsafe.Sizeof(interruptSigevent{}) - 64)
	_ = uint(64 - unsafe.Sizeof(interruptSigevent{}))
)

// Linux's kernel real-time signal range is 32..64 and glibc reserves the bottom
// two. Signal 40 is Wago's process-wide reserved host-interrupt signal. Go
// preinstalls its dispatcher for the full range, so installation preserves that
// handler and the asm path chains non-tgkill deliveries to it.
const interruptReservedSignal = 40

type interruptSigaction struct {
	handler  uintptr
	flags    uint64
	restorer uintptr
	mask     uint64
}

const (
	interruptSA_SIGINFO = 0x00000004
	interruptSA_ONSTACK = 0x08000000
)

func interruptRTSigaction(sig uintptr, act, old *interruptSigaction) error {
	_, _, errno := syscall.RawSyscall6(syscall.SYS_RT_SIGACTION, sig,
		uintptr(unsafe.Pointer(act)), uintptr(unsafe.Pointer(old)), 8, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func installInterruptHandler() {
	interruptTrapPC = addrNativeInterruptTrap()
	sig := uintptr(interruptReservedSignal)
	var old interruptSigaction
	if err := interruptRTSigaction(sig, nil, &old); err != nil {
		interruptInstallErr = fmt.Errorf("inspect reserved real-time signal %d: %w", sig, err)
		return
	}
	interruptOldHandler = old.handler
	act := interruptSigaction{
		handler: addrInterruptSigHandler(),
		flags:   interruptSA_SIGINFO | interruptSA_ONSTACK,
	}
	configureInterruptSigaction(&act)
	if err := interruptRTSigaction(sig, &act, nil); err != nil {
		interruptInstallErr = fmt.Errorf("install reserved real-time signal %d: %w", sig, err)
		return
	}
	atomic.StoreUint32(&interruptSignal, uint32(sig))
}

func beginInterruptActivation(trap []byte) (*interruptActivation, error) {
	interruptInstallOnce.Do(installInterruptHandler)
	if interruptInstallErr != nil {
		return nil, fmt.Errorf("jit host interrupt: %w", interruptInstallErr)
	}
	if len(trap) < 4 {
		return nil, fmt.Errorf("jit host interrupt: trap cell requires at least 4 bytes")
	}

	goruntime.LockOSThread()
	tid := uint32(syscall.Gettid())
	trapPtr := slicePtr(trap)
	for i := range interruptActivations {
		a := &interruptActivations[i]
		if atomic.CompareAndSwapUint32(&a.state, interruptOutside, interruptRuntime) {
			atomic.StoreUint32(&a.tid, tid)
			atomic.StoreUintptr(&a.trap, trapPtr)
			atomic.StoreUintptr(&a.linMem, 0)
			atomic.StoreUint32(&a.ack, 0)
			atomic.StoreUint32(&a.armed, 0)
			if err := armActivationDeadline(a); err != nil {
				atomic.StoreUintptr(&a.trap, 0)
				atomic.StoreUint32(&a.tid, 0)
				atomic.StoreUint32(&a.state, interruptOutside)
				goruntime.UnlockOSThread()
				return nil, err
			}
			return a, nil
		}
	}
	goruntime.UnlockOSThread()
	return nil, fmt.Errorf("jit host interrupt: activation table full (%d)", maxInterruptActivations)
}

func (a *interruptActivation) enterWasm(linMem uintptr) {
	if a != nil {
		atomic.StoreUintptr(&a.linMem, linMem)
		atomic.StoreUint32(&a.state, interruptWasm)
	}
}

func (a *interruptActivation) leaveWasm() {
	if a != nil {
		atomic.StoreUint32(&a.state, interruptRuntime)
		atomic.StoreUintptr(&a.linMem, 0)
	}
}

func (a *interruptActivation) close() {
	if a == nil {
		return
	}
	atomic.StoreUint32(&a.state, interruptRuntime)
	if atomic.SwapUint32(&a.armed, 0) != 0 {
		_, _, _ = syscall.RawSyscall(syscall.SYS_TIMER_DELETE, uintptr(uint32(a.timer)), 0, 0)
	}
	atomic.StoreUintptr(&a.trap, 0)
	atomic.StoreUintptr(&a.linMem, 0)
	atomic.StoreUint32(&a.tid, 0)
	atomic.StoreUint32(&a.ack, 0)
	atomic.StoreUint32(&a.state, interruptOutside)
	goruntime.UnlockOSThread()
}

type interruptSigevent struct {
	value  uint64
	signo  int32
	notify int32
	tid    int32
	_      [44]byte
}

type interruptTimespec struct {
	sec  int64
	nsec int64
}

type interruptItimerspec struct {
	interval interruptTimespec
	value    interruptTimespec
}

func armActivationDeadline(a *interruptActivation) error {
	trapPtr := atomic.LoadUintptr(&a.trap)
	interruptDeadlineMu.Lock()
	unixNano := interruptDeadlines[trapPtr]
	interruptDeadlineMu.Unlock()
	if unixNano == 0 {
		return nil
	}
	delay := time.Until(time.Unix(0, unixNano))
	if delay <= 0 {
		delay = time.Nanosecond
	}
	event := interruptSigevent{
		signo:  int32(atomic.LoadUint32(&interruptSignal)),
		notify: 4, // SIGEV_THREAD_ID
		tid:    int32(atomic.LoadUint32(&a.tid)),
	}
	var timerID int32
	_, _, errno := syscall.RawSyscall(syscall.SYS_TIMER_CREATE, 1, /*CLOCK_MONOTONIC*/
		uintptr(unsafe.Pointer(&event)), uintptr(unsafe.Pointer(&timerID)))
	if errno != 0 {
		return fmt.Errorf("jit host interrupt: timer_create: %w", errno)
	}
	spec := interruptItimerspec{
		interval: interruptTimespec{
			sec:  int64(interruptDeadlineRetry / time.Second),
			nsec: int64(interruptDeadlineRetry % time.Second),
		},
		value: interruptTimespec{sec: int64(delay / time.Second), nsec: int64(delay % time.Second)},
	}
	_, _, errno = syscall.RawSyscall6(syscall.SYS_TIMER_SETTIME, uintptr(uint32(timerID)), 0,
		uintptr(unsafe.Pointer(&spec)), 0, 0, 0)
	if errno != 0 {
		_, _, _ = syscall.RawSyscall(syscall.SYS_TIMER_DELETE, uintptr(uint32(timerID)), 0, 0)
		return fmt.Errorf("jit host interrupt: timer_settime: %w", errno)
	}
	a.timer = timerID
	atomic.StoreUint32(&a.armed, 1)
	return nil
}

// SetInterruptDeadline associates a context deadline with the next activation
// using trap. beginInterruptActivation converts it to a per-thread kernel timer.
func SetInterruptDeadline(trap []byte, deadline time.Time) func() {
	if len(trap) < 4 || deadline.IsZero() {
		return func() {}
	}
	trapPtr := slicePtr(trap)
	interruptDeadlineMu.Lock()
	interruptDeadlines[trapPtr] = deadline.UnixNano()
	interruptDeadlineMu.Unlock()
	return func() {
		interruptDeadlineMu.Lock()
		delete(interruptDeadlines, trapPtr)
		interruptDeadlineMu.Unlock()
	}
}

func registerExecutableCode(mem []byte) error {
	if len(mem) == 0 {
		return fmt.Errorf("register executable code: empty mapping")
	}
	start := slicePtr(mem)
	end := start + uintptr(len(mem))
	executableCodeMu.Lock()
	defer executableCodeMu.Unlock()
	for i := range executableCodeRanges {
		r := &executableCodeRanges[i]
		if atomic.LoadUintptr(&r.start) == 0 {
			atomic.StoreUintptr(&r.end, end)
			atomic.StoreUintptr(&r.start, start) // publish last
			limit := uint32(i + 1)
			if limit > atomic.LoadUint32(&executableCodeRangeLimit) {
				atomic.StoreUint32(&executableCodeRangeLimit, limit)
			}
			return nil
		}
	}
	return fmt.Errorf("executable code range table full (%d)", maxExecutableCodeRanges)
}

func unregisterExecutableCode(mem []byte) {
	if len(mem) == 0 {
		return
	}
	start := slicePtr(mem)
	executableCodeMu.Lock()
	defer executableCodeMu.Unlock()
	for i := range executableCodeRanges {
		r := &executableCodeRanges[i]
		if atomic.LoadUintptr(&r.start) == start {
			atomic.StoreUintptr(&r.start, 0) // disable before unmapping
			atomic.StoreUintptr(&r.end, 0)
			limit := int(atomic.LoadUint32(&executableCodeRangeLimit))
			for limit > 0 && atomic.LoadUintptr(&executableCodeRanges[limit-1].start) == 0 {
				limit--
			}
			atomic.StoreUint32(&executableCodeRangeLimit, uint32(limit))
			return
		}
	}
}

// RequestInterrupt publishes the ordinary interruption trap and directs the
// reserved real-time signal to the exact OS thread currently owning that cell.
// The request performs a bounded retry; lifecycle and context callbacks use
// RequestInterruptAsync when they must span a host park.
func RequestInterrupt(trap []byte) {
	storeTrap(trap, uint32(TrapInterrupted))
	if len(trap) < 4 {
		return
	}
	requestInterruptPointer(slicePtr(trap))
}

// requestInterruptPointer signals only while trapPtr is still owned by a
// published activation. It never dereferences the trap address, so an async
// Close retry cannot race arena release after activation teardown.
func requestInterruptPointer(trapPtr uintptr) bool {
	sig := atomic.LoadUint32(&interruptSignal)
	if sig == 0 {
		return false
	}
	pid := syscall.Getpid()
	// A signal can be delivered in the few runtime instructions around native
	// entry/exit or while a host import is parked. The handler clears ack in that
	// case. Retry for a bounded window so Close closes ordinary entry/exit races
	// without waiting indefinitely for a stuck host function.
	for attempt := 0; attempt < 256; attempt++ {
		found := false
		for i := range interruptActivations {
			a := &interruptActivations[i]
			if atomic.LoadUintptr(&a.trap) != trapPtr {
				continue
			}
			found = true
			state := atomic.LoadUint32(&a.state)
			if state == interruptRuntime || state == interruptOutside {
				return true
			}
			if atomic.LoadUint32(&a.ack) == 1 {
				return true
			}
			tid := atomic.LoadUint32(&a.tid)
			if tid != 0 && atomic.CompareAndSwapUint32(&a.ack, 0, 2) {
				_, _, errno := syscall.RawSyscall(syscall.SYS_TGKILL, uintptr(pid), uintptr(tid), uintptr(sig))
				if errno != 0 {
					atomic.StoreUint32(&a.ack, 0)
				}
			}
			break
		}
		if !found {
			return false
		}
		goruntime.Gosched()
	}
	// A delivery can be lost to a concurrent signal-disposition/mask transition.
	// Do not strand the activation in "sent"; the outer cancellation retry (or a
	// later Close request) must be able to send again.
	for i := range interruptActivations {
		a := &interruptActivations[i]
		if atomic.LoadUintptr(&a.trap) == trapPtr {
			atomic.CompareAndSwapUint32(&a.ack, 2, 0)
			return true
		}
	}
	return false
}

// RequestInterruptAsync keeps retrying after a non-blocking lifecycle request,
// including across an arbitrarily long host park. The goroutine exists only
// after interruption is requested and exits when the activation acknowledges
// or releases its slot.
func RequestInterruptAsync(trap []byte) {
	storeTrap(trap, uint32(TrapInterrupted))
	if len(trap) < 4 {
		return
	}
	trapPtr := slicePtr(trap)
	go func() {
		for {
			if !requestInterruptPointer(trapPtr) {
				return
			}
			for i := range interruptActivations {
				a := &interruptActivations[i]
				if atomic.LoadUintptr(&a.trap) != trapPtr {
					continue
				}
				if atomic.LoadUint32(&a.ack) == 1 {
					return
				}
				break
			}
			time.Sleep(50 * time.Microsecond)
		}
	}()
}

func HostInterruptSupported() bool { return true }

func interruptSigHandler()
func nativeInterruptTrap()
func addrInterruptSigHandler() uintptr
func addrNativeInterruptTrap() uintptr
