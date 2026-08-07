package runtime

import (
	"fmt"
	"unsafe"
)

// CodeBuffer owns a growable off-heap machine-code image. It is writable while
// the compiler appends and patches code, then permanently sealed executable.
// The zero value is not usable.
//
// CodeBuffer is deliberately not safe for concurrent use. Compilation and
// sealing transfer exclusive ownership between phases.
type CodeBuffer struct {
	mem        []byte
	n          int
	sealed     bool
	registered bool
	closed     bool
}

// NewCodeBuffer allocates an RW code image with at least capacity bytes.
func NewCodeBuffer(capacity int) (*CodeBuffer, error) {
	if capacity < 0 {
		return nil, fmt.Errorf("jit: negative code capacity %d", capacity)
	}
	mem, err := mmapCodeRW(capacity)
	if err != nil {
		return nil, err
	}
	return &CodeBuffer{mem: mem}, nil
}

// Append adds p to the image. A capacity underestimate grows the mapping
// geometrically instead of turning a valid Wasm module into a compile failure.
func (b *CodeBuffer) Append(p []byte) error {
	if err := b.grow(len(p)); err != nil {
		return err
	}
	copy(b.mem[b.n:], p)
	b.n += len(p)
	return nil
}

// AppendZeros adds n zero bytes, used for deterministic function alignment.
func (b *CodeBuffer) AppendZeros(n int) error {
	if n < 0 {
		return fmt.Errorf("jit: negative code padding %d", n)
	}
	if err := b.grow(n); err != nil {
		return err
	}
	clear(b.mem[b.n : b.n+n])
	b.n += n
	return nil
}

// Bytes returns the exact logical image. It is writable until Seal succeeds
// and read-only afterward. Callers must not retain it after Close.
func (b *CodeBuffer) Bytes() []byte {
	if b == nil || b.closed {
		return nil
	}
	return b.mem[:b.n:b.n]
}

// Mapping returns the opaque page-rounded mapping used for registration and
// unmapping. Most callers should use Bytes.
func (b *CodeBuffer) Mapping() []byte {
	if b == nil || b.closed {
		return nil
	}
	return b.mem
}

// Base returns the stable address of the code image.
func (b *CodeBuffer) Base() uintptr {
	if b == nil || b.closed || len(b.mem) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&b.mem[0]))
}

// Seal changes the complete mapping from RW to RX and registers it with the
// host interruption machinery. Sealing is idempotent.
func (b *CodeBuffer) Seal() error {
	if b == nil {
		return fmt.Errorf("jit: nil code buffer")
	}
	if b.closed {
		return fmt.Errorf("jit: code buffer is closed")
	}
	if b.sealed {
		return nil
	}
	if err := SealCode(b.mem); err != nil {
		_ = munmap(b.mem)
		b.mem = nil
		b.n = 0
		b.closed = true
		return err
	}
	b.sealed = true
	b.registered = true
	return nil
}

// Take transfers an unsealed mapping to another owner. It is used at the
// compiler/runtime boundary to keep Compiled's cache compact.
func (b *CodeBuffer) Take() ([]byte, uintptr, error) {
	if b == nil {
		return nil, 0, fmt.Errorf("jit: nil code buffer")
	}
	if b.closed {
		return nil, 0, fmt.Errorf("jit: code buffer is closed")
	}
	if b.sealed {
		return nil, 0, fmt.Errorf("jit: sealed code buffer cannot transfer ownership")
	}
	mem, base := b.mem, b.Base()
	b.mem = nil
	b.n = 0
	b.closed = true
	return mem, base, nil
}

// Close releases the mapping. Callers must ensure no instance can still enter
// the image.
func (b *CodeBuffer) Close() error {
	if b == nil || b.closed {
		return nil
	}
	if b.registered {
		unregisterExecutableCode(b.mem)
	}
	mem := b.mem
	b.mem = nil
	b.n = 0
	b.closed = true
	return munmap(mem)
}

func (b *CodeBuffer) grow(extra int) error {
	if b == nil {
		return fmt.Errorf("jit: nil code buffer")
	}
	if b.closed {
		return fmt.Errorf("jit: code buffer is closed")
	}
	if b.sealed {
		return fmt.Errorf("jit: code buffer is sealed")
	}
	if extra < 0 || b.n > int(^uint(0)>>1)-extra {
		return fmt.Errorf("jit: code image size overflow")
	}
	need := b.n + extra
	if need <= len(b.mem) {
		return nil
	}
	capacity := len(b.mem) * 2
	if capacity < need {
		capacity = need
	}
	mem, err := mmapCodeRW(capacity)
	if err != nil {
		return err
	}
	copy(mem, b.mem[:b.n])
	old := b.mem
	b.mem = mem
	if err := munmap(old); err != nil {
		_ = munmap(mem)
		b.mem = nil
		b.n = 0
		b.closed = true
		return err
	}
	return nil
}
