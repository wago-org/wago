//go:build (linux && (amd64 || arm64 || riscv64)) || (darwin && arm64)

package runtime

import (
	"encoding/binary"
	"fmt"
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
	stack    []byte
	stackTop uintptr

	// Scratch for the common non-reentrant synchronous host-call path. Passing
	// slices to HostCall makes stack-local arrays escape; keeping one bounded pair
	// on the Engine avoids two tiny heap allocations per host re-entry while still
	// falling back to per-call scratch if CallWithHost is re-entered before the
	// previous call returns.
	hostScratchInUse bool
	hostArgs         [maxHostArity]uint64
	hostResults      [maxHostArity]uint64
}

func loadTrap(trap []byte) uint32 {
	if len(trap) < 4 {
		return 0
	}
	return atomic.LoadUint32((*uint32)(unsafe.Pointer(&trap[0])))
}

func storeTrap(trap []byte, v uint32) {
	if len(trap) >= 4 {
		atomic.StoreUint32((*uint32)(unsafe.Pointer(&trap[0])), v)
	}
}

const defaultStackBytes = 4 << 20 // 4 MiB foreign execution stack

func NewEngine() (*Engine, error) {
	st, err := mmapRW(defaultStackBytes)
	if err != nil {
		return nil, err
	}
	top := uintptr(unsafe.Pointer(&st[0])) + uintptr(len(st))
	top &^= 15 // 16-byte align (page-aligned already, but be explicit)
	return &Engine{stack: st, stackTop: top}, nil
}

var engineCache struct {
	sync.Mutex
	e *Engine
}

// AcquireEngine returns an Engine, reusing one recently released by ReleaseEngine
// when available. The cache is intentionally one slot: repeated instantiate/close
// loops avoid stack mmap churn without retaining an unbounded number of 4 MiB
// foreign stacks.
func AcquireEngine() (*Engine, error) {
	engineCache.Lock()
	e := engineCache.e
	engineCache.e = nil
	engineCache.Unlock()
	if e != nil {
		return e, nil
	}
	return NewEngine()
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

// Call enters native code at code following WARP's WasmWrapper ABI. serArgs,
// linMem, trap and results MUST be backed by off-heap memory (Arena/JobMemory)
// so their addresses are stable across the call. It returns a *TrapError if the
// wrapper set a non-zero trap code.
//
// The trap cell is zeroed and its pointer installed in basedata here, once per
// entry, so generated code never passes or clears it: emitTrap (the only
// consumer, cold) reads [linMem-abi.TrapCellPtrOffset], and function returns
// carry no trap protocol at all (WARP's model).
func (e *Engine) Call(code uintptr, serArgs, linMem, trap, results []byte) error {
	installTrapCell(linMem, trap)
	enterNative(code, slicePtr(serArgs), slicePtr(linMem), slicePtr(trap), slicePtr(results), e.stackTop)
	if len(trap) >= 4 {
		if tc := TrapCode(loadTrap(trap)); tc != TrapNone {
			return &TrapError{Code: tc}
		}
	}
	return nil
}

// CallPrepared enters native code after JobMemory.BindTrapCell established a
// stable trap pointer and a zero trap cell. Successful native execution never
// writes that cell, so repeated calls avoid clearing/rebinding it. A cold trap
// is consumed and cleared before returning, re-establishing the invariant for
// the next call.
func (e *Engine) CallPrepared(code uintptr, serArgs []byte, linMemBase uintptr, trap, results []byte) error {
	enterNative(code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop)
	if len(trap) >= 4 {
		if tc := TrapCode(loadTrap(trap)); tc != TrapNone {
			storeTrap(trap, 0)
			return &TrapError{Code: tc}
		}
	}
	return nil
}

// installTrapCell zeroes the trap cell and writes its address into the
// basedata trap-cell slot so generated code can reach it on the (cold) trap
// path without any per-call plumbing.
func installTrapCell(linMem, trap []byte) {
	if len(trap) < 4 || len(linMem) == 0 {
		return
	}
	storeTrap(trap, 0)
	*(*uint64)(unsafe.Pointer(uintptr(unsafe.Pointer(&linMem[0])) - abi.TrapCellPtrOffset)) = uint64(slicePtr(trap))
}

// CallWithHost runs native code that may request returning host imports via the
// synchronous re-entry protocol. The first crossing is a
// normal enterNative; whenever native code parks at a host call (trap cell ==
// hostCallPending), the bound host function is run here — on the goroutine stack,
// in normal Go context, so arbitrary host code is safe (no foreign-stack /
// morestack hazard) — and native code is resumed via resumeNative. This mirrors
// wazero's host-call exec loop.
//
// ctrl must point at an off-heap control frame of at least ctrlFrameSize bytes
// whose address has been installed as the import ctx via JobMemory.SetCustomCtx.
func (e *Engine) CallWithHost(code uintptr, serArgs, linMem, trap, results, ctrl []byte, host HostCall) error {
	stub, err := hostCallStubPtr()
	if err != nil {
		return fmt.Errorf("jit: host-call stub: %w", err)
	}
	installTrapCell(linMem, trap)
	binary.LittleEndian.PutUint64(ctrl[hcTrampoline:], uint64(stub)) // native calls [ctrl+hcTrampoline]
	ctrlPtr := slicePtr(ctrl)
	if e.hostScratchInUse {
		var argBuf, resBuf [maxHostArity]uint64
		return e.callWithHostLoop(code, serArgs, linMem, trap, results, ctrl, ctrlPtr, host, argBuf[:], resBuf[:])
	}
	e.hostScratchInUse = true
	defer func() { e.hostScratchInUse = false }()
	return e.callWithHostLoop(code, serArgs, linMem, trap, results, ctrl, ctrlPtr, host, e.hostArgs[:], e.hostResults[:])
}

func (e *Engine) callWithHostLoop(code uintptr, serArgs, linMem, trap, results, ctrl []byte, ctrlPtr uintptr, host HostCall, argBuf, resBuf []uint64) error {
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
	// forever: both require the caller to arm a timeout, exactly as under wazero.
	for first := true; ; first = false {
		if first {
			enterNative(code, slicePtr(serArgs), slicePtr(linMem), slicePtr(trap), slicePtr(results), e.stackTop)
		} else {
			storeTrap(trap, 0) // clear the pending marker before resuming
			resumeNative(ctrlPtr, e.stackTop)
		}
		switch tc := loadTrap(trap); {
		case tc == hostCallPending:
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
				return fmt.Errorf("jit: host call arity %d/%d exceeds %d", n, nres, maxHostArity)
			}
			for k := 0; k < n; k++ {
				argBuf[k] = binary.LittleEndian.Uint64(ctrl[hcArgs+k*8:])
			}
			for k := 0; k < nres; k++ {
				resBuf[k] = 0
			}
			host(imp, argBuf[:n], resBuf)
			for k := 0; k < nres; k++ {
				binary.LittleEndian.PutUint64(ctrl[hcResults+k*8:], resBuf[k])
			}
			// loop: resumeNative continues native code after the host call
		case tc != 0:
			return &TrapError{Code: TrapCode(tc)}
		default:
			return nil
		}
	}
}

func (e *Engine) Close() error { return munmap(e.stack) }

// MapCode copies machine code into a fresh W^X executable mapping and returns
// the mapping (keep it alive / Unmap to free) plus the entry-point pointer to
// pass to Engine.Call.
func MapCode(code []byte) (mem []byte, entry uintptr, err error) {
	mem, err = mmapExec(code)
	if err != nil {
		return nil, 0, err
	}
	return mem, slicePtr(mem), nil
}

// Unmap releases a mapping returned by MapCode.
func Unmap(mem []byte) error { return munmap(mem) }

// slicePtr returns the address of the first element of an off-heap slice as a
// uintptr. Safe only for mmap-backed slices, whose backing array the GC never
// moves.
func slicePtr(b []byte) uintptr {
	if len(b) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&b[0]))
}
