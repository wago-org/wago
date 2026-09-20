//go:build darwin && (amd64 || arm64) && !tinygo

package runtime

import (
	"runtime"
	"syscall"
	"unsafe"
)

const madvZero = 11

func madviseDontNeed(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	// MADV_ZERO reports success under Rosetta but leaves Intel pages intact.
	// Clear there to preserve the allocator's zero-on-reuse contract. Native
	// Apple Silicon uses the lazy kernel operation below.
	if runtime.GOARCH == "amd64" {
		clear(b)
		return nil
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_MADVISE,
		uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), madvZero); errno == 0 {
		return nil
	}
	// Darwin may reject MADV_ZERO after the process has forked. MADV_DONTNEED
	// does not guarantee that a private anonymous mapping reads back as zero, so
	// clear explicitly to preserve the allocator's zero-on-reuse contract.
	clear(b)
	return nil
}
