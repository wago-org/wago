//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"sync/atomic"
	"unsafe"
)

// loadTrap and storeTrap are shared by the synchronous engine and every host
// interruption implementation. The trap buffer is mmap/VirtualAlloc-backed and
// therefore stable while native code may hold its address.
func loadTrap(trap []byte) uint32 {
	if len(trap) < 4 {
		return 0
	}
	return atomic.LoadUint32((*uint32)(unsafe.Pointer(&trap[0])))
}

func storeTrap(trap []byte, value uint32) {
	if len(trap) >= 4 {
		atomic.StoreUint32((*uint32)(unsafe.Pointer(&trap[0])), value)
	}
}
