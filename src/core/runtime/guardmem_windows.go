//go:build windows && (amd64 || arm64) && wago_guardpage

package runtime

import (
	"fmt"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

const (
	maxLinMemBytes   = uintptr(1) << 32
	offsetGuardBytes = (uintptr(1) << 32) + (1 << 16)
	wasmPageBytes    = 1 << 16
	pageNoAccess     = 0x01
)

var guardReserveBytes = uintptr(roundUpPage(int(uintptr(basedataSize) + maxLinMemBytes + offsetGuardBytes)))

func NewJobMemoryGuarded(linBytes, maxBytes int) (*JobMemory, error) {
	if err := validateGuardedJobMemorySizes(linBytes, maxBytes); err != nil {
		return nil, err
	}
	// VirtualAlloc reserves on 64 KiB allocation-granularity boundaries. Keep
	// linMem on the same boundary so every lazy 64 KiB Wasm-page commit names an
	// allocation-aligned subrange of the reservation on both Windows targets.
	linOff := wasmPageBytes
	commit := linOff + linBytes
	base, _, callErr := procVirtualAlloc.Call(0, guardReserveBytes, memReserve, pageNoAccess)
	if base == 0 {
		return nil, fmt.Errorf("guard VirtualAlloc reserve: %w", callErr)
	}
	committed, _, callErr := procVirtualAlloc.Call(base, uintptr(commit), memCommit, pageReadWrite)
	if committed != base {
		_, _, _ = procVirtualFree.Call(base, 0, memRelease)
		return nil, fmt.Errorf("guard VirtualAlloc commit: %w", callErr)
	}
	j := &JobMemory{
		mem:         unsafe.Slice((*byte)(offHeapPointer(base)), commit),
		linOff:      linOff,
		linLen:      linBytes,
		reserveBase: base,
		reserveLen:  guardReserveBytes,
	}
	j.putGuardedSizeCaches(linBytes, maxBytes)
	if err := registerGuardRegion(base, base+guardReserveBytes, base+uintptr(linOff)); err != nil {
		_, _, _ = procVirtualFree.Call(base, 0, memRelease)
		return nil, err
	}
	return j, nil
}

func (j *JobMemory) putGuardedSizeCaches(linBytes, maxBytes int) {
	j.putU32(offActualLinMemByteSize, uint32(linBytes))
	j.putU64(offActualLinMemByteSize64, uint64(linBytes))
	j.putU32(offLinMemWasmSize, uint32(linBytes/wasmPageBytes))
	maxPages := maxBytes / wasmPageBytes
	if maxPages > 65536 {
		maxPages = 65536
	}
	if maxPages < linBytes/wasmPageBytes {
		maxPages = linBytes / wasmPageBytes
	}
	j.putU32(offMaxLinMemPages, uint32(maxPages))
}

var jobMemoryGuardedCache struct {
	sync.Mutex
	j *JobMemory
}

func init() {
	guardReleaseHook = releaseGuardedJobMemory
	guardCloseHook = unregisterGuardRegion
	guardOwnerHook = setGuardRegionOwner
}

func AcquireJobMemoryGuarded(linBytes, maxBytes int) (*JobMemory, error) {
	if err := validateGuardedJobMemorySizes(linBytes, maxBytes); err != nil {
		return nil, err
	}
	jobMemoryGuardedCache.Lock()
	j := jobMemoryGuardedCache.j
	jobMemoryGuardedCache.j = nil
	if j != nil {
		changeInterruptLinearMemoryCache(-1)
	}
	jobMemoryGuardedCache.Unlock()
	if j == nil {
		return NewJobMemoryGuarded(linBytes, maxBytes)
	}
	if err := j.rearmGuarded(linBytes, maxBytes); err != nil {
		_ = j.Close()
		return NewJobMemoryGuarded(linBytes, maxBytes)
	}
	return j, nil
}

func releaseGuardedJobMemory(j *JobMemory) bool {
	if j == nil || j.reserveBase == 0 || j.decommitGuarded() != nil {
		return false
	}
	jobMemoryGuardedCache.Lock()
	if jobMemoryGuardedCache.j == nil {
		jobMemoryGuardedCache.j = j
		changeInterruptLinearMemoryCache(1)
		jobMemoryGuardedCache.Unlock()
		return true
	}
	jobMemoryGuardedCache.Unlock()
	return false
}

func (j *JobMemory) decommitGuarded() error {
	used := uintptr(roundUpPage(j.curBytes()))
	if used == 0 {
		return nil
	}
	ok, _, callErr := procVirtualFree.Call(j.reserveBase+uintptr(j.linOff), used, memDecommit)
	if ok == 0 {
		return fmt.Errorf("guard VirtualFree(MEM_DECOMMIT): %w", callErr)
	}
	return nil
}

func (j *JobMemory) rearmGuarded(linBytes, maxBytes int) error {
	if linBytes > 0 {
		addr := j.reserveBase + uintptr(j.linOff)
		ptr, _, callErr := procVirtualAlloc.Call(addr, uintptr(linBytes), memCommit, pageReadWrite)
		if ptr != addr {
			return fmt.Errorf("guard VirtualAlloc rearm: %w", callErr)
		}
	}
	j.mem = unsafe.Slice((*byte)(offHeapPointer(j.reserveBase)), j.linOff+linBytes)
	j.linLen = linBytes
	clear(j.mem[:j.linOff])
	j.putGuardedSizeCaches(linBytes, maxBytes)
	return nil
}

func growGuardedHostView(j *JobMemory, logicalBytes int) error {
	committed := len(j.mem) - j.linOff
	if logicalBytes <= committed {
		return nil
	}
	addr := j.reserveBase + uintptr(j.linOff+committed)
	size := uintptr(logicalBytes - committed)
	ptr, _, callErr := procVirtualAlloc.Call(addr, size, memCommit, pageReadWrite)
	if ptr != addr {
		return fmt.Errorf("guard VirtualAlloc host view: %w", callErr)
	}
	j.mem = unsafe.Slice((*byte)(offHeapPointer(j.reserveBase)), j.linOff+logicalBytes)
	return nil
}

type guardRegion struct {
	start       uintptr
	end         uintptr
	linMem      uintptr
	ownerLinMem uintptr
}

const maxGuardRegions = 256

var (
	guardRegions  [maxGuardRegions]guardRegion
	guardRegionMu sync.Mutex
)

func registerGuardRegion(start, end, linMem uintptr) error {
	guardRegionMu.Lock()
	defer guardRegionMu.Unlock()
	for i := range guardRegions {
		if guardRegions[i].start == 0 {
			guardRegions[i].linMem = linMem
			guardRegions[i].ownerLinMem = linMem
			guardRegions[i].end = end
			atomic.StoreUintptr(&guardRegions[i].start, start)
			return nil
		}
	}
	return fmt.Errorf("guard region table full (%d)", maxGuardRegions)
}

func setGuardRegionOwner(start, ownerLinMem uintptr) {
	guardRegionMu.Lock()
	defer guardRegionMu.Unlock()
	for i := range guardRegions {
		if guardRegions[i].start == start {
			atomic.StoreUintptr(&guardRegions[i].ownerLinMem, ownerLinMem)
			return
		}
	}
}

func unregisterGuardRegion(start uintptr) {
	guardRegionMu.Lock()
	defer guardRegionMu.Unlock()
	for i := range guardRegions {
		if guardRegions[i].start == start {
			atomic.StoreUintptr(&guardRegions[i].start, 0)
			guardRegions[i].end = 0
			guardRegions[i].linMem = 0
			guardRegions[i].ownerLinMem = 0
			return
		}
	}
}

var (
	guardMu        sync.Mutex
	guardInstalled bool
)

func InstallGuardTrapHandler() error {
	guardMu.Lock()
	defer guardMu.Unlock()
	if guardInstalled {
		return nil
	}
	if err := installWindowsExceptionHandler(); err != nil {
		return err
	}
	guardInstalled = true
	return nil
}

func (e *Engine) CallGuarded(code uintptr, serArgs []byte, linMemBase uintptr, trap, results []byte, j *JobMemory) error {
	if j.reserveBase == 0 || linMemBase == 0 {
		return fmt.Errorf("CallGuarded requires NewJobMemoryGuarded")
	}
	if err := validateTrapBuffer(trap); err != nil {
		return err
	}
	clearTrapUnlessInterrupted(trap)
	j.putU64(abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	enterNative(code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop)
	goruntime.KeepAlive(serArgs)
	goruntime.KeepAlive(trap)
	goruntime.KeepAlive(results)
	goruntime.KeepAlive(j)
	goruntime.KeepAlive(e)
	if tc := TrapCode(loadTrap(trap)); tc != TrapNone {
		return trapErrorFromBuffer(tc, trap)
	}
	return nil
}
