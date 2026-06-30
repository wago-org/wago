//go:build linux && amd64

package runtime

import "syscall"

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

func (a *Arena) Alloc(n int) []byte {
	a.off = (a.off + 7) &^ 7
	if a.off+n > len(a.mem) {
		panic("jit: arena out of memory")
	}
	b := a.mem[a.off : a.off+n : a.off+n]
	a.off += n
	return b
}

func (a *Arena) Close() error { return munmap(a.mem) }
