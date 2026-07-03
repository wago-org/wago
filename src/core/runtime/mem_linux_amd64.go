//go:build linux && amd64

package runtime

import (
	"sync"
	"syscall"
)

const pageSize = 4096

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

// mmapRWReserve maps n bytes RW with MAP_NORESERVE: the address space is
// reserved and pages become readable/writable on first touch, but physical
// memory (and swap) is only consumed as pages are used. Used to back growable
// linear memory so memory.grow is a pure size-cache update with no remap.
func mmapRWReserve(n int) ([]byte, error) {
	return syscall.Mmap(-1, 0, roundUpPage(n),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_ANON|syscall.MAP_PRIVATE|syscall.MAP_NORESERVE)
}

// mmapExec uses W^X: allocate RW, copy, then flip to R-X.
func mmapExec(code []byte) ([]byte, error) {
	mem, err := mmapRW(len(code))
	if err != nil {
		return nil, err
	}
	copy(mem, code)
	if err := syscall.Mprotect(mem, syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		_ = syscall.Munmap(mem)
		return nil, err
	}
	return mem, nil
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
	a.off = (a.off + 7) &^ 7
	if a.off+n > len(a.mem) {
		panic("jit: arena out of memory")
	}
	b := a.mem[a.off : a.off+n : a.off+n]
	a.off += n
	if a.zeroOnAlloc {
		clear(b)
	}
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
