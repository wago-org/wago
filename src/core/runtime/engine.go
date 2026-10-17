//go:build ((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package runtime

import (
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
	stack              []byte
	stackTop           uintptr
	preparedScalarHost *PreparedScalarHost
	preparedInt        tinygoPreparedIntState

	engineHostState
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

// ReleaseEngine transfers an idle Engine to the bounded cache, or closes it.
// All native calls, parked continuations, and borrowed stack addresses must
// have been released before this ownership transfer.
func ReleaseEngine(e *Engine) error {
	if e == nil {
		return nil
	}
	e.preparedScalarHost = nil
	e.resetNativeInvocation()
	engineCache.Lock()
	if engineCache.e == nil {
		if !e.prepareIdleStackForCache() {
			engineCache.Unlock()
			return e.Close()
		}
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

// NativeStackRange returns an alias within the usable owned foreign stack.
// It checks addresses without following them. The caller must retain the active
// Engine and its stack lease for the lifetime of the returned view.
func (e *Engine) NativeStackRange(address uintptr, size int) []byte {
	if e == nil || len(e.stack) == 0 || size <= 0 {
		return nil
	}
	base := slicePtr(e.stack)
	if address < base || address-base < stackFenceMargin || address-base > uintptr(len(e.stack)) {
		return nil
	}
	offset := address - base
	if uintptr(size) > uintptr(len(e.stack))-offset {
		return nil
	}
	start := int(offset)
	return e.stack[start : start+size : start+size]
}

// StackBytes reports the mapped foreign execution stack capacity.
func (e *Engine) StackBytes() uint64 {
	if e == nil {
		return 0
	}
	return uint64(len(e.stack))
}

// Only first entry publishes invocation extents. Resume keeps the parked root's
// view intact, even when a host callback enters a different Engine.
func (e *Engine) enterNative(code uintptr, arguments []byte, home uintptr, trap, results []byte) {
	e.prepareNativeInvocation(arguments, results)
	enterNative(code, slicePtr(arguments), home, slicePtr(trap), slicePtr(results), e.stackTop)
}

func (e *Engine) enterNativeBounded(code uintptr, arguments []byte, home uintptr, trap, results []byte) {
	e.prepareNativeInvocation(arguments, results)
	enterNativeBounded(code, slicePtr(arguments), home, slicePtr(trap), slicePtr(results), e.stackTop)
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
	if len(trap) < TrapBufferBytes {
		return errIncompleteTrapBuffer
	}
	if len(linMem) != 0 {
		clearTrapUnlessInterrupted(trap)
		base := unsafe.Pointer(&linMem[0])
		*(*uint64)(unsafe.Add(base, -int(abi.TrapCellPtrOffset))) = uint64(slicePtr(trap))
		*(*uint64)(unsafe.Add(base, -int(abi.EHHandlerPtrOffset))) = 0
	}
	e.enterNative(code, serArgs, slicePtr(linMem), trap, results)
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
	e.enterNative(code, serArgs, linMemBase, trap, results)
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

// CallPreparedBounded uses the compiler's straight-line work proof to keep a
// short wrapper entry on the Go P. All trap and owner handling is unchanged.
func (e *Engine) CallPreparedBounded(code uintptr, serArgs []byte, linMemBase uintptr, trap, results []byte) error {
	if err := validateTrapBuffer(trap); err != nil {
		return err
	}
	e.enterNativeBounded(code, serArgs, linMemBase, trap, results)
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

// clearTrapUnlessInterrupted clears a stale non-interrupt trap. A concurrent
// close interruption wins the CAS reset and remains visible at the first
// generated safepoint.
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
				if goruntime.GOARCH == "riscv64" {
					clear(trap[8:24])
				} else {
					clear(trap[16:24])
				}
			}
			return
		}
	}
}

// PreparePreparedIntTrap gives direct prepared entries the same stale-trap
// reset as the ordinary wrapper entry. The zero fast path avoids rewriting the
// cold source payload on successful calls; a concurrent interruption remains
// visible to the generated entry poll.
func PreparePreparedIntTrap(trap []byte) {
	if len(trap) < 4 || atomic.LoadUint32((*uint32)(unsafe.Pointer(&trap[0]))) == 0 {
		return
	}
	clearTrapUnlessInterrupted(trap)
}

func (e *Engine) Close() error {
	e.preparedScalarHost = nil
	preparedErr := e.preparedInt.close()
	stackErr := closeOwnedMapping(&e.stack, 0, 0)
	if stackErr == nil {
		e.stackTop = 0
	}
	return errors.Join(preparedErr, stackErr)
}

// MapCode copies machine code into a fresh W^X executable mapping and returns
// the mapping (keep it alive / Unmap to free) plus the entry-point pointer to
// pass to Engine.Call.
// On failure entry is zero. A nonnil mapping retains cleanup ownership when
// rollback failed; the caller must keep it and retry Unmap.
func MapCode(code []byte) (mem []byte, entry uintptr, err error) {
	return mapCodeWith(code, mmapExec, registerExecutableCode, Unmap)
}

func mapCodeWith(code []byte, mapExec func([]byte) ([]byte, error), register func([]byte) error, unmap func([]byte) error) (mem []byte, entry uintptr, err error) {
	mem, err = mapExec(code)
	if err != nil {
		return mem, 0, err
	}
	if err = register(mem); err != nil {
		mem, err = rollbackCodeMapping(mem, err, unmap)
		return mem, 0, err
	}
	return mem, slicePtr(mem), nil
}

// mmapExecWith keeps a failed RX/flush rollback attached to the return value.
// Function arguments provide local fault injection without process-wide hooks.
func mmapExecWith(code []byte, mapRW func(int) ([]byte, error), protect func([]byte) error, unmap func([]byte) error) ([]byte, error) {
	mem, err := mapRW(len(code))
	if err != nil {
		return mem, err
	}
	copy(mem, code)
	if err := protect(mem); err != nil {
		return rollbackCodeMapping(mem, err, unmap)
	}
	return mem, nil
}

func rollbackCodeMapping(mem []byte, primary error, unmap func([]byte) error) ([]byte, error) {
	if cleanup := unmap(mem); cleanup != nil {
		return mem, errors.Join(primary, cleanup)
	}
	return nil, primary
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

// The engine's retained native-memory lease authenticates addr. This store
// never accepts a Go-heap address; avoid a temporary pointer-sized Go object.
//
//go:nocheckptr
func storeOffHeapU64(addr uintptr, value uint64) {
	*(*uint64)(unsafe.Pointer(addr)) = value
}

// The active native lease owns the complete mmap-backed control or instance
// word at addr. This reader must never receive an address into the Go heap.
//
//go:nocheckptr
func loadOffHeapU64(addr uintptr) uint64 {
	return *(*uint64)(unsafe.Pointer(addr))
}
