//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

const (
	DefaultNativeStackBytes = 4 << 20
	MinNativeStackBytes     = 512 << 10
	MaxNativeStackBytes     = 1 << 30
	stackFenceMargin        = 256 << 10
	bridgeSaveBytes         = unsafe.Sizeof(nativeSaveFrame{})
	trapBufferBytes         = 24 // current core/runtime TrapBufferBytes; no host frame
)

// go_asm.h derives the assembly save offsets from this integer-only layout.
// S0/S1 are x8/x9; S2..S9 are x18..x25; CTXT/G are x26/x27 (psABI S10/S11).
type nativeSaveFrame struct {
	GoSP, RA, GP, TP                       uint64
	S0, S1, S2, S3, S4, S5, S6, S7, S8, S9 uint64
	CTXT, G                                uint64
}

type entryFunc func(code, a0, a1, low, top, frame, trap uintptr) (uint64, uint64)

// Engine owns an off-heap foreign stack and trap cell. Calls on one Engine are
// serialized. Closing an Engine waits for its current invocation to return.
type Engine struct {
	mu          sync.Mutex
	stack, trap []byte
	low, top    uintptr
	closed      bool
	ops         *memoryOps
	invoke      entryFunc
}

func NewEngine(stackBytes int) (*Engine, error) {
	if !nativeAvailable {
		return nil, ErrUnavailable
	}
	return newEngine(stackBytes, nativeCall, &linuxMemory)
}

func newEngine(stackBytes int, invoke entryFunc, ops *memoryOps) (*Engine, error) {
	if stackBytes < MinNativeStackBytes || stackBytes > MaxNativeStackBytes || stackBytes&15 != 0 {
		return nil, fmt.Errorf("rv64: invalid aligned foreign stack capacity %d", stackBytes)
	}
	stack, err := ops.mapRW(stackBytes)
	if err != nil {
		return nil, err
	}
	trap, err := ops.mapRW(trapBufferBytes)
	if err != nil {
		if cleanup := ops.unmap(stack); cleanup != nil {
			return &Engine{stack: stack, closed: true, ops: ops}, errors.Join(err, cleanup)
		}
		return nil, err
	}
	base := pointer(stack)
	return &Engine{stack: stack, trap: trap, low: base + stackFenceMargin,
		top: (base + uintptr(stackBytes)) &^ 15, ops: ops, invoke: invoke}, nil
}

// TrapError reports the code written to the stable off-heap trap cell by a
// snippet. Source locations and current Wago trap/cancellation APIs are not wired.
type TrapError struct{ Code uint32 }

func (e *TrapError) Error() string { return fmt.Sprintf("rv64: native snippet trap %d", e.Code) }

// Call invokes a trusted, statically bounded integer snippet at a byte offset.
// The experimental guest ABI is a0/a1 inputs, a0 result, a2 trap-cell pointer,
// a3 stack fence. The snippet must preserve SP/RA and must not change gp/tp/g.
// frameBytes is its proven entry frame (at least 16, aligned to 16). Any nested
// guest frame must independently guard available stack before SP changes/writes;
// the entry-frame check alone does not bound transitive or recursive descent.
// This is not admission of arbitrary Wasm or untrusted native machine code.
func (e *Engine) Call(p *Program, offset int, a0, a1 uint64, frameBytes int) (uint64, error) {
	if e == nil || p == nil {
		return 0, ErrClosed
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return 0, ErrClosed
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return 0, ErrClosed
	}
	if offset < 0 || offset&3 != 0 || offset > p.n-4 {
		return 0, fmt.Errorf("rv64: invalid entry offset %d", offset)
	}
	available := e.top - e.low
	if frameBytes < 16 || frameBytes&15 != 0 || available < bridgeSaveBytes || uintptr(frameBytes) > available-bridgeSaveBytes {
		return 0, ErrStackBounds
	}
	clear(e.trap[:trapBufferBytes])
	result, status := e.invoke(pointer(p.mem)+uintptr(offset), uintptr(a0), uintptr(a1),
		e.low, e.top, uintptr(frameBytes), pointer(e.trap))
	runtime.KeepAlive(p)
	runtime.KeepAlive(e)
	if status == 1 {
		return 0, ErrStackBounds
	}
	if status != 0 {
		return 0, ErrUnavailable
	}
	if code := binary.LittleEndian.Uint32(e.trap); code != 0 {
		return 0, &TrapError{Code: code}
	}
	return result, nil
}

// Close is retryable if a syscall fails. Closed engines reject further entry.
func (e *Engine) Close() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	var first, second error
	if len(e.stack) != 0 {
		first = e.ops.unmap(e.stack)
		if first == nil {
			e.stack = nil
		}
	}
	if len(e.trap) != 0 {
		second = e.ops.unmap(e.trap)
		if second == nil {
			e.trap = nil
		}
	}
	return errors.Join(first, second)
}
