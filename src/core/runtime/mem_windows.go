//go:build windows && (amd64 || arm64)

package runtime

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

const pageSize = 4096

const (
	memCommit       = 0x1000
	memReserve      = 0x2000
	memDecommit     = 0x4000
	memRelease      = 0x8000
	pageReadWrite   = 0x04
	pageExecuteRead = 0x20
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procVirtualAlloc          = kernel32.NewProc("VirtualAlloc")
	procVirtualFree           = kernel32.NewProc("VirtualFree")
	procVirtualProtect        = kernel32.NewProc("VirtualProtect")
	procFlushInstructionCache = kernel32.NewProc("FlushInstructionCache")
	procGetCurrentProcess     = kernel32.NewProc("GetCurrentProcess")
)

func roundUpPage(n int) int {
	if n <= 0 {
		return pageSize
	}
	return (n + pageSize - 1) &^ (pageSize - 1)
}

func virtualAlloc(address uintptr, size int, allocationType, protection uintptr) ([]byte, error) {
	ptr, _, callErr := procVirtualAlloc.Call(address, uintptr(size), allocationType, protection)
	if ptr == 0 {
		return nil, fmt.Errorf("VirtualAlloc(%d bytes): %w", size, callErr)
	}
	return unsafe.Slice((*byte)(offHeapPointer(ptr)), size), nil
}

func mmapRW(n int) ([]byte, error) {
	n = roundUpPage(n)
	return virtualAlloc(0, n, memReserve|memCommit, pageReadWrite)
}

// mmapRWReserve keeps the complete linear-memory range at a stable address.
// Windows commits the reservation up front so the returned byte slice is safe
// for Go host access; committed untouched pages remain demand-zero and do not
// consume physical memory until touched.
func mmapRWReserve(n int) ([]byte, error) { return mmapRW(n) }

func mmapExec(code []byte) ([]byte, error) {
	mem, err := mmapRW(len(code))
	if err != nil {
		return nil, err
	}
	copy(mem, code)
	var oldProtect uintptr
	ok, _, callErr := procVirtualProtect.Call(
		uintptr(unsafe.Pointer(&mem[0])), uintptr(len(mem)), pageExecuteRead,
		uintptr(unsafe.Pointer(&oldProtect)),
	)
	if ok == 0 {
		_ = munmap(mem)
		return nil, fmt.Errorf("VirtualProtect(RX): %w", callErr)
	}
	process, _, _ := procGetCurrentProcess.Call()
	ok, _, callErr = procFlushInstructionCache.Call(
		process, uintptr(unsafe.Pointer(&mem[0])), uintptr(len(mem)),
	)
	if ok == 0 {
		_ = munmap(mem)
		return nil, fmt.Errorf("FlushInstructionCache: %w", callErr)
	}
	return mem, nil
}

func munmap(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	ok, _, callErr := procVirtualFree.Call(
		uintptr(unsafe.Pointer(&b[0])), 0, memRelease,
	)
	if ok == 0 {
		return fmt.Errorf("VirtualFree: %w", callErr)
	}
	return nil
}

func madviseDontNeed(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	address := uintptr(unsafe.Pointer(&b[0]))
	ok, _, callErr := procVirtualFree.Call(address, uintptr(len(b)), memDecommit)
	if ok == 0 {
		return fmt.Errorf("VirtualFree(MEM_DECOMMIT): %w", callErr)
	}
	ptr, _, callErr := procVirtualAlloc.Call(address, uintptr(len(b)), memCommit, pageReadWrite)
	if ptr != address {
		return fmt.Errorf("VirtualAlloc(MEM_COMMIT): %w", callErr)
	}
	return nil
}

func munmapRange(base, _ uintptr) error {
	if base == 0 {
		return nil
	}
	ok, _, callErr := procVirtualFree.Call(base, 0, memRelease)
	if ok == 0 {
		return fmt.Errorf("VirtualFree: %w", callErr)
	}
	return nil
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
	if a != nil {
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
