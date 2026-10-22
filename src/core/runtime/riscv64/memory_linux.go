//go:build linux && !tinygo

// Package riscv64 is an experimental integer-only native runtime boundary.
// It does not enable a Wago backend, artifact target, host ABI, or Wasm feature.
package riscv64

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"github.com/wago-org/wago/src/core/codeimage"
)

var (
	ErrClosed      = errors.New("rv64: native owner is closed")
	ErrStackBounds = errors.New("rv64: foreign stack bounds exceeded")
	ErrUnavailable = errors.New("rv64: native entry requires Linux/RV64 standard Go")
)

// LimitCodeBytes bounds this checkpoint's logical staging image independently
// of capacity estimates. It is not a Wasm module-admission or product limit.
const LimitCodeBytes = 1 << 30

type memoryOps struct {
	mapRW     func(int) ([]byte, error)
	unmap     func([]byte) error
	protectRX func([]byte) error
	flush     func([]byte, int) error
}

var linuxMemory = memoryOps{
	mapRW:     mapRW,
	unmap:     syscall.Munmap,
	protectRX: func(mem []byte) error { return syscall.Mprotect(mem, syscall.PROT_READ|syscall.PROT_EXEC) },
	flush:     flushICache,
}

func mapRW(n int) ([]byte, error) {
	if n <= 0 || n > LimitCodeBytes {
		return nil, fmt.Errorf("rv64: mapping size %d is outside 1..%d", n, LimitCodeBytes)
	}
	page := syscall.Getpagesize()
	n = (n + page - 1) / page * page
	return syscall.Mmap(-1, 0, n, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
}

func pointer(mem []byte) uintptr {
	if len(mem) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&mem[0]))
}

// CodeBuffer is an exclusively owned, bounded RW staging image. Growing moves
// its mapping, invalidating borrowed Bytes slices. Take transfers it once.
// Like core/runtime.CodeBuffer, this builder is not safe for concurrent use.
type CodeBuffer struct {
	mem      []byte
	extra    []byte // retained cleanup ownership after a failed growth transaction
	n, limit int
	closed   bool
	failed   error
	ops      *memoryOps
}

var _ codeimage.Image = (*CodeBuffer)(nil)

func NewCodeBuffer(capacity, limit int) (*CodeBuffer, error) {
	return newCodeBuffer(capacity, limit, &linuxMemory)
}

func newCodeBuffer(capacity, limit int, ops *memoryOps) (*CodeBuffer, error) {
	if limit < 1 || limit > LimitCodeBytes || capacity < 0 || capacity > limit {
		return nil, fmt.Errorf("rv64: invalid code capacity/limit %d/%d", capacity, limit)
	}
	mem, err := ops.mapRW(max(1, capacity))
	if err != nil {
		return nil, err
	}
	return &CodeBuffer{mem: mem, limit: limit, ops: ops}, nil
}

func (b *CodeBuffer) Append(code []byte) error {
	if b == nil || b.closed {
		return ErrClosed
	}
	if b.failed != nil {
		return b.failed
	}
	if len(code) > b.limit-b.n {
		return fmt.Errorf("rv64: code image exceeds %d-byte limit", b.limit)
	}
	need := b.n + len(code)
	if need > len(b.mem) {
		capacity := min(b.limit, max(need, len(b.mem)*2))
		mem, err := b.ops.mapRW(capacity)
		if err != nil {
			return err
		}
		// Copy the append source before retiring the old mapping: it may alias
		// any writable bytes in that mapping, including beyond the logical end.
		copy(mem, b.mem[:b.n])
		copy(mem[b.n:], code)
		if err := b.ops.unmap(b.mem); err != nil {
			cleanup := b.ops.unmap(mem)
			if cleanup != nil {
				b.extra = mem
			}
			b.failed = errors.Join(err, cleanup)
			return b.failed
		}
		b.mem = mem
	} else {
		copy(b.mem[b.n:], code)
	}
	b.n = need
	return nil
}

func (b *CodeBuffer) Bytes() []byte {
	if b == nil || b.closed {
		return nil
	}
	return b.mem[:b.n:b.n]
}

func (b *CodeBuffer) Take() ([]byte, uintptr, error) {
	if b == nil || b.closed {
		return nil, 0, ErrClosed
	}
	if b.failed != nil {
		return nil, 0, b.failed
	}
	mem := b.mem
	b.mem, b.n, b.closed = nil, 0, true
	return mem, pointer(mem), nil
}

// Close prevents further use immediately. A failed unmap retains ownership and
// a subsequent Close retries cleanup, including both mappings after failed growth.
func (b *CodeBuffer) Close() error {
	if b == nil {
		return nil
	}
	b.closed = true
	var first, second error
	if len(b.mem) != 0 {
		first = b.ops.unmap(b.mem)
		if first == nil {
			b.mem = nil
		}
	}
	if len(b.extra) != 0 {
		second = b.ops.unmap(b.extra)
		if second == nil {
			b.extra = nil
		}
	}
	return errors.Join(first, second)
}

// Program owns a sealed executable image. Entry pointers are kept private:
// Engine.Call holds an invocation reservation until after native return.
type Program struct {
	mu     sync.RWMutex
	mem    []byte
	n      int
	closed bool
	ops    *memoryOps
}

// PublishImage consumes the producer's unsealed mapping via the current
// codeimage.Image contract. RX protection and process-wide cache synchronization
// complete before an executable owner becomes visible. codeBytes is logical size.
func PublishImage(image codeimage.Image, codeBytes int) (*Program, error) {
	return publishImage(image, codeBytes, &linuxMemory)
}

func publishImage(image codeimage.Image, codeBytes int, ops *memoryOps) (*Program, error) {
	if image == nil {
		return nil, fmt.Errorf("rv64: nil code image")
	}
	if codeBytes < 4 || codeBytes&3 != 0 || codeBytes > LimitCodeBytes {
		return nil, errors.Join(fmt.Errorf("rv64: invalid fixed-width code length %d", codeBytes), image.Close())
	}
	mem, base, err := image.Take()
	if err != nil {
		return nil, errors.Join(err, image.Close())
	}
	if codeBytes > len(mem) || base == 0 || base != pointer(mem) || base&3 != 0 {
		err = fmt.Errorf("rv64: invalid transferred code mapping")
	} else if err = ops.protectRX(mem); err == nil {
		err = ops.flush(mem, codeBytes)
	}
	if err != nil {
		if len(mem) != 0 {
			if cleanup := ops.unmap(mem); cleanup != nil {
				// Return a closed owner solely for retryable cleanup; Call rejects it.
				return &Program{mem: mem, n: codeBytes, closed: true, ops: ops}, errors.Join(err, cleanup)
			}
		}
		return nil, err
	}
	return &Program{mem: mem, n: codeBytes, ops: ops}, nil
}

// MapCode copies code and publishes it through the same transfer/seal path.
// On error a nonnil Program is closed and owns only failed cleanup; Close it.
func MapCode(code []byte) (*Program, error) {
	if len(code) < 4 || len(code)&3 != 0 {
		return nil, fmt.Errorf("rv64: invalid code length %d", len(code))
	}
	b, err := NewCodeBuffer(len(code), len(code))
	if err != nil {
		return nil, err
	}
	if err = b.Append(code); err != nil {
		return nil, errors.Join(err, b.Close())
	}
	return PublishImage(b, len(code))
}

// Close waits for invocation quiescence. Once retirement starts, no new Call
// can enter, even when unmapping fails and a later Close must retry it.
func (p *Program) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if len(p.mem) == 0 {
		return nil
	}
	err := p.ops.unmap(p.mem)
	if err == nil {
		p.mem = nil
	}
	return err
}
