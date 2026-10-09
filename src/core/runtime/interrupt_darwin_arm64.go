//go:build darwin && arm64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"fmt"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Darwin has no Linux-style targeted queued signal broadcast. Interruption is
// nevertheless a cold operation, so enumerate and suspend Mach threads only
// when cancellation is requested. The matching thread is authenticated by both
// its generated-code PC and X26 linear-memory context before its PC is redirected
// to the native foreign-stack landing pad. Normal Wasm execution does no work.
const (
	maxDarwinExecutableCodeRanges = 4096
	executableCodeBucketCount     = 1024
	armThreadState64Flavor        = 6
	armThreadState64Count         = 68
	armThreadStateNoPtrauth       = 1
	darwinInterruptRetry          = 50 * time.Microsecond
)

type darwinExecutableCodeRange struct {
	start uintptr
	end   uintptr
}

type darwinARMThreadState64 struct {
	X     [29]uint64
	FP    uint64
	LR    uint64
	SP    uint64
	PC    uint64
	CPSR  uint32
	Flags uint32
}

var (
	darwinExecutableCodeRanges [maxDarwinExecutableCodeRanges]darwinExecutableCodeRange
	// A zero count proves that this bucket has no live range.
	executableCodeBucketCounts     [executableCodeBucketCount]uint16
	darwinExecutableCodeRangeLimit uint32
	darwinExecutableCodeMu         sync.Mutex
	darwinInterruptMu              sync.Mutex
	darwinInterruptLandingPC       = addrDarwinNativeInterruptTrap()
)

const (
	_ = uint(unsafe.Sizeof(darwinARMThreadState64{}) - armThreadState64Count*4)
	_ = uint(armThreadState64Count*4 - unsafe.Sizeof(darwinARMThreadState64{}))
)

type interruptActivation struct{}

func beginInterruptActivation([]byte) (*interruptActivation, error) { return nil, nil }
func (*interruptActivation) enterWasm(uintptr)                      {}
func (*interruptActivation) leaveWasm()                             {}
func (*interruptActivation) close()                                 {}

func registerExecutableCode(mem []byte) error {
	if len(mem) == 0 {
		return fmt.Errorf("register executable code: empty mapping")
	}
	start := slicePtr(mem)
	end := start + uintptr(len(mem))
	darwinExecutableCodeMu.Lock()
	defer darwinExecutableCodeMu.Unlock()
	bucket := executableCodeBucket(start)
	checkDuplicate := executableCodeBucketCounts[bucket] != 0
	limit := int(atomic.LoadUint32(&darwinExecutableCodeRangeLimit))
	firstHole := -1
	for i := 0; i < limit; i++ {
		r := &darwinExecutableCodeRanges[i]
		registered := atomic.LoadUintptr(&r.start)
		if checkDuplicate && registered == start {
			if atomic.LoadUintptr(&r.end) != end {
				return fmt.Errorf("executable code range changed for mapping")
			}
			return nil
		}
		if registered == 0 && firstHole < 0 {
			firstHole = i
			if !checkDuplicate {
				break
			}
		}
	}
	if firstHole < 0 {
		if limit == len(darwinExecutableCodeRanges) {
			return fmt.Errorf("executable code range table full (%d)", maxDarwinExecutableCodeRanges)
		}
		firstHole = limit
	}
	r := &darwinExecutableCodeRanges[firstHole]
	atomic.StoreUintptr(&r.end, end)
	atomic.StoreUintptr(&r.start, start)
	if firstHole == limit {
		atomic.StoreUint32(&darwinExecutableCodeRangeLimit, uint32(limit+1))
	}
	executableCodeBucketCounts[bucket]++
	return nil
}

func executableCodeBucket(start uintptr) uint16 {
	return uint16((uint64(start>>12) * 0x9e3779b97f4a7c15) >> 54)
}

func unregisterExecutableCode(mem []byte) {
	if len(mem) == 0 {
		return
	}
	start := slicePtr(mem)
	darwinExecutableCodeMu.Lock()
	defer darwinExecutableCodeMu.Unlock()
	for i := range darwinExecutableCodeRanges {
		r := &darwinExecutableCodeRanges[i]
		if atomic.LoadUintptr(&r.start) != start {
			continue
		}
		atomic.StoreUintptr(&r.start, 0)
		atomic.StoreUintptr(&r.end, 0)
		executableCodeBucketCounts[executableCodeBucket(start)]--
		limit := int(atomic.LoadUint32(&darwinExecutableCodeRangeLimit))
		for limit > 0 && atomic.LoadUintptr(&darwinExecutableCodeRanges[limit-1].start) == 0 {
			limit--
		}
		atomic.StoreUint32(&darwinExecutableCodeRangeLimit, uint32(limit))
		return
	}
}

func darwinGeneratedPC(pc uintptr) bool {
	// arm64e thread states can carry pointer-authentication bits. Native mappings
	// occupy the canonical low address space; strip only the authentication top.
	pc &= uintptr(0x0000ffffffffffff)
	limit := int(atomic.LoadUint32(&darwinExecutableCodeRangeLimit))
	for i := 0; i < limit; i++ {
		r := &darwinExecutableCodeRanges[i]
		start := atomic.LoadUintptr(&r.start)
		if start != 0 && pc >= start && pc < atomic.LoadUintptr(&r.end) {
			return true
		}
	}
	return false
}

func RequestInterrupt(trap []byte) {
	storeTrap(trap, uint32(TrapInterrupted))
	if len(trap) < 4 {
		return
	}
	requestDarwinInterrupt(slicePtr(trap))
}

func RequestInterruptAsync(trap []byte) func() {
	storeTrap(trap, uint32(TrapInterrupted))
	if len(trap) < 4 {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	var stopOnce sync.Once
	trapPtr := slicePtr(trap)
	go func() {
		defer close(stopped)
		retry := time.NewTicker(darwinInterruptRetry)
		defer retry.Stop()
		for {
			if requestDarwinInterrupt(trapPtr) {
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

func SetInterruptDeadline(trap []byte, deadline time.Time) (func(), error) {
	if len(trap) < 4 || deadline.IsZero() {
		return func() {}, nil
	}
	// The context watcher already schedules the deadline and retries the cold
	// Mach redirection. A second timer would race the same trap ownership and can
	// publish a late interrupt after the first request has completed.
	return func() {}, nil
}

func HostInterruptSupported() bool { return true }

func requestDarwinInterrupt(trapPtr uintptr) bool {
	// Context cancellation and deadline expiry can request the same trap at the
	// same instant. Serialize enumeration so two requesters never suspend one
	// another while each is walking the process thread list.
	darwinInterruptMu.Lock()
	defer darwinInterruptMu.Unlock()
	// Keep the self port associated with this goroutine throughout enumeration.
	// A migration could otherwise make a candidate become our current thread.
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	task := machTaskSelf()
	self := machThreadSelf()
	if task == 0 || self == 0 {
		return false
	}
	defer machPortDeallocate(task, self)
	var list unsafe.Pointer
	var count uint32
	if machTaskThreads(task, &list, &count) != 0 || list == nil {
		return false
	}
	defer machVMDeallocate(task, uintptr(list), uintptr(count)*4)
	threads := unsafe.Slice((*uint32)(list), int(count))
	for i, thread := range threads {
		if thread == 0 {
			continue
		}
		if thread == self {
			machPortDeallocate(task, thread)
			continue
		}
		// thread_suspend can wait behind an unrelated thread's uninterruptible
		// kernel work. Take a non-stopping state snapshot first and suspend only a
		// thread whose PC is currently inside a registered native-code mapping.
		var sampled darwinARMThreadState64
		sampledCount := uint32(armThreadState64Count)
		if machThreadGetState(thread, &sampled, &sampledCount) != 0 || sampledCount < armThreadState64Count || !darwinGeneratedPC(uintptr(sampled.PC)) {
			machPortDeallocate(task, thread)
			continue
		}
		var state darwinARMThreadState64
		matched := redirectDarwinThread(task, thread, trapPtr, &state)
		machPortDeallocate(task, thread)
		if matched {
			for _, remaining := range threads[i+1:] {
				if remaining != 0 {
					machPortDeallocate(task, remaining)
				}
			}
			return true
		}
	}
	return false
}

//go:linkname procPin runtime.procPin
func procPin() int

//go:linkname procUnpin runtime.procUnpin
func procUnpin()

// redirectDarwinThread suspends thread and, if it is executing generated code
// for trapPtr, moves its PC to the interrupt landing pad. The PC sample taken
// before suspension can be stale: the thread may already be back in Go code,
// holding a P or runtime locks. The window therefore must not block, allocate,
// or yield its P. A stop-the-world started meanwhile would otherwise wait on
// the suspended thread's P while this goroutine waited in exitsyscall to
// resume it. procPin disables preemption, and raw calls keep the P, so a
// stop-the-world waits for the thread to be resumed instead.
func redirectDarwinThread(task, thread uint32, trapPtr uintptr, state *darwinARMThreadState64) bool {
	procPin()
	defer procUnpin()
	if r, _, _ := rawSyscall6(addrMachThreadSuspend(), uintptr(thread), 0, 0, 0, 0, 0); int32(r) != 0 {
		return false
	}
	matched := false
	count := uint32(armThreadState64Count)
	r, _, _ := rawSyscall6(addrMachThreadGetState(), uintptr(thread), armThreadState64Flavor, uintptr(unsafe.Pointer(state)), uintptr(unsafe.Pointer(&count)), 0, 0)
	if int32(r) == 0 && count >= armThreadState64Count && darwinGeneratedPC(uintptr(state.PC)) {
		linMem := uintptr(state.X[26])
		if darwinTrapContextMatches(task, linMem, trapPtr) {
			// The requester's earlier trap store can be overwritten by a guest
			// host-call status and then cleared by the host loop. Republish it
			// while the thread is stopped, as the Linux handler does, so the
			// landing pad never returns to Go with an empty trap cell.
			atomic.StoreUint32((*uint32)(offHeapPointer(trapPtr)), uint32(TrapInterrupted))
			state.X[9] = uint64(linMem)
			state.PC = uint64(darwinInterruptLandingPC)
			state.Flags |= armThreadStateNoPtrauth
			r, _, _ = rawSyscall6(addrMachThreadSetState(), uintptr(thread), armThreadState64Flavor, uintptr(unsafe.Pointer(state)), armThreadState64Count, 0, 0)
			matched = int32(r) == 0
		}
	}
	_, _, _ = rawSyscall6(addrMachThreadResume(), uintptr(thread), 0, 0, 0, 0, 0)
	return matched
}

// A PC inside a code image does not establish that X26 contains linear memory:
// entry/exit adapters can be sampled before installing or after restoring it.
// Ask the kernel to copy the candidate word so an invalid or unmapped register
// value cannot fault the cancellation goroutine. This is a cold-path operation.
// It runs inside redirectDarwinThread's pinned window, so it uses a raw call.
func darwinTrapContextMatches(task uint32, linMem, trapPtr uintptr) bool {
	if linMem < abi.TrapCellPtrOffset || trapPtr == 0 {
		return false
	}
	var candidate uintptr
	var copied uint64
	result, _, _ := rawSyscall6(addrMachVMReadOverwrite(), uintptr(task),
		linMem-abi.TrapCellPtrOffset, unsafe.Sizeof(candidate),
		uintptr(unsafe.Pointer(&candidate)), uintptr(unsafe.Pointer(&copied)), 0)
	return int32(result) == 0 && copied == uint64(unsafe.Sizeof(candidate)) && candidate == trapPtr
}

func machTaskSelf() uint32 {
	r, _, _ := syscall6(addrMachTaskSelf(), 0, 0, 0, 0, 0, 0)
	return uint32(r)
}

func machThreadSelf() uint32 {
	r, _, _ := syscall6(addrMachThreadSelf(), 0, 0, 0, 0, 0, 0)
	return uint32(r)
}

func machTaskThreads(task uint32, list *unsafe.Pointer, count *uint32) int32 {
	r, _, _ := syscall6(addrMachTaskThreads(), uintptr(task), uintptr(unsafe.Pointer(list)), uintptr(unsafe.Pointer(count)), 0, 0, 0)
	return int32(r)
}

func machThreadGetState(thread uint32, state *darwinARMThreadState64, count *uint32) int32 {
	r, _, _ := syscall6(addrMachThreadGetState(), uintptr(thread), armThreadState64Flavor, uintptr(unsafe.Pointer(state)), uintptr(unsafe.Pointer(count)), 0, 0)
	return int32(r)
}

func machPortDeallocate(task, port uint32) {
	_, _, _ = syscall6(addrMachPortDeallocate(), uintptr(task), uintptr(port), 0, 0, 0, 0)
}

func machVMDeallocate(task uint32, address, size uintptr) {
	_, _, _ = syscall6(addrMachVMDeallocate(), uintptr(task), address, size, 0, 0, 0)
}

func addrMachTaskSelf() uintptr
func addrMachThreadSelf() uintptr
func addrMachTaskThreads() uintptr
func addrMachThreadSuspend() uintptr
func addrMachThreadResume() uintptr
func addrMachThreadGetState() uintptr
func addrMachThreadSetState() uintptr
func addrMachPortDeallocate() uintptr
func addrMachVMDeallocate() uintptr
func addrMachVMReadOverwrite() uintptr
func addrDarwinNativeInterruptTrap() uintptr

//go:cgo_import_dynamic libc_mach_task_self mach_task_self "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_mach_thread_self mach_thread_self "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_task_threads task_threads "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_thread_suspend thread_suspend "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_thread_resume thread_resume "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_thread_get_state thread_get_state "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_thread_set_state thread_set_state "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_mach_port_deallocate mach_port_deallocate "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_mach_vm_deallocate mach_vm_deallocate "/usr/lib/libSystem.B.dylib"

//go:cgo_import_dynamic libc_mach_vm_read_overwrite mach_vm_read_overwrite "/usr/lib/libSystem.B.dylib"
