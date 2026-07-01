//go:build linux && amd64 && wago_guardpage

package runtime

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Guard-page linear memory (EXPERIMENTAL). Instead of an exactly-sized RW
// mapping with explicit per-access bounds checks, reserve a large PROT_NONE
// region and commit only the used pages. A wasm access reaches
// linMem + addr(≤4 GiB-1) + offset(≤4 GiB-1) + size, so the reservation covers
// the full 8 GiB an in-range *or* out-of-range wasm32 access can name; an
// out-of-range address lands on a PROT_NONE page and faults. The SIGSEGV/SIGBUS
// handler (sigtrap_linux_amd64.go) turns that fault into a wasm trap, so the
// JIT can omit the inline check entirely (amd64.ElideBoundsChecks).
const (
	maxLinMemBytes   = uintptr(1) << 32               // 4 GiB: max wasm32 linear memory
	offsetGuardBytes = (uintptr(1) << 32) + (1 << 16) // 4 GiB + 64 KiB: max memarg offset reach
)

// guardReserveBytes is the total virtual reservation per guarded memory.
var guardReserveBytes = uintptr(roundUpPage(int(uintptr(basedataSize) + maxLinMemBytes + offsetGuardBytes)))

// wasmPageBytes is the wasm linear-memory page size (64 KiB).
const wasmPageBytes = 1 << 16

// NewJobMemoryGuarded lays out [ basedata | linear memory ] inside a large
// PROT_NONE reservation, committing (RW) only basedata + the initial linear
// pages. The bytes beyond stay PROT_NONE and fault on access; the trap handler
// lazily commits pages within the (grown) logical size and traps only on a
// genuinely out-of-range address. memory.grow may raise the logical size up to
// maxBytes without any remap. Pair with InstallGuardTrapHandler and
// amd64.ElideBoundsChecks.
func NewJobMemoryGuarded(linBytes, maxBytes int) (*JobMemory, error) {
	// Place linMem on a page boundary (basedata sits in the page just below it) so
	// that, because wasm linear memory is always a multiple of the 64 KiB wasm
	// page, linMem+linBytes lands exactly on a guard page. An access at offset
	// linBytes then faults — page-granular trapping is byte-exact for wasm.
	linOff := roundUpPage(basedataSize)
	commit := uintptr(linOff + linBytes)
	base, _, errno := syscall.Syscall6(syscall.SYS_MMAP, 0, guardReserveBytes,
		syscall.PROT_NONE, syscall.MAP_ANON|syscall.MAP_PRIVATE|syscall.MAP_NORESERVE,
		^uintptr(0), 0)
	if errno != 0 {
		return nil, fmt.Errorf("guard mmap reserve: %w", errno)
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_MPROTECT, base, commit,
		syscall.PROT_READ|syscall.PROT_WRITE); errno != 0 {
		_, _, _ = syscall.Syscall(syscall.SYS_MUNMAP, base, guardReserveBytes, 0)
		return nil, fmt.Errorf("guard mprotect commit: %w", errno)
	}
	mem := unsafe.Slice((*byte)(unsafe.Pointer(base)), commit)
	j := &JobMemory{
		mem:         mem,
		linOff:      linOff,
		linLen:      linBytes,
		reserveBase: base,
		reserveLen:  guardReserveBytes,
	}
	j.putU32(offActualLinMemByteSize, uint32(linBytes))
	j.putU32(offLinMemWasmSize, uint32(linBytes/wasmPageBytes))
	// Grow ceiling: memory.grow raises the logical size (and thus the region the
	// fault handler will commit-on-demand) up to maxBytes. Cap at 65535 pages,
	// since 65536 pages (4 GiB) overflows the u32 byte-size cache.
	maxPages := maxBytes / wasmPageBytes
	if maxPages > 65535 {
		maxPages = 65535
	}
	if maxPages < linBytes/wasmPageBytes {
		maxPages = linBytes / wasmPageBytes
	}
	j.putU32(offMaxLinMemPages, uint32(maxPages))
	if err := registerGuardRegion(base, base+guardReserveBytes, base+uintptr(linOff)); err != nil {
		_, _, _ = syscall.Syscall(syscall.SYS_MUNMAP, base, guardReserveBytes, 0)
		return nil, err
	}
	return j, nil
}
