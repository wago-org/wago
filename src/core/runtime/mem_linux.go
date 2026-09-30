//go:build linux && (amd64 || arm64)

package runtime

import (
	"sync"
	"syscall"
	"unsafe"
)

const pageSize = 4096

// madviseDontNeed drops the physical pages backing b (a private anonymous
// mapping), so the range reads back as zero on next access without being
// unmapped. Used to zero-reclaim a reused reservation's dirtied region cheaply,
// avoiding a full clear() of a possibly multi-GiB reservation.
func madviseDontNeed(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_MADVISE,
		uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), syscall.MADV_DONTNEED); errno != 0 {
		return errno
	}
	return nil
}

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

func mmapCodeRW(n int) ([]byte, error) { return mmapRW(n) }

func protectCodeRX(mem []byte) error {
	return syscall.Mprotect(mem, syscall.PROT_READ|syscall.PROT_EXEC)
}

// mmapExec uses W^X: allocate RW, copy, then flip to R-X.
func mmapExec(code []byte) ([]byte, error) {
	mem, err := mmapRW(len(code))
	if err != nil {
		return nil, err
	}
	copy(mem, code)
	if err := protectCodeRX(mem); err != nil {
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
	mem []byte
	off int
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
// by InstantiateArenaCacheBytes so short instantiate/close loops avoid mmap churn
// without retaining unbounded off-heap memory.
func AcquireArena(n int) (*Arena, error) {
	need := roundUpPage(n)
	arenaCache.Lock()
	a := arenaCache.a
	if a != nil && len(a.mem) >= need {
		arenaCache.a = nil
		arenaCache.Unlock()
		a.off = 0
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
	return a.AllocNoZero(n)
}

// AllocNoZero skips explicit zeroing. Callers must fully initialize the bytes
// before use. This is for buffers that native or host code writes before reading.
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
// occupied. All allocations and borrowed views must be released first. A
// successful private-anonymous MADV_DONTNEED guarantees zero-fill on reuse;
// failure closes the mapping so stale bytes cannot be reused.
func ReleaseArena(a *Arena) error {
	if a == nil {
		return nil
	}
	if len(a.mem) > roundUpPage(InstantiateArenaCacheBytes) {
		return a.Close()
	}
	arenaCache.Lock()
	if arenaCache.a == nil {
		if err := madviseDontNeed(a.mem); err != nil {
			arenaCache.Unlock()
			return a.Close()
		}
		arenaCache.a = a
		arenaCache.Unlock()
		return nil
	}
	arenaCache.Unlock()
	return a.Close()
}
