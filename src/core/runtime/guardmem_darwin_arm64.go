//go:build darwin && arm64 && wago_guardpage

package runtime

import (
	"fmt"
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

var guardReserveBytes = uintptr(roundUpPage(int(uintptr(basedataSize) + maxLinMemBytes + offsetGuardBytes)))

func NewJobMemoryGuarded(linBytes, maxBytes int) (*JobMemory, error) {
	linOff := roundUpPage(basedataSize)
	commit := uintptr(linOff + linBytes)
	mem, err := syscall.Mmap(-1, 0, int(guardReserveBytes), syscall.PROT_NONE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("guard mmap reserve: %w", err)
	}
	if err := syscall.Mprotect(mem[:commit], syscall.PROT_READ|syscall.PROT_WRITE); err != nil {
		_ = syscall.Munmap(mem)
		return nil, fmt.Errorf("guard mprotect commit: %w", err)
	}
	base := uintptr(unsafe.Pointer(&mem[0]))
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

var jobMemoryGuardedCache struct {
	sync.Mutex
	j *JobMemory
}

func init() {
	guardReleaseHook = releaseGuardedJobMemory
	guardCloseHook = unregisterGuardRegion
}

func AcquireJobMemoryGuarded(linBytes, maxBytes int) (*JobMemory, error) {
	jobMemoryGuardedCache.Lock()
	j := jobMemoryGuardedCache.j
	jobMemoryGuardedCache.j = nil
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
	if err := syscall.Mprotect(lin, syscall.PROT_NONE); err != nil {
		return err
	}
	return madviseDontNeed(lin)
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
	clear(j.mem[:j.linOff])
	j.putGuardedSizeCaches(linBytes, maxBytes)
	return nil
}

type guardRegion struct {
	start  uintptr
	end    uintptr
	linMem uintptr
	_      uintptr
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
			guardRegions[i].end = end
			// Publish last; the signal handler acquire-loads start before it
			// consumes the rest of the entry.
			atomic.StoreUintptr(&guardRegions[i].start, start)
			return nil
		}
	}
	return fmt.Errorf("guard region table full (%d)", maxGuardRegions)
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

func (e *Engine) CallGuarded(code uintptr, serArgs, linMem, trap, results []byte, j *JobMemory) error {
	if j.reserveBase == 0 {
		return fmt.Errorf("CallGuarded requires NewJobMemoryGuarded")
	}
	linMemBase := j.LinMemBase()
	if len(trap) >= 4 {
		trap[0], trap[1], trap[2], trap[3] = 0, 0, 0, 0
		j.putU64(abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	}
	enterNative(code, slicePtr(serArgs), linMemBase, slicePtr(trap), slicePtr(results), e.stackTop)
	if len(trap) >= 4 {
		if tc := TrapCode(uint32(trap[0]) | uint32(trap[1])<<8 | uint32(trap[2])<<16 | uint32(trap[3])<<24); tc != TrapNone {
			return &TrapError{Code: tc}
		}
	}
	return nil
}
