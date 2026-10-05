//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"fmt"
	"sync"
)

// MaxSerializedParams bounds this experimental integer calling convention.
const MaxSerializedParams = 256

// CallFrame owns serialized eight-byte argument cells followed by one result
// cell and one hidden context cell. The guest receives argument/result addresses
// in a0/a1; no Go heap pointer crosses the native boundary. Calls and Close on
// this frame serialize.
type CallFrame struct {
	mu       sync.Mutex
	mem      []byte
	capacity int
	closed   bool
	ops      *memoryOps
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
	return &CallFrame{mem: mem, capacity: capacity, ops: ops}, nil
}

// Call uses the first len(args) cells and the fixed result cell at capacity*8.
// The caller validates function arity; this layer checks storage capacity.
// Guest code must write all eight result bytes, including normalized i32 bits.
func (f *CallFrame) Call(e *Engine, p *Program, offset int, args []uint64, frameBytes int) (uint64, error) {
	return f.CallWithContext(e, p, offset, args, frameBytes, 0)
}

// CallWithContext stores a compiler-owned off-heap context address after the
// fixed result cell. Its lifetime must cover this call. ABI0 entry is unchanged.
func (f *CallFrame) CallWithContext(e *Engine, p *Program, offset int, args []uint64, frameBytes int, context uint64) (uint64, error) {
	if f == nil {
		return 0, ErrClosed
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed || len(f.mem) == 0 {
		return 0, ErrClosed
	}
	if len(args) > f.capacity {
		return 0, fmt.Errorf("rv64: too many serialized arguments")
	}
	for i, v := range args {
		binary.LittleEndian.PutUint64(f.mem[i*8:], v)
	}
	result := f.mem[f.capacity*8:]
	clear(result[:8])
	binary.LittleEndian.PutUint64(result[8:], context)
	if _, err := e.Call(p, offset, uint64(pointer(f.mem)), uint64(pointer(result)), frameBytes); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(result), nil
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
