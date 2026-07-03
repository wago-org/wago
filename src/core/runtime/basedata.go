//go:build linux && amd64

package runtime

import (
	"encoding/binary"
	"sync"
	"syscall"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Basedata field offsets in bytes BELOW the linear-memory base — i.e. addressed
// by native code as [linMem - off]. Verified against WARP
// src/core/common/basedataoffsets.hpp for the Phase-0 config (x86_64,
// INTERRUPTION_REQUEST=0, LINEAR_MEMORY_BOUNDS_CHECKS=1,
// ACTIVE_STACK_OVERFLOW_CHECK=1, BUILTIN_FUNCTIONS=0, no stacktrace,
// STACKSIZE_LEFT_BEFORE_NATIVE_CALL=0).
const (
	offLinMemWasmSize       = 4  // u32 (pages)
	offActualLinMemByteSize = 8  // u32 (bytes); memSize cache = this-8
	offMaxLinMemPages       = 12 // u32 (pages); wago extension: grow ceiling (reserved size)
	offTrapHandlerPtr       = 16 // u64
	offTrapStackReentry     = 24 // u64
	offRuntimePtr           = 32 // u64
	offCustomCtx            = 40 // u64 (V2 host-import ctx pointer)
	offSpillRegion          = 48 // 8B scratch
	offJobMemoryDataPtrPtr  = 56 // u64
	offMemoryHelperPtr      = 64 // u64
	offStackFence           = 72 // u64
	offTablePtr             = 80 // u64: indirect-call table descriptor (wago extension)
	offGlobalsPtr           = abi.GlobalsPtrOffset

	basedataSize = abi.BasedataSize // keeps linMem 16-byte aligned after appending wago extension fields
)

// JobMemory is the contiguous [ basedata | linear memory ] region that
// WARP-compiled code expects. The linMem base pointer it receives addresses the
// memSize cache, stack fence, mutable globals, import ctx, etc. at negative
// offsets. It is mmap'd off-heap so its address is stable for native code.
type JobMemory struct {
	mem    []byte
	linOff int
	linLen int // byte length of the RW-usable region (initial size, or the reservation for growable memory)
	// Guard-page mode (NewJobMemoryGuarded): the full PROT_NONE reservation that
	// must be unmapped on Close and that the SIGSEGV handler range-checks. Zero in
	// the classic exactly-sized RW layout.
	reserveBase uintptr
	reserveLen  uintptr
}

const (
	maxClassicLinMemBytes        = 65535 * 65536
	minClassicLinMemReserveBytes = 65536
	jobMemoryCacheMaxBytes       = 1 << 20
)

var jobMemoryCache struct {
	sync.Mutex
	j *JobMemory
}

// NewJobMemory lays out basedata immediately before a fixed-size (non-growable)
// linear memory. memory.grow on it always fails (max == initial).
func NewJobMemory(linBytes int) (*JobMemory, error) {
	return NewJobMemoryGrowable(linBytes, linBytes)
}

// NewJobMemoryGrowable reserves maxBytes of RW address space (lazily backed) but
// exposes only initialBytes as in-bounds linear memory. memory.grow raises the
// size cache up to maxBytes without any remap, so the base pointer never moves.
func NewJobMemoryGrowable(initialBytes, maxBytes int) (*JobMemory, error) {
	initialBytes, maxBytes, reserveBytes := normalizeMemorySizes(initialBytes, maxBytes)
	mem, err := mmapRWReserve(basedataSize + reserveBytes)
	if err != nil {
		return nil, err
	}
	j := &JobMemory{mem: mem, linOff: basedataSize, linLen: reserveBytes}
	j.reset(initialBytes, maxBytes, reserveBytes, false)
	return j, nil
}

func normalizeMemorySizes(initialBytes, maxBytes int) (int, int, int) {
	// 65536 pages is 4 GiB, whose byte size (2^32) does not fit the u32 size
	// cache, so cap the logical size at 65535 pages (0xFFFF0000 bytes).
	if maxBytes > maxClassicLinMemBytes {
		maxBytes = maxClassicLinMemBytes
	}
	if maxBytes < initialBytes {
		maxBytes = initialBytes
	}
	if initialBytes > maxClassicLinMemBytes {
		initialBytes = maxClassicLinMemBytes
	}
	// The mapping is floored at one page so the linear-memory base is always a
	// valid address even for a zero-page logical memory; the logical max (which may
	// be smaller, even zero) is recorded separately for the grow check.
	reserveBytes := maxBytes
	if reserveBytes < minClassicLinMemReserveBytes {
		reserveBytes = minClassicLinMemReserveBytes
	}
	return initialBytes, maxBytes, reserveBytes
}

func (j *JobMemory) reset(initialBytes, maxBytes, reserveBytes int, clearMem bool) {
	if clearMem {
		clear(j.mem[:basedataSize+reserveBytes])
	}
	j.linOff = basedataSize
	j.linLen = reserveBytes
	j.reserveBase = 0
	j.reserveLen = 0
	j.putU32(offActualLinMemByteSize, uint32(initialBytes))
	j.putU32(offLinMemWasmSize, uint32(initialBytes/65536))
	j.putU32(offMaxLinMemPages, uint32(maxBytes/65536))
}

// AcquireJobMemoryGrowable returns a non-guarded JobMemory, reusing one recently
// released by ReleaseJobMemory when its reservation is small enough. Reuse fully
// clears basedata and the exposed reservation before installing size caches, so
// fresh-instance zero memory and future memory.grow zeroing semantics are kept.
func AcquireJobMemoryGrowable(initialBytes, maxBytes int) (*JobMemory, error) {
	initialBytes, maxBytes, reserveBytes := normalizeMemorySizes(initialBytes, maxBytes)
	if reserveBytes > jobMemoryCacheMaxBytes {
		return NewJobMemoryGrowable(initialBytes, maxBytes)
	}
	need := basedataSize + reserveBytes
	jobMemoryCache.Lock()
	j := jobMemoryCache.j
	if j != nil && j.reserveBase == 0 && len(j.mem) >= need {
		jobMemoryCache.j = nil
		jobMemoryCache.Unlock()
		j.reset(initialBytes, maxBytes, reserveBytes, true)
		return j, nil
	}
	if j != nil && len(j.mem) < need {
		jobMemoryCache.j = nil
		jobMemoryCache.Unlock()
		_ = j.Close()
		return NewJobMemoryGrowable(initialBytes, maxBytes)
	}
	jobMemoryCache.Unlock()
	return NewJobMemoryGrowable(initialBytes, maxBytes)
}

// curBytes is the current in-bounds linear-memory size, read from the cache that
// native code maintains (memory.grow updates it without involving Go).
func (j *JobMemory) curBytes() int { return int(j.getU32(offActualLinMemByteSize)) }

// CurrentBytes returns the host-facing view of linear memory at its current
// (possibly grown) logical size — what Memory.Bytes exposes.
func (j *JobMemory) CurrentBytes() []byte {
	n := j.curBytes()
	return j.mem[j.linOff : j.linOff+n : j.linOff+n]
}

// LinearMemory returns the native-facing view spanning the full reservation, so
// its base pointer is always valid; native code enforces the current logical
// size via the bounds-check size cache, not this slice's length.
func (j *JobMemory) LinearMemory() []byte {
	n := j.linLen
	return j.mem[j.linOff : j.linOff+n : j.linOff+n]
}

// LinMemBase is the pointer handed to native code as the linMem base
// (RSI on entry, RBX inside WARP code).
func (j *JobMemory) LinMemBase() uintptr {
	if j.reserveBase != 0 {
		return j.reserveBase + uintptr(j.linOff)
	}
	return uintptr(unsafe.Pointer(&j.mem[j.linOff]))
}

// SetStackFence writes the low stack bound checked by WARP's active stack-fence
// guard ([linMem - 72]).
func (j *JobMemory) SetStackFence(v uintptr) { j.putU64(offStackFence, uint64(v)) }

// SetCustomCtx writes the V2 host-import context pointer ([linMem - 40]).
func (j *JobMemory) SetCustomCtx(v uintptr) { j.putU64(offCustomCtx, uint64(v)) }

// SetTablePtr writes the indirect-call table descriptor pointer ([linMem - 80]).
func (j *JobMemory) SetTablePtr(v uintptr) { j.putU64(offTablePtr, uint64(v)) }

// SetGlobalsPtr writes the globals pointer-table address at offGlobalsPtr.
func (j *JobMemory) SetGlobalsPtr(v uintptr) { j.putU64(offGlobalsPtr, uint64(v)) }

// ReserveRange returns the guard-page reservation [base, base+len) for the trap
// handler's fault-address check (both zero in classic mode).
func (j *JobMemory) ReserveRange() (base, length uintptr) { return j.reserveBase, j.reserveLen }

// guardCloseHook, set by the wago_guardpage build, removes a guarded reservation
// from the trap handler's registry before it is unmapped. nil otherwise.
var guardCloseHook func(reserveBase uintptr)

func (j *JobMemory) Close() error {
	if j.reserveBase != 0 { // guard-page reservation
		if guardCloseHook != nil {
			guardCloseHook(j.reserveBase)
		}
		if _, _, errno := syscall.Syscall(syscall.SYS_MUNMAP, j.reserveBase, j.reserveLen, 0); errno != 0 {
			return errno
		}
		return nil
	}
	return munmap(j.mem)
}

// ReleaseJobMemory returns small non-guarded memories to a bounded cache or
// unmaps them. Guard-page reservations are never cached because their lifecycle
// is registered with the signal handler.
func ReleaseJobMemory(j *JobMemory) error {
	if j == nil {
		return nil
	}
	if j.reserveBase != 0 || len(j.mem)-basedataSize > jobMemoryCacheMaxBytes {
		return j.Close()
	}
	jobMemoryCache.Lock()
	if jobMemoryCache.j == nil {
		jobMemoryCache.j = j
		jobMemoryCache.Unlock()
		return nil
	}
	jobMemoryCache.Unlock()
	return j.Close()
}

func (j *JobMemory) putU32(below int, v uint32) {
	binary.LittleEndian.PutUint32(j.mem[j.linOff-below:], v)
}

func (j *JobMemory) getU32(below int) uint32 {
	return binary.LittleEndian.Uint32(j.mem[j.linOff-below:])
}

func (j *JobMemory) putU64(below int, v uint64) {
	binary.LittleEndian.PutUint64(j.mem[j.linOff-below:], v)
}
