//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"encoding/binary"
	"errors"
	"fmt"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// enterNative switches to the engine's foreign stack, calls the WARP WasmWrapper
// at code following the target's native argument mapping, then restores the Go
// context. The standard toolchain implements it in target assembly; TinyGo amd64
// generates an equivalent machine-code trampoline at run time.

// Engine owns a dedicated, off-heap execution stack for native wasm code.
type Engine struct {
	stack       []byte
	stackTop    uintptr
	preparedInt tinygoPreparedIntState

	// Scratch for the common non-reentrant synchronous host-call path. Passing
	// slices to HostCall makes stack-local arrays escape; keeping one bounded pair
	// on the Engine avoids two tiny heap allocations per host re-entry while still
	// falling back to per-call scratch if CallWithHost is re-entered before the
	// previous call returns.
	hostScratchInUse bool
	hostArgs         [maxHostArity]uint64
	hostResults      [maxHostArity]uint64
}

const (
	// DefaultNativeStackBytes preserves the historical 4 MiB foreign stack.
	DefaultNativeStackBytes uint64 = 4 << 20
	// MinNativeStackBytes leaves at least one fence margin of usable stack.
	MinNativeStackBytes uint64 = 512 << 10
	// MaxNativeStackBytes bounds retained virtual address space per Engine.
	MaxNativeStackBytes uint64 = 1 << 30
)

func validateNativeStackBytes(stackBytes uint64) error {
	if stackBytes < MinNativeStackBytes || stackBytes > MaxNativeStackBytes {
		return fmt.Errorf("jit: native stack bytes must be between %d and %d, got %d", MinNativeStackBytes, MaxNativeStackBytes, stackBytes)
	}
	if stackBytes&15 != 0 {
		return fmt.Errorf("jit: native stack bytes must be 16-byte aligned, got %d", stackBytes)
	}
	return nil
}

func NewEngine() (*Engine, error) {
	return NewEngineWithStackBytes(DefaultNativeStackBytes)
}

// NewEngineWithStackBytes creates an Engine with the selected bounded foreign
// execution stack capacity.
func NewEngineWithStackBytes(stackBytes uint64) (*Engine, error) {
	if err := validateNativeStackBytes(stackBytes); err != nil {
		return nil, err
	}
	st, err := mmapRW(int(stackBytes))
	if err != nil {
		return nil, err
	}
	top := uintptr(unsafe.Pointer(&st[0])) + uintptr(len(st))
	top &^= 15 // 16-byte align (page-aligned already, but be explicit)
	e := &Engine{stack: st, stackTop: top}
	if err := e.initNativeEntry(); err != nil {
		return nil, errors.Join(err, munmap(st))
	}
	return e, nil
}

var engineCache struct {
	sync.Mutex
	e *Engine
}

// AcquireEngine returns a default-capacity Engine.
func AcquireEngine() (*Engine, error) {
	return AcquireEngineWithStackBytes(DefaultNativeStackBytes)
}

// AcquireEngineWithStackBytes returns an Engine with exactly the requested
// capacity. The one-slot cache never substitutes a smaller stack or retains a
// mismatched large stack after a later default-capacity request.
func AcquireEngineWithStackBytes(stackBytes uint64) (*Engine, error) {
	if err := validateNativeStackBytes(stackBytes); err != nil {
		return nil, err
	}
	engineCache.Lock()
	e := engineCache.e
	engineCache.e = nil
	engineCache.Unlock()
	if e != nil {
		if e.StackBytes() == stackBytes {
			return e, nil
		}
		if err := e.Close(); err != nil {
			return nil, err
		}
	}
	return NewEngineWithStackBytes(stackBytes)
}

// ReleaseEngine returns e to the bounded cache or unmaps its stack if the cache
// is already occupied.
func ReleaseEngine(e *Engine) error {
	if e == nil {
		return nil
	}
	engineCache.Lock()
	if engineCache.e == nil {
		engineCache.e = e
		engineCache.Unlock()
		return nil
	}
	engineCache.Unlock()
	return e.Close()
}

// stackFenceMargin is the headroom above the foreign stack's low bound at which
// the prologue stack-fence check trips. It must exceed the deepest stack a
// single function descends after its check (call argument buffers, the trap
// unwind path) so the trap fires before any access faults.
const stackFenceMargin = 256 << 10 // 256 KiB

// StackLimit is the address below which the foreign execution stack is exhausted.
// Native code compares its stack pointer against this (installed via
// JobMemory.SetStackFence) to trap on unbounded recursion instead of faulting.
func (e *Engine) StackLimit() uintptr {
	return uintptr(unsafe.Pointer(&e.stack[0])) + stackFenceMargin
}

// StackTop returns the exclusive high address of the current foreign stack.
// Exact native frame walkers use it only to bound cold parked-wrapper scans.
func (e *Engine) StackTop() uintptr {
	if e == nil || len(e.stack) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&e.stack[0])) + uintptr(len(e.stack))
}

// StackBytes reports the mapped foreign execution stack capacity.
func (e *Engine) StackBytes() uint64 {
	if e == nil {
		return 0
	}
	return uint64(len(e.stack))
}

// Call enters native code at code following WARP's WasmWrapper ABI. serArgs,
// linMem, trap and results MUST be backed by off-heap memory (Arena/JobMemory)
// so their addresses are stable across the call. trap must contain at least
// TrapBufferBytes bytes because native trap stubs write the code and source
// location. It returns a *TrapError if the wrapper set a non-zero trap code.
//
// The trap cell is zeroed and its pointer installed in basedata here, once per
// entry, so generated code never passes or clears it: emitTrap (the only
// consumer, cold) reads [linMem-abi.TrapCellPtrOffset], and function returns
// carry no trap protocol at all (WARP's model).
func (e *Engine) Call(code uintptr, serArgs, linMem, trap, results []byte) error {
	if err := validateTrapBuffer(trap); err != nil {
		return err
	}
	installTrapCell(linMem, trap)
	enterNative(code, slicePtr(serArgs), slicePtr(linMem), slicePtr(trap), slicePtr(results), e.stackTop)
	goruntime.KeepAlive(serArgs)
	goruntime.KeepAlive(linMem)
	goruntime.KeepAlive(trap)
	goruntime.KeepAlive(results)
	goruntime.KeepAlive(e)
	if tc := TrapCode(loadTrap(trap)); tc != TrapNone {
		return trapErrorFromBuffer(tc, trap)
	}
	return nil
}

// CallPrepared enters native code after JobMemory.BindTrapCell established a
// stable trap pointer and a zero trap buffer of at least TrapBufferBytes bytes.
// Successful native execution never writes that buffer, so repeated calls avoid
// clearing/rebinding it. A cold trap is consumed and cleared before returning,
// re-establishing the invariant for the next call.
func (e *Engine) CallPrepared(code uintptr, serArgs []byte, linMemBase uintptr, trap, results []byte) error {
	if err := validateTrapBuffer(trap); err != nil {
		return err
	}
	enterNative(code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop)
	goruntime.KeepAlive(serArgs)
	goruntime.KeepAlive(trap)
	goruntime.KeepAlive(results)
	goruntime.KeepAlive(e)
	if tc := TrapCode(loadTrap(trap)); tc != TrapNone {
		storeTrap(trap, 0)
		return trapErrorFromBuffer(tc, trap)
	}
	return nil
}

var errIncompleteTrapBuffer = errors.New("jit: trap buffer needs at least 24 bytes")

func validateTrapBuffer(trap []byte) error {
	if len(trap) < TrapBufferBytes {
		return errIncompleteTrapBuffer
	}
	return nil
}

// installTrapCell clears any stale non-interrupt trap and writes the cell's
// address into basedata. A concurrent close interruption wins the CAS reset and
// remains visible at the first generated safepoint.
func clearTrapUnlessInterrupted(trap []byte) {
	if len(trap) < 4 {
		return
	}
	cell := (*uint32)(unsafe.Pointer(&trap[0]))
	for {
		old := atomic.LoadUint32(cell)
		// A zero cell needs no write. A concurrent interruption remains visible
		// because this fast path never stores over it.
		if old == 0 || TrapCode(old) == TrapInterrupted || atomic.CompareAndSwapUint32(cell, old, 0) {
			if len(trap) >= TrapBufferBytes {
				clear(trap[16:24])
			}
			return
		}
	}
}

func installTrapCell(linMem, trap []byte) {
	if len(trap) < 4 || len(linMem) == 0 {
		return
	}
	clearTrapUnlessInterrupted(trap)
	base := unsafe.Pointer(&linMem[0])
	*(*uint64)(unsafe.Add(base, -int(abi.TrapCellPtrOffset))) = uint64(slicePtr(trap))
	*(*uint64)(unsafe.Add(base, -int(abi.EHHandlerPtrOffset))) = 0
}

// CallWithHost runs native code that may request returning host imports via the
// synchronous re-entry protocol. The first crossing is a
// normal enterNative; whenever native code parks at a host call (trap cell ==
// hostCallPending), the bound host function is run here — on the goroutine stack,
// in normal Go context, so arbitrary host code is safe (no foreign-stack /
// morestack hazard) — and native code is resumed via resumeNative.
//
// ctrl must point at an off-heap control frame of at least ctrlFrameSize bytes
// whose address has been installed as the import ctx via JobMemory.SetCustomCtx.
// A cross-instance callee may park through a different frame; its stub publishes
// that exact pointer at trap+8 so dispatch and resume follow the active callee.
func (e *Engine) CallWithHost(code uintptr, serArgs, linMem, trap, results, ctrl []byte, host HostCall) error {
	err := e.CallWithHostBase(code, serArgs, slicePtr(linMem), trap, results, ctrl, host)
	goruntime.KeepAlive(linMem)
	return err
}

// CallWithHostBase is the stable-base form used by guard-page JobMemory, whose
// reserved linear-memory base may not be representable by LinearMemory's Go
// slice. The guard handler is installed/registered by JobMemory creation.
func (e *Engine) CallWithHostBase(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, host HostCall) error {
	if linMemBase == 0 {
		return fmt.Errorf("jit: host-call linear-memory base is zero")
	}
	if err := validateTrapBuffer(trap); err != nil {
		return err
	}
	if err := InitHostCtrlFrame(ctrl); err != nil {
		return err
	}
	clearTrapUnlessInterrupted(trap)
	storeOffHeapU64(linMemBase-abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	ctrlPtr := slicePtr(ctrl)
	var callErr error
	if e.hostScratchInUse {
		var argBuf, resBuf [maxHostArity]uint64
		callErr = e.callWithHostLoop(code, serArgs, linMemBase, trap, results, ctrl, ctrlPtr, host, argBuf[:], resBuf[:])
	} else {
		e.hostScratchInUse = true
		defer func() { e.hostScratchInUse = false }()
		callErr = e.callWithHostLoop(code, serArgs, linMemBase, trap, results, ctrl, ctrlPtr, host, e.hostArgs[:], e.hostResults[:])
	}
	// Native frames can retain these addresses across every host park/resume.
	goruntime.KeepAlive(serArgs)
	goruntime.KeepAlive(trap)
	goruntime.KeepAlive(results)
	goruntime.KeepAlive(ctrl)
	goruntime.KeepAlive(e)
	return callErr
}

// InitHostCtrlFrame installs the shared host-call trampoline in an off-heap
// control frame. Instantiate calls it eagerly so a callee that has never been a
// public root can still park when reached through a cross-instance call.
func InitHostCtrlFrame(ctrl []byte) error {
	if len(ctrl) < ctrlFrameSize {
		return fmt.Errorf("jit: host control frame has %d bytes, need %d", len(ctrl), ctrlFrameSize)
	}
	stub, err := hostCallStubPtr()
	if err != nil {
		return fmt.Errorf("jit: host-call stub: %w", err)
	}
	if _, err := initHostCtrlExtension(ctrl); err != nil {
		return err
	}
	binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], uint64(stub))
	return nil
}

func hostCtrlFrame(ptr uintptr) []byte {
	if n, ok := registeredHostCtrlFrames.Load(ptr); ok {
		return unsafe.Slice((*byte)(offHeapPointer(ptr)), n.(int))
	}
	return unsafe.Slice((*byte)(offHeapPointer(ptr)), ctrlFrameSize)
}

func (e *Engine) callWithHostLoop(code uintptr, serArgs []byte, linMemBase uintptr, trap, results, ctrl []byte, ctrlPtr uintptr, host HostCall, argBuf, resBuf []uint64) error {
	rootCtrl, rootCtrlPtr := ctrl, ctrlPtr
	// The host-call re-entry loop is intentionally unbounded: a single guest
	// invocation may legitimately make an arbitrary number of host calls (e.g. a
	// long-running rule that polls Date.now()/Math.random() in a loop). A fixed
	// re-entry cap would turn such a guest into an opaque hard error *before* its
	// deadline, pre-empting the cooperative interrupt. The runaway-guest guard is
	// the trap cell: a cancelled/expired context arms TrapInterrupted, the guest
	// traps at the next function-entry/loop-header safepoint, and that surfaces
	// here as `tc != 0` — breaking the loop with the interrupt code rather than a
	// synthetic "too many host calls" error. A guest with no deadline that spins
	// on host calls forever is no different from one that spins on compute
	// forever: both require the caller to arm a timeout.
	for first := true; ; first = false {
		if first {
			enterNative(code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop)
		} else {
			clearTrapUnlessInterrupted(trap) // clear host-pending, but preserve concurrent Close interruption
			if TrapCode(loadTrap(trap)) == TrapInterrupted {
				return trapErrorFromBuffer(TrapInterrupted, trap)
			}
			stackTop := e.StackTop()
			prepareHostResume(ctrl, trap, stackTop, e.StackLimit())
			resumeNative(ctrlPtr, stackTop)
		}
		switch tc := loadTrap(trap); {
		case tc == hostCallPending:
			ctrlPtr = uintptr(binary.LittleEndian.Uint64(trap[8:]))
			if ctrlPtr == 0 {
				return fmt.Errorf("jit: host call did not publish an active control frame")
			}
			if ctrlPtr == rootCtrlPtr {
				ctrl = rootCtrl
			} else {
				ctrl = hostCtrlFrame(ctrlPtr)
			}
			imp := binary.LittleEndian.Uint32(ctrl[hcImportIdx:])
			// hcNArgs packs the call's slot counts: low 16 bits = param slots
			// (native->Go), high 16 bits = result slots (Go->native). Copying only
			// the real result count — not all maxHostArity slots — drops ~15 wasted
			// slot zeroings + copy-backs on the common 0/1-result host call, the hot
			// part of the wasm->host round trip.
			raw := binary.LittleEndian.Uint32(ctrl[hcNArgs:])
			n := int(raw & 0xffff)
			nres := int(raw >> 16)
			if n > maxHostArity || nres > maxHostArity {
				argsArea, resultsArea, capacity, err := hostCtrlWideCallAreas(ctrl, n, nres)
				if err != nil {
					return err
				}
				args := unsafe.Slice((*uint64)(unsafe.Pointer(&argsArea[0])), capacity)
				wideResults := unsafe.Slice((*uint64)(unsafe.Pointer(&resultsArea[0])), capacity)
				clear(wideResults[:nres])
				host(ctrlPtr, imp, args[:n], wideResults[:nres])
				continue
			}
			for k := 0; k < n; k++ {
				argBuf[k] = binary.LittleEndian.Uint64(ctrl[hcArgs+k*8:])
			}
			for k := 0; k < nres; k++ {
				resBuf[k] = 0
			}
			host(ctrlPtr, imp, argBuf[:n], resBuf[:nres])
			for k := 0; k < nres; k++ {
				binary.LittleEndian.PutUint64(ctrl[hcResults+k*8:], resBuf[k])
			}
			// loop: resumeNative continues native code after the host call
		case tc != 0:
			return trapErrorFromBuffer(TrapCode(tc), trap)
		default:
			return nil
		}
	}
}

func (e *Engine) Close() error {
	return errors.Join(e.preparedInt.close(), munmap(e.stack))
}

// MapCode copies machine code into a fresh W^X executable mapping and returns
// the mapping (keep it alive / Unmap to free) plus the entry-point pointer to
// pass to Engine.Call.
func MapCode(code []byte) (mem []byte, entry uintptr, err error) {
	mem, err = mmapExec(code)
	if err != nil {
		return nil, 0, err
	}
	if err = registerExecutableCode(mem); err != nil {
		_ = munmap(mem)
		return nil, 0, err
	}
	return mem, slicePtr(mem), nil
}

// SealCode changes an existing code mapping from RW to RX and registers it for
// safe host interruption. The caller retains ownership on failure.
func SealCode(mem []byte) error {
	if len(mem) == 0 {
		return fmt.Errorf("seal executable code: empty mapping")
	}
	if err := protectCodeRX(mem); err != nil {
		return err
	}
	return registerExecutableCode(mem)
}

// Unmap releases a mapping returned by MapCode.
func Unmap(mem []byte) error {
	unregisterExecutableCode(mem)
	return munmap(mem)
}

// slicePtr returns the address of the first element of an off-heap slice as a
// uintptr. Safe only for mmap-backed slices, whose backing array the GC never
// moves.
func slicePtr(b []byte) uintptr {
	if len(b) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&b[0]))
}

// storeOffHeapU64 writes through a known mmap-backed address. Routing the
// uintptr bits through a pointer-sized local mirrors the public runtime's
// offHeapPtr helper and avoids pretending the address belongs to the Go heap.
func offHeapPointer(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

func storeOffHeapU64(addr uintptr, value uint64) {
	*(*uint64)(offHeapPointer(addr)) = value
}
