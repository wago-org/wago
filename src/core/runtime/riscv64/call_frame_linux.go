//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"fmt"
	"sync"
)

// MaxSerializedParams bounds this experimental integer calling convention.
const MaxSerializedParams = 256

const MaxSerializedResults = 256

// CallFrame owns serialized eight-byte argument/result cells and a context
// cell. The guest receives argument/result addresses in a0/a1; no Go heap pointer
// crosses the native boundary. Calls and Close on this frame serialize. The
// legacy single-result and new fixed-header layouts have separate constructors.
type CallFrame struct {
	mu        sync.Mutex
	mem       []byte
	capacity  int
	results   int
	argOffset int
	closed    bool
	ops       *memoryOps
}

func NewCallFrame(capacity int) (*CallFrame, error) {
	return newCallFrame(capacity, &linuxMemory)
}

func newCallFrame(capacity int, ops *memoryOps) (*CallFrame, error) {
	if capacity < 0 || capacity > MaxSerializedParams {
		return nil, fmt.Errorf("rv64: serialized parameter capacity outside 0..256")
	}
	// Preserve argument/result offsets; one hidden context cell follows result.
	mem, err := ops.mapRW((capacity + 2) * 8)
	if err != nil {
		return nil, err
	}
	return &CallFrame{mem: mem, capacity: capacity, results: 1, ops: ops}, nil
}

// NewCallFrameLayout owns separate argument and result ranges and a fixed
// context header immediately before the argument range. This integer-only
// staging ABI is a prerequisite to the production native wrapper integration.
// Capacities are resource bounds, not the arity of an individual invocation.
func NewCallFrameLayout(params, results int) (*CallFrame, error) {
	return newCallFrameLayout(params, results, &linuxMemory)
}

func newCallFrameLayout(params, results int, ops *memoryOps) (*CallFrame, error) {
	if params < 0 || params > MaxSerializedParams || results < 0 || results > MaxSerializedResults {
		return nil, fmt.Errorf("rv64: serialized capacities outside 0..256")
	}
	// Reserve one result cell even for void modules so a1 is always in mapping.
	results = max(1, results)
	mem, err := ops.mapRW((1 + params + results) * 8)
	if err != nil {
		return nil, err
	}
	return &CallFrame{mem: mem, capacity: params, results: results, argOffset: 8, ops: ops}, nil
}

func (f *CallFrame) Call(e *Engine, p *Program, offset int, args []uint64, frameBytes int) (uint64, error) {
	return f.CallWithContext(e, p, offset, args, frameBytes, 0)
}

func (f *CallFrame) CallWithContext(e *Engine, p *Program, offset int, args []uint64, frameBytes int, context uint64) (uint64, error) {
	var result [1]uint64
	err := f.CallIntoWithContext(e, p, offset, args, result[:], frameBytes, context)
	return result[0], err
}

// CallIntoWithContext copies guest results into caller-owned storage only after
// successful completion. No Go result pointer crosses the native boundary.
// The caller validates function arity; this layer validates arena capacities
// before changing any cells. The entire result range is cleared before entry.
func (f *CallFrame) CallIntoWithContext(e *Engine, p *Program, offset int, args, results []uint64, frameBytes int, context uint64) error {
	if f == nil {
		return ErrClosed
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed || len(f.mem) == 0 {
		return ErrClosed
	}
	if len(args) > f.capacity || len(results) > f.results {
		return fmt.Errorf("rv64: serialized argument/result capacity exceeded")
	}
	arguments := f.mem[f.argOffset:]
	for i, v := range args {
		binary.LittleEndian.PutUint64(arguments[i*8:], v)
	}
	result := arguments[f.capacity*8:]
	clear(result[:f.results*8])
	if f.argOffset != 0 {
		binary.LittleEndian.PutUint64(f.mem, context)
	} else {
		// Keep the old single-result diagnostic ABI for standalone snippets.
		binary.LittleEndian.PutUint64(result[8:], context)
	}
	if _, err := e.Call(p, offset, uint64(pointer(arguments)), uint64(pointer(result)), frameBytes); err != nil {
		return err
	}
	for i := range results {
		results[i] = binary.LittleEndian.Uint64(result[i*8:])
	}
	return nil
}

func (f *CallFrame) Close() error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	if len(f.mem) == 0 {
		return nil
	}
	err := f.ops.unmap(f.mem)
	if err == nil {
		f.mem = nil
	}
	return err
}
