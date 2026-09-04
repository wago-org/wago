//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package runtime

import (
	"fmt"
	"os"
	goruntime "runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	maxInterruptRequests       = 64
	maxExecutableCodeRanges    = 4096
	maxInterruptLinearMemories = 4096
	interruptDeadlineRetry     = 50 * time.Microsecond
)

// interruptRequest is published only while a cold interruption request is
// broadcasting. Generated Wasm keeps its linear-memory base in a fixed
// register, so the signal handler obtains the active trap cell directly from
// the saved CPU context and needs no per-invocation activation.
type interruptRequest struct {
	trap uintptr
	ack  uint32
	refs uint32
}

type executableCodeRange struct {
	start uintptr
	end   uintptr
}

var (
	interruptRequests          [maxInterruptRequests]interruptRequest
	executableCodeRanges       [maxExecutableCodeRanges]executableCodeRange
	executableCodeRangeLimit   uint32
	executableCodeMu           sync.Mutex
	interruptLinearMemories    [maxInterruptLinearMemories]uintptr
	interruptLinearMemoryLimit uint32
	interruptLinearMemoryState uint32
	interruptLinearMemoryCount uint32
	interruptLinearMemoryPeak  uint32
	interruptLinearMemoryCache uint32
	interruptLinearMemoryMu    sync.Mutex

	interruptRequestMu   sync.Mutex
	interruptInstallOnce sync.Once
	interruptInstallErr  error
	interruptSignal      uint32
)

func init() {
	interruptLinearMemoryRegister = registerInterruptLinearMemory
	interruptLinearMemoryUnregister = unregisterInterruptLinearMemory
	interruptLinearMemoryCacheChange = changeInterruptLinearMemoryCacheLinux
	nativeMemoryStatsSnapshot = processNativeMemoryStatsLinux
}

func changeInterruptLinearMemoryCacheLinux(delta int32) {
	atomic.AddUint32(&interruptLinearMemoryCache, uint32(delta))
}

func processNativeMemoryStatsLinux() NativeMemoryStats {
	registered := atomic.LoadUint32(&interruptLinearMemoryCount)
	cached := atomic.LoadUint32(&interruptLinearMemoryCache)
	active := uint32(0)
	if cached <= registered {
		active = registered - cached
	}
	return NativeMemoryStats{
		Supported:      true,
		Active:         active,
		Cached:         cached,
		Registered:     registered,
		PeakRegistered: atomic.LoadUint32(&interruptLinearMemoryPeak),
		Capacity:       maxInterruptLinearMemories,
		ScanSpan:       atomic.LoadUint32(&interruptLinearMemoryLimit),
	}
}

func registerInterruptLinearMemory(linMem uintptr) error {
	if linMem == 0 {
		return fmt.Errorf("register interrupt linear memory: zero base")
	}
	interruptLinearMemoryMu.Lock()
	defer interruptLinearMemoryMu.Unlock()
	limit := int(atomic.LoadUint32(&interruptLinearMemoryLimit))
	firstHole := -1
	for i := 0; i < limit; i++ {
		registered := atomic.LoadUintptr(&interruptLinearMemories[i])
		if registered == linMem {
			return nil
		}
		if registered == 0 && firstHole < 0 {
			firstHole = i
		}
	}
	if firstHole < 0 {
		if limit == len(interruptLinearMemories) {
			return &ResourceLimitError{
				Resource:   "native memory mappings",
				Scope:      "process",
				Used:       uint64(atomic.LoadUint32(&interruptLinearMemoryCount)),
				Requested:  1,
				Limit:      maxInterruptLinearMemories,
				Suggestion: "close unused Instance or Memory values, inspect ProcessNativeMemoryStats, or use another process",
			}
		}
		firstHole = limit
	}
	atomic.StoreUintptr(&interruptLinearMemories[firstHole], linMem)
	count := atomic.AddUint32(&interruptLinearMemoryCount, 1)
	for peak := atomic.LoadUint32(&interruptLinearMemoryPeak); count > peak; peak = atomic.LoadUint32(&interruptLinearMemoryPeak) {
		if atomic.CompareAndSwapUint32(&interruptLinearMemoryPeak, peak, count) {
			break
		}
	}
	if firstHole == limit {
		atomic.StoreUint32(&interruptLinearMemoryLimit, uint32(limit+1))
	}
	return nil
}

func unregisterInterruptLinearMemory(linMem uintptr) {
	interruptLinearMemoryMu.Lock()
	defer interruptLinearMemoryMu.Unlock()
	// Set the writer gate before clearing entries. Signal readers that entered
	// earlier remain counted; later readers observe the gate and do not inspect
	// the registry. Reopening the gate publishes the cleared slots before Close
	// proceeds to unmap the memory.
	for {
		state := atomic.LoadUint32(&interruptLinearMemoryState)
		if atomic.CompareAndSwapUint32(&interruptLinearMemoryState, state, state|interruptLinearMemoryWriter) {
			break
		}
	}
	limit := int(atomic.LoadUint32(&interruptLinearMemoryLimit))
	removed := uint32(0)
	for i := 0; i < limit; i++ {
		if atomic.LoadUintptr(&interruptLinearMemories[i]) != linMem {
			continue
		}
		atomic.StoreUintptr(&interruptLinearMemories[i], 0)
		removed++
	}
	if removed != 0 {
		atomic.AddUint32(&interruptLinearMemoryCount, ^uint32(removed-1))
	}
	for limit > 0 && atomic.LoadUintptr(&interruptLinearMemories[limit-1]) == 0 {
		limit--
	}
	atomic.StoreUint32(&interruptLinearMemoryLimit, uint32(limit))
	for atomic.LoadUint32(&interruptLinearMemoryState)&interruptLinearMemoryReaders != 0 {
		goruntime.Gosched()
	}
	atomic.StoreUint32(&interruptLinearMemoryState, 0)
}

const (
	interruptLinearMemoryWriter  uint32 = 1 << 31
	interruptLinearMemoryReaders        = interruptLinearMemoryWriter - 1
)

//lint:ignore U1000 referenced from interrupt_linux_{amd64,arm64}.s
var interruptTrapPC uintptr

//lint:ignore U1000 referenced from interrupt_linux_{amd64,arm64}.s
var interruptOldHandler uintptr

const (
	_ = uint(unsafe.Sizeof(interruptRequest{}) - 16)
	_ = uint(16 - unsafe.Sizeof(interruptRequest{}))
	_ = uint(unsafe.Offsetof(interruptRequest{}.ack) - 8)
	_ = uint(8 - unsafe.Offsetof(interruptRequest{}.ack))
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

func registerExecutableCode(mem []byte) error {
	if len(mem) == 0 {
		return fmt.Errorf("register executable code: empty mapping")
	}
	interruptInstallOnce.Do(installInterruptHandler)
	if interruptInstallErr != nil {
		return fmt.Errorf("jit host interrupt: %w", interruptInstallErr)
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

// RequestInterrupt publishes the ordinary interruption trap, then broadcasts
// Wago's reserved signal to the process threads. Only a thread whose saved PC
// is generated Wasm and whose fixed linear-memory register names this trap cell
// rewrites its CPU context; all other deliveries return immediately.
func RequestInterrupt(trap []byte) {
	storeTrap(trap, uint32(TrapInterrupted))
	if len(trap) < 4 {
		return
	}
	requestInterruptPointer(slicePtr(trap))
}

func requestInterruptPointer(trapPtr uintptr) bool {
	sig := atomic.LoadUint32(&interruptSignal)
	if sig == 0 {
		return false
	}
	request := acquireInterruptRequest(trapPtr)
	if request == nil {
		return false
	}
	atomic.StoreUint32(&request.ack, 0)
	broadcastInterruptSignal(sig)
	for attempt := 0; attempt < 64 && atomic.LoadUint32(&request.ack) == 0; attempt++ {
		goruntime.Gosched()
	}
	acknowledged := atomic.LoadUint32(&request.ack) != 0
	releaseInterruptRequest(request, trapPtr)
	return acknowledged
}

func acquireInterruptRequest(trapPtr uintptr) *interruptRequest {
	interruptRequestMu.Lock()
	defer interruptRequestMu.Unlock()
	start := int((trapPtr >> 4) & (maxInterruptRequests - 1))
	for probe := 0; probe < maxInterruptRequests; probe++ {
		request := &interruptRequests[(start+probe)&(maxInterruptRequests-1)]
		owner := atomic.LoadUintptr(&request.trap)
		if owner == trapPtr || (owner == 0 && atomic.CompareAndSwapUintptr(&request.trap, 0, trapPtr)) {
			atomic.AddUint32(&request.refs, 1)
			return request
		}
	}
	return nil
}

func releaseInterruptRequest(request *interruptRequest, trapPtr uintptr) {
	interruptRequestMu.Lock()
	defer interruptRequestMu.Unlock()
	if atomic.AddUint32(&request.refs, ^uint32(0)) == 0 {
		atomic.CompareAndSwapUintptr(&request.trap, trapPtr, 0)
	}
}

func broadcastInterruptSignal(sig uint32) {
	entries, err := os.ReadDir("/proc/self/task")
	if err != nil {
		return
	}
	pid := syscall.Getpid()
	for _, entry := range entries {
		tid, err := strconv.Atoi(entry.Name())
		if err == nil {
			_, _, _ = syscall.RawSyscall(syscall.SYS_TGKILL, uintptr(pid), uintptr(tid), uintptr(sig))
		}
	}
}

// RequestInterruptAsync covers the narrow check-to-native-entry race for
// lifecycle interruption. A trap published during a host park is consumed at
// the host boundary, so retries need only span native entry/exit transitions;
// a host call that never returns still requires process isolation.
func RequestInterruptAsync(trap []byte) func() {
	storeTrap(trap, uint32(TrapInterrupted))
	if len(trap) < 4 {
		return func() {}
	}
	trapPtr := slicePtr(trap)
	done := make(chan struct{})
	stopped := make(chan struct{})
	var stopOnce sync.Once
	go func() {
		defer close(stopped)
		retry := time.NewTicker(50 * time.Microsecond)
		defer retry.Stop()
		for attempt := 0; attempt < 256; attempt++ {
			if requestInterruptPointer(trapPtr) {
				return
			}
			select {
			case <-done:
				return
			case <-retry.C:
			}
		}
	}()
	return func() {
		stopOnce.Do(func() { close(done) })
		<-stopped
	}
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

// SetInterruptDeadline takes the slow path only for a context carrying an
// actual deadline. Pinning that invocation lets a per-thread kernel timer keep
// working even while Go is stopped for GC; ordinary calls execute none of this.
func SetInterruptDeadline(trap []byte, deadline time.Time) (func(), error) {
	if len(trap) < 4 || deadline.IsZero() {
		return func() {}, nil
	}
	goruntime.LockOSThread()
	trapPtr := slicePtr(trap)
	request := acquireInterruptRequest(trapPtr)
	if request == nil {
		goruntime.UnlockOSThread()
		return nil, &ResourceLimitError{Resource: "native deadline requests", Scope: "process", Used: maxInterruptRequests, Requested: 1, Limit: maxInterruptRequests, Suggestion: "reduce concurrent deadline calls"}
	}
	event := interruptSigevent{
		signo:  int32(atomic.LoadUint32(&interruptSignal)),
		notify: 4, // SIGEV_THREAD_ID
		tid:    int32(syscall.Gettid()),
	}
	var timerID int32
	_, _, errno := syscall.RawSyscall(syscall.SYS_TIMER_CREATE, 1, /* CLOCK_MONOTONIC */
		uintptr(unsafe.Pointer(&event)), uintptr(unsafe.Pointer(&timerID)))
	if errno != 0 {
		releaseInterruptRequest(request, trapPtr)
		goruntime.UnlockOSThread()
		return nil, fmt.Errorf("create native deadline timer: %w", errno)
	}
	delay := time.Until(deadline)
	if delay <= 0 {
		delay = time.Nanosecond
	}
	spec := interruptItimerspec{
		interval: interruptTimespec{
			sec:  int64(interruptDeadlineRetry / time.Second),
			nsec: int64(interruptDeadlineRetry % time.Second),
		},
		value: interruptTimespec{
			sec:  int64(delay / time.Second),
			nsec: int64(delay % time.Second),
		},
	}
	_, _, errno = syscall.RawSyscall6(syscall.SYS_TIMER_SETTIME, uintptr(uint32(timerID)), 0,
		uintptr(unsafe.Pointer(&spec)), 0, 0, 0)
	if errno != 0 {
		_, _, _ = syscall.RawSyscall(syscall.SYS_TIMER_DELETE, uintptr(uint32(timerID)), 0, 0)
		releaseInterruptRequest(request, trapPtr)
		goruntime.UnlockOSThread()
		return nil, fmt.Errorf("arm native deadline timer: %w", errno)
	}
	return func() {
		deleteInterruptTimer(timerID)
		releaseInterruptRequest(request, trapPtr)
		goruntime.UnlockOSThread()
	}, nil
}

// deleteInterruptTimer blocks the reserved signal on the pinned target thread,
// deletes the timer, and drains any already-pending expiration before the
// deadline request is unpublished. A late timer signal therefore cannot affect
// a later invocation that reuses the same trap address.
func deleteInterruptTimer(timerID int32) {
	sigset := uint64(1) << (interruptReservedSignal - 1)
	var oldset uint64
	_, _, errno := syscall.RawSyscall6(syscall.SYS_RT_SIGPROCMASK, 0, // SIG_BLOCK
		uintptr(unsafe.Pointer(&sigset)), uintptr(unsafe.Pointer(&oldset)), 8, 0, 0)
	_, _, _ = syscall.RawSyscall(syscall.SYS_TIMER_DELETE, uintptr(uint32(timerID)), 0, 0)
	if errno == 0 {
		var zero interruptTimespec
		for {
			_, _, waitErr := syscall.RawSyscall6(syscall.SYS_RT_SIGTIMEDWAIT,
				uintptr(unsafe.Pointer(&sigset)), 0, uintptr(unsafe.Pointer(&zero)), 8, 0, 0)
			if waitErr != 0 {
				break
			}
		}
		_, _, _ = syscall.RawSyscall6(syscall.SYS_RT_SIGPROCMASK, 2, // SIG_SETMASK
			uintptr(unsafe.Pointer(&oldset)), 0, 8, 0, 0)
	}
}

func HostInterruptSupported() bool { return true }

//lint:ignore U1000 entry point referenced from assembly
func interruptSigHandler()

//lint:ignore U1000 landing pad referenced from assembly
func nativeInterruptTrap()

func addrInterruptSigHandler() uintptr
func addrNativeInterruptTrap() uintptr
