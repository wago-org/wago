//go:build linux && amd64 && wago_guardpage

package runtime

import (
	"fmt"
	"sync"
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
// maxBytes without any remap. Pair with InstallGuardTrapHandler and code
// compiled in signals-based bounds mode (the amd64 backend's guard mode, which
// elides the inline bounds checks and relies on the guard-page fault instead).
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
	j.putGuardedSizeCaches(linBytes, maxBytes)
	if err := registerGuardRegion(base, base+guardReserveBytes, base+uintptr(linOff)); err != nil {
		_, _, _ = syscall.Syscall(syscall.SYS_MUNMAP, base, guardReserveBytes, 0)
		return nil, err
	}
	return j, nil
}

// putGuardedSizeCaches writes the three basedata size caches native code reads:
// the current byte size, the current wasm-page count, and the grow ceiling.
// memory.grow raises the logical size (and thus the region the fault handler
// commits on demand) up to maxBytes; cap at 65535 pages, since 65536 pages
// (4 GiB) overflows the u32 byte-size cache.
func (j *JobMemory) putGuardedSizeCaches(linBytes, maxBytes int) {
	j.putU32(offActualLinMemByteSize, uint32(linBytes))
	j.putU32(offLinMemWasmSize, uint32(linBytes/wasmPageBytes))
	maxPages := maxBytes / wasmPageBytes
	if maxPages > 65535 {
		maxPages = 65535
	}
	if maxPages < linBytes/wasmPageBytes {
		maxPages = linBytes / wasmPageBytes
	}
	j.putU32(offMaxLinMemPages, uint32(maxPages))
}

// jobMemoryGuardedCache parks at most one released guarded reservation, kept
// mapped and still registered with the trap handler, so the next
// AcquireJobMemoryGuarded skips the ~8 GiB reserve mmap + registry churn that
// otherwise dominates guard-page instantiate. A reused reservation is fully
// re-armed to PROT_NONE and zero-reclaimed first, so a fresh instance still sees
// zeroed linear memory and out-of-range accesses still fault.
var jobMemoryGuardedCache struct {
	sync.Mutex
	j *JobMemory
}

func init() { guardReleaseHook = releaseGuardedJobMemory }

// AcquireJobMemoryGuarded returns a guard-page linear memory of the requested
// size, reusing the cached reservation when one is parked. Every guarded
// reservation shares the same fixed geometry (guardReserveBytes, linOff), so any
// cached reservation can back any request — only the committed initial region and
// the basedata size caches differ, which rearmGuarded installs.
func AcquireJobMemoryGuarded(linBytes, maxBytes int) (*JobMemory, error) {
	jobMemoryGuardedCache.Lock()
	j := jobMemoryGuardedCache.j
	jobMemoryGuardedCache.j = nil
	jobMemoryGuardedCache.Unlock()
	if j == nil {
		return NewJobMemoryGuarded(linBytes, maxBytes)
	}
	if err := j.rearmGuarded(linBytes, maxBytes); err != nil {
		// The reservation is unusable; drop it (unregister + unmap) and start clean.
		_ = j.Close()
		return NewJobMemoryGuarded(linBytes, maxBytes)
	}
	return j, nil
}

// releaseGuardedJobMemory decommits a finished guarded reservation to a clean,
// fully-guarded, zeroed state and parks it in the one-slot cache. It returns
// false — leaving ReleaseJobMemory to Close it — when the slot is taken or the
// decommit fails, so at most one reservation is ever retained and the mapping is
// never leaked on error. The reservation keeps its trap-handler registry entry
// while parked; nothing accesses the idle range, so the handler never matches it.
func releaseGuardedJobMemory(j *JobMemory) bool {
	if j == nil || j.reserveBase == 0 {
		return false
	}
	if err := j.decommitGuarded(); err != nil {
		return false
	}
	jobMemoryGuardedCache.Lock()
	if jobMemoryGuardedCache.j == nil {
		jobMemoryGuardedCache.j = j
		jobMemoryGuardedCache.Unlock()
		return true
	}
	jobMemoryGuardedCache.Unlock()
	return false
}

// decommitGuarded re-arms every page the instance could have committed — the
// initial region plus any pages a memory.grow access faulted in, all within the
// current logical size — back to PROT_NONE, then drops them with MADV_DONTNEED so
// they zero-refill on next commit. This restores the same state a fresh
// reserve+PROT_NONE mmap would have (basedata aside, reset in rearmGuarded), so a
// reused reservation cannot leak a previous instance's memory and out-of-range
// accesses fault again. Runs before the reservation is parked, off the cache lock.
func (j *JobMemory) decommitGuarded() error {
	used := uintptr(roundUpPage(j.curBytes()))
	if used == 0 {
		return nil
	}
	linBase := j.reserveBase + uintptr(j.linOff)
	if _, _, errno := syscall.Syscall(syscall.SYS_MPROTECT, linBase, used, syscall.PROT_NONE); errno != 0 {
		return errno
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_MADVISE, linBase, used, syscall.MADV_DONTNEED); errno != 0 {
		return errno
	}
	return nil
}

// rearmGuarded prepares a decommitted, parked reservation for a new instance:
// commit [0,linBytes) of linear memory RW (zero-filled, since decommitGuarded
// dropped it), re-slice the basedata+initial view, clear basedata to fresh-mmap
// zero, and install the size caches. The reservation and its registry entry are
// unchanged, so no re-registration is needed. Runs off the cache lock.
func (j *JobMemory) rearmGuarded(linBytes, maxBytes int) error {
	linBase := j.reserveBase + uintptr(j.linOff)
	if linBytes > 0 {
		if _, _, errno := syscall.Syscall(syscall.SYS_MPROTECT, linBase, uintptr(linBytes),
			syscall.PROT_READ|syscall.PROT_WRITE); errno != 0 {
			return errno
		}
	}
	// j.mem's base pointer is the reservation base; re-slice through the existing
	// *byte (not the reserveBase uintptr) so the length covers basedata + the newly
	// committed initial region without a uintptr->Pointer round-trip.
	j.mem = unsafe.Slice(&j.mem[0], j.linOff+linBytes)
	j.linLen = linBytes
	clear(j.mem[:j.linOff]) // basedata: match fresh-mmap zeroing
	j.putGuardedSizeCaches(linBytes, maxBytes)
	return nil
}
