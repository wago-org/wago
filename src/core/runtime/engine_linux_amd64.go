//go:build linux && amd64

package runtime

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"unsafe"
)

// enterNative switches RSP to the engine's foreign stack, calls the WARP
// WasmWrapper at code following the System V mapping (serArgs->RDI, linMem->RSI,
// trap->RDX, results->RCX), then restores the Go context. The standard toolchain
// implements it in assembly (trampoline_asm_amd64.go + trampoline_amd64.s);
// TinyGo, which cannot assemble Plan9 .s files, generates an equivalent
// machine-code trampoline at run time (trampoline_tinygo_amd64.go).

// Engine owns a dedicated, off-heap execution stack for native wasm code.
type Engine struct {
	stack    []byte
	stackTop uintptr
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

// stackFenceMargin is the headroom above the foreign stack's low bound at which
// the prologue stack-fence check trips. It must exceed the deepest stack a
// single function descends after its check (call argument buffers, the trap
// unwind path) so the trap fires before any access faults.
const stackFenceMargin = 256 << 10 // 256 KiB

// StackLimit is the address below which the foreign execution stack is exhausted.
// Native code compares rsp against this (installed via JobMemory.SetStackFence)
// to trap on unbounded recursion instead of faulting.
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
		if tc := TrapCode(binary.LittleEndian.Uint32(trap)); tc != TrapNone {
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
	binary.LittleEndian.PutUint32(trap, 0)
	*(*uint64)(unsafe.Pointer(uintptr(unsafe.Pointer(&linMem[0])) - abi.TrapCellPtrOffset)) = uint64(slicePtr(trap))
}

// HostFunc is a V2-style host import for the spike: it reads its argument and
// returns a result. The real engine will pass slot buffers; this single-scalar
// shape is enough to prove the round trip.
type HostFunc func(arg uint32) uint32

// CallWithHost runs native code that may request host imports via the safe
// re-entry protocol. When the stub signals hostCallPending (via *trap), the Go
// host function is run here — on the goroutine stack, in normal Go context, so
// arbitrary host code is safe (no foreign-stack/morestack hazard) — and native
// code is re-entered to resume. This mirrors wazero's host-call exec loop.
//
// ctrl must point at an off-heap control block (see ctrl* offsets) whose address
// has been installed as the import ctx via JobMemory.SetCustomCtx.
func (e *Engine) CallWithHost(code uintptr, serArgs, linMem, trap, results, ctrl []byte, host HostFunc) error {
	installTrapCell(linMem, trap)
	const maxReentries = 1 << 20
	for i := 0; i < maxReentries; i++ {
		enterNative(code, slicePtr(serArgs), slicePtr(linMem), slicePtr(trap), slicePtr(results), e.stackTop)
		switch tc := binary.LittleEndian.Uint32(trap); {
		case tc == hostCallPending:
			arg := binary.LittleEndian.Uint32(ctrl[ctrlArg:])
			binary.LittleEndian.PutUint32(ctrl[ctrlRet:], host(arg))
			// fall through the loop to re-enter and resume native code
		case tc != 0:
			return &TrapError{Code: TrapCode(tc)}
		default:
			return nil
		}
	}
	return fmt.Errorf("jit: host re-entry limit exceeded")
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
