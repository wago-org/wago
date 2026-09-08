//go:build darwin && (amd64 || arm64) && wago_guardpage

package runtime

import (
	"fmt"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

const (
	maxLinMemBytes   = uintptr(1) << 32
	offsetGuardBytes = (uintptr(1) << 32) + (1 << 16)
	wasmPageBytes    = 1 << 16
)

var guardReserveBytes = uintptr(roundUpPage(int(uintptr(basedataSize) + wasmPageBytes - 1 + maxLinMemBytes + offsetGuardBytes)))

func guardedLinOff(base uintptr) int {
	linMem := (base + uintptr(basedataSize) + wasmPageBytes - 1) &^ uintptr(wasmPageBytes-1)
	return int(linMem - base)
}

func NewJobMemoryGuarded(linBytes, maxBytes int) (*JobMemory, error) {
	if err := validateGuardedJobMemorySizes(linBytes, maxBytes); err != nil {
		return nil, err
	}
	mem, err := syscall.Mmap(-1, 0, int(guardReserveBytes), syscall.PROT_NONE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("guard mmap reserve: %w", err)
	}
	base := uintptr(unsafe.Pointer(&mem[0]))
	// The ARM64 signal handler commits a complete 64 KiB wasm page. Align the
	// linear-memory base to that boundary rather than merely to the Darwin host
	// page, and reserve one wasm page of slack for the adjustment.
	linOff := guardedLinOff(base)
	commit := uintptr(linOff + linBytes)
	if err := syscall.Mprotect(mem[:commit], syscall.PROT_READ|syscall.PROT_WRITE); err != nil {
		_ = syscall.Munmap(mem)
		return nil, fmt.Errorf("guard mprotect commit: %w", err)
	}
	j := &JobMemory{
		mem:         mem[:commit],
		linOff:      linOff,
		linLen:      linBytes,
		reserveBase: base,
		reserveLen:  guardReserveBytes,
	}
	j.putGuardedSizeCaches(linBytes, maxBytes)
	if err := registerGuardRegion(base, base+guardReserveBytes, base+uintptr(linOff)); err != nil {
		_ = syscall.Munmap(mem)
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
	if j == nil || j.reserveBase == 0 {
		return false
	}
	if err := j.decommitGuarded(); err != nil {
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
	full := j.mem[:j.linOff+int(used)]
	lin := full[j.linOff:]
	if err := syscall.Mprotect(lin, syscall.PROT_READ|syscall.PROT_WRITE); err != nil {
		return err
	}
	clear(lin)
	// Reclaim while the range is writable. On Intel macOS madviseDontNeed
	// deliberately clears the range because Rosetta's MADV_ZERO does not discard
	// translated pages; doing that after PROT_NONE would fault in host code.
	if err := madviseDontNeed(lin); err != nil {
		return err
	}
	return syscall.Mprotect(lin, syscall.PROT_NONE)
}

func (j *JobMemory) rearmGuarded(linBytes, maxBytes int) error {
	if linBytes > 0 {
		lin := j.mem[:j.linOff+linBytes][j.linOff:]
		if err := syscall.Mprotect(lin, syscall.PROT_READ|syscall.PROT_WRITE); err != nil {
			return err
		}
	}
	j.mem = j.mem[:j.linOff+linBytes]
	j.linLen = linBytes
	clear(j.mem[j.linOff-basedataSize : j.linOff])
	j.putGuardedSizeCaches(linBytes, maxBytes)
	return nil
}

type guardRegion struct {
	start       uintptr
	end         uintptr
	linMem      uintptr
	ownerLinMem uintptr
}

const (
	_ = uint(unsafe.Sizeof(guardRegion{}) - 32)
	_ = uint(32 - unsafe.Sizeof(guardRegion{}))
)

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
			// Publish last; the signal handler acquire-loads start before it
			// consumes the rest of the entry.
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
			// Disable first so the handler cannot begin a new match while the
			// remaining fields are cleared.
			atomic.StoreUintptr(&guardRegions[i].start, 0)
			guardRegions[i].end = 0
			guardRegions[i].linMem = 0
			guardRegions[i].ownerLinMem = 0
			return
		}
	}
}

func InstallGuardTrapHandler() error {
	guardMu.Lock()
	defer guardMu.Unlock()
	if guardInstalled {
		return nil
	}
	guardTrapExitHandlerJumpPC = addrNativeTrapExitHandlerJump()
	if err := installDarwinSignalHandlers(); err != nil {
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
