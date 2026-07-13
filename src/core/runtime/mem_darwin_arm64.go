//go:build darwin && arm64

package runtime

import (
	"sync"
	"syscall"
)

const pageSize = 16 << 10

func roundUpPage(n int) int {
	if n <= 0 {
		return pageSize
	}
	return (n + pageSize - 1) &^ (pageSize - 1)
}

func mmapRW(n int) ([]byte, error) {
	return syscall.Mmap(-1, 0, roundUpPage(n),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_ANON|syscall.MAP_PRIVATE)
}

// mmapCodeRW uses MAP_JIT for memory that will later become executable on
// hardened Apple Silicon systems.
func mmapCodeRW(n int) ([]byte, error) {
	return syscall.Mmap(-1, 0, roundUpPage(n),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_ANON|syscall.MAP_PRIVATE|syscall.MAP_JIT)
}

// mmapRWReserve reserves a stable growable-memory address range. Darwin has no
// MAP_NORESERVE equivalent in Go's syscall surface, so the explicit-bounds path
// uses a normal private anonymous mapping and keeps memory.grow as a size-cache
// update over that fixed reservation.
func mmapRWReserve(n int) ([]byte, error) {
	return mmapRW(n)
}

// mmapExec maps executable code on Apple Silicon. MAP_JIT is required on many
// hardened macOS configurations for JIT mappings; the mapping is writable only
// during the copy and then flipped to RX to preserve W^X at the syscall level.
func mmapExec(code []byte) ([]byte, error) {
	mem, err := syscall.Mmap(-1, 0, roundUpPage(len(code)),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_ANON|syscall.MAP_PRIVATE|syscall.MAP_JIT)
	if err != nil {
		return nil, err
	}
	copy(mem, code)
	if err := syscall.Mprotect(mem, syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		_ = syscall.Munmap(mem)
		return nil, err
	}
	return mem[:len(code):len(code)], nil
}

func munmap(b []byte) error {
	if b == nil {
		return nil
	}
	return syscall.Munmap(b)
}

// Arena is a bump allocator over stable off-heap memory.
type Arena struct {
	mem         []byte
	off         int
	zeroOnAlloc bool
}

func NewArena(n int) (*Arena, error) {
	mem, err := mmapRW(n)
	if err != nil {
		return nil, err
	}
	return &Arena{mem: mem}, nil
}

var arenaCache struct {
	sync.Mutex
	a *Arena
}

// AcquireArena returns an arena of at least n bytes, reusing one recently
// released by ReleaseArena when possible. The cache is a single mapping bounded
// by InstantiateArenaSize so short instantiate/close loops avoid mmap churn
// without retaining unbounded off-heap memory.
func AcquireArena(n int) (*Arena, error) {
	need := roundUpPage(n)
	arenaCache.Lock()
	a := arenaCache.a
	if a != nil && len(a.mem) >= need {
		arenaCache.a = nil
		arenaCache.Unlock()
		a.off = 0
		a.zeroOnAlloc = true
		return a, nil
	}
	if a != nil && len(a.mem) < need {
		arenaCache.a = nil
		arenaCache.Unlock()
		_ = a.Close()
		return NewArena(n)
	}
	arenaCache.Unlock()
	return NewArena(n)
}

func (a *Arena) Alloc(n int) []byte {
	b := a.AllocNoZero(n)
	if a.zeroOnAlloc {
		clear(b)
	}
	return b
}

// AllocNoZero is Alloc without the reused-arena zero-fill. The returned bytes may
// contain stale data from a prior instance, so the caller MUST fully initialize
// them (or otherwise not read them) before use.
func (a *Arena) AllocNoZero(n int) []byte {
	a.off = (a.off + 7) &^ 7
	if a.off+n > len(a.mem) {
		panic("jit: arena out of memory")
	}
	b := a.mem[a.off : a.off+n : a.off+n]
	a.off += n
	return b
}

func (a *Arena) Close() error { return munmap(a.mem) }

// ReleaseArena returns a to the bounded cache or unmaps it if the cache is
// occupied. Reused arenas zero each allocation before it is handed out, matching
// the fresh-anonymous-mmap behavior callers depend on for sparse table entries.
func ReleaseArena(a *Arena) error {
	if a == nil {
		return nil
	}
	if len(a.mem) > roundUpPage(InstantiateArenaSize) {
		return a.Close()
	}
	arenaCache.Lock()
	if arenaCache.a == nil {
		a.off = 0
		a.zeroOnAlloc = true
		arenaCache.a = a
		arenaCache.Unlock()
		return nil
	}
	arenaCache.Unlock()
	return a.Close()
}
