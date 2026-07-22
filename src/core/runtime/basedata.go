//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package runtime

import (
	"encoding/binary"
	"fmt"
	"sync"
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
	offFuncRefDescPtr       = abi.FuncRefDescPtrOffset
	offPassiveElemPtr       = abi.PassiveElemPtrOffset
	offGlobalsPtr           = abi.GlobalsPtrOffset
	offPassiveDataPtr       = abi.PassiveDataPtrOffset
	offTableDirPtr          = abi.TableDirPtrOffset

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
	if initialBytes > maxClassicLinMemBytes {
		initialBytes = maxClassicLinMemBytes
	}
	if maxBytes > maxClassicLinMemBytes {
		maxBytes = maxClassicLinMemBytes
	}
	if maxBytes < initialBytes {
		maxBytes = initialBytes
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

// AcquireJobMemoryGrowable returns a non-guarded JobMemory, reusing one parked by
// ReleaseJobMemory when the parked reservation is at least as large as this
// module needs. ReleaseJobMemory zero-reclaims (madvise MADV_DONTNEED) everything
// the previous instance could have dirtied, and every access is confined to
// [0,curBytes) by bounds checks, so the whole reservation already reads back as
// zero — reset only reinstalls the size caches, with no clear() proportional to
// the (possibly multi-GiB) reservation. This lets even growable/exported-memory
// modules, whose reservation is the full ~4 GiB logical max, reuse the mapping
// instead of paying a fresh mmap+munmap of that range on every instantiate.
func AcquireJobMemoryGrowable(initialBytes, maxBytes int) (*JobMemory, error) {
	initialBytes, maxBytes, reserveBytes := normalizeMemorySizes(initialBytes, maxBytes)
	need := basedataSize + reserveBytes
	jobMemoryCache.Lock()
	j := jobMemoryCache.j
	if j != nil && j.reserveBase == 0 && len(j.mem) >= need {
		jobMemoryCache.j = nil
		jobMemoryCache.Unlock()
		j.reset(initialBytes, maxBytes, reserveBytes, false)
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

// jobMemoryReclaimThreshold splits reclaimForReuse's two zeroing strategies. At
// or below it, an in-place clear() is cheaper and keeps the pages committed, so
// the next reuse skips minor page faults — this keeps small, frequently-cycled
// modules (tiny/fib) near their ~0.7µs best. Above it, clearing a large (up to
// ~4 GiB) region dominates, so MADV_DONTNEED wins by dropping the pages instead.
// The crossover (clear cost ≈ madvise+refault cost) measures near ~384 KiB.
const jobMemoryReclaimThreshold = 384 << 10

// reclaimForReuse returns this non-guarded reservation to a zeroed state so it
// can be parked and reused without the old whole-reservation clear(). It only
// needs to reclaim what the instance could have dirtied — basedata plus linear
// memory up to its current logical size — because memory.grow just raises the
// size cache and bounds checks confine every access to [0,curBytes). Small
// regions are cleared in place (pages stay committed); large regions are dropped
// with MADV_DONTNEED (mirrors the guard-page path's decommitGuarded, minus the
// PROT_NONE re-arm — explicit bounds never fault, so the mapping stays RW).
func (j *JobMemory) reclaimForReuse() error {
	used := roundUpPage(basedataSize + j.curBytes())
	if used > len(j.mem) {
		used = len(j.mem)
	}
	if used <= jobMemoryReclaimThreshold {
		clear(j.mem[:used])
		return nil
	}
	return madviseDontNeed(j.mem[:used])
}

// curBytes is the current in-bounds linear-memory size, read from the cache that
// native code maintains (memory.grow updates it without involving Go).
func (j *JobMemory) curBytes() int { return int(j.getU32(offActualLinMemByteSize)) }

// RestoreLinear reloads linear memory from data (a full snapshot image whose
// length is the desired logical size) and resets the size caches to match, so
// the mapping returns to exactly the captured state for reuse. Any pages the
// previous tenant grew or dirtied beyond len(data) are zeroed, so a later
// memory.grow re-exposes them as spec-required zero bytes. Explicit-bounds
// (non-guarded) mappings only; guard-page reuse would also need a PROT re-arm.
func (j *JobMemory) RestoreLinear(data []byte) {
	n := len(data)
	old := j.curBytes()
	hi := n
	if old > hi {
		hi = old
	}
	if hi > j.linLen {
		hi = j.linLen
	}
	lin := j.mem[j.linOff : j.linOff+hi]
	copy(lin, data)
	if old > n {
		clear(lin[n:old]) // drop grown/dirtied tail back to zero
	}
	j.putU32(offActualLinMemByteSize, uint32(n))
	j.putU32(offLinMemWasmSize, uint32(n/65536))
}

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

// HostBytes returns a LIVE view of linear memory for host-side access (for example,
// embedder Read/Write), reflecting memory.grow. Native code tracks the current
// logical size in the basedata cache (curBytes) and, in guard-page mode, commits
// the grown pages without touching the Go-side j.mem slice — so a host reader must
// slice over the stable base up to curBytes, not rely on j.mem's (initial) length
// or j.linLen. The base never moves: growable memory is a fixed full-size
// reservation, so only the length grows. Unlike CurrentBytes, this stays valid
// after growth in guard-page mode (where j.mem is capped at the initial commit).
func (j *JobMemory) HostBytes() []byte {
	n := j.curBytes()
	if j.reserveBase != 0 {
		// Guard-page: j.mem is capped at the initial commit, but its backing array
		// is the whole reservation (committed up to curBytes by grow), so re-slice
		// through the existing *byte to the current size — like rearmGuarded, and
		// vet-safe (no uintptr->Pointer round-trip).
		return unsafe.Slice(&j.mem[0], j.linOff+n)[j.linOff:]
	}
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

// BindTrapCell installs the stable trap-cell pointer used by native trap stubs
// and establishes the zero-on-entry invariant required by Engine.CallPrepared.
// The caller must keep trap alive and at a stable address for the JobMemory's
// native calls (Arena-backed instance buffers satisfy this).
func (j *JobMemory) BindTrapCell(trap []byte) error {
	if len(trap) < 4 {
		return fmt.Errorf("trap cell requires at least 4 bytes")
	}
	binary.LittleEndian.PutUint32(trap, 0)
	j.putU64(abi.TrapCellPtrOffset, uint64(slicePtr(trap)))
	return nil
}

// HasTrapCell reports whether basedata still names this invocation's trap cell.
// Cross-instance entry replaces the pointer (and the fence alongside it), so
// this one-word identity check is sufficient for the prepared-call fast path.
func (j *JobMemory) HasTrapCell(trap []byte) bool {
	return len(trap) >= 4 && j.getU64(abi.TrapCellPtrOffset) == uint64(slicePtr(trap))
}

// InstanceContext is the per-instance subset of basedata. It deliberately
// excludes linear-memory size/growth state and per-invocation trap/stack fields,
// which belong to the shared Memory backing and active Engine call respectively.
type InstanceContext struct {
	CustomCtx      uintptr
	TablePtr       uintptr
	FuncRefDescPtr uintptr
	PassiveElemPtr uintptr
	GlobalsPtr     uintptr
	PassiveDataPtr uintptr
	TableDirPtr    uintptr
}

const InstanceContextBytes = 7 * 8

// CaptureInstanceContext snapshots the per-instance pointer fields currently
// installed in basedata.
func (j *JobMemory) CaptureInstanceContext() InstanceContext {
	return InstanceContext{
		CustomCtx:      uintptr(j.getU64(offCustomCtx)),
		TablePtr:       uintptr(j.getU64(offTablePtr)),
		FuncRefDescPtr: uintptr(j.getU64(offFuncRefDescPtr)),
		PassiveElemPtr: uintptr(j.getU64(offPassiveElemPtr)),
		GlobalsPtr:     uintptr(j.getU64(offGlobalsPtr)),
		PassiveDataPtr: uintptr(j.getU64(offPassiveDataPtr)),
		TableDirPtr:    uintptr(j.getU64(offTableDirPtr)),
	}
}

// BindInstanceContext installs one instance's pointer fields before native
// entry. Memory size/growth caches and invocation control words are untouched.
func (j *JobMemory) BindInstanceContext(ctx InstanceContext) {
	j.putU64(offCustomCtx, uint64(ctx.CustomCtx))
	j.putU64(offTablePtr, uint64(ctx.TablePtr))
	j.putU64(offFuncRefDescPtr, uint64(ctx.FuncRefDescPtr))
	j.putU64(offPassiveElemPtr, uint64(ctx.PassiveElemPtr))
	j.putU64(offGlobalsPtr, uint64(ctx.GlobalsPtr))
	j.putU64(offPassiveDataPtr, uint64(ctx.PassiveDataPtr))
	j.putU64(offTableDirPtr, uint64(ctx.TableDirPtr))
}

// CaptureInstanceContextBytes stores the current context in a stable off-heap
// buffer owned by the instance arena.
func (j *JobMemory) CaptureInstanceContextBytes(dst []byte) {
	if len(dst) < InstanceContextBytes {
		panic("runtime: short instance context buffer")
	}
	ctx := j.CaptureInstanceContext()
	for i, value := range [...]uintptr{ctx.CustomCtx, ctx.TablePtr, ctx.FuncRefDescPtr, ctx.PassiveElemPtr, ctx.GlobalsPtr, ctx.PassiveDataPtr, ctx.TableDirPtr} {
		binary.LittleEndian.PutUint64(dst[i*8:], uint64(value))
	}
}

// BindInstanceContextBytes restores a context captured by
// CaptureInstanceContextBytes.
func (j *JobMemory) BindInstanceContextBytes(src []byte) {
	if len(src) < InstanceContextBytes {
		panic("runtime: short instance context buffer")
	}
	j.BindInstanceContext(InstanceContext{
		CustomCtx:      uintptr(binary.LittleEndian.Uint64(src[0:])),
		TablePtr:       uintptr(binary.LittleEndian.Uint64(src[8:])),
		FuncRefDescPtr: uintptr(binary.LittleEndian.Uint64(src[16:])),
		PassiveElemPtr: uintptr(binary.LittleEndian.Uint64(src[24:])),
		GlobalsPtr:     uintptr(binary.LittleEndian.Uint64(src[32:])),
		PassiveDataPtr: uintptr(binary.LittleEndian.Uint64(src[40:])),
		TableDirPtr:    uintptr(binary.LittleEndian.Uint64(src[48:])),
	})
}

// SetCustomCtx writes the V2 host-import context pointer ([linMem - 40]).
func (j *JobMemory) SetCustomCtx(v uintptr) { j.putU64(offCustomCtx, uint64(v)) }

// SetTablePtr writes the indirect-call table descriptor pointer ([linMem - 80]).
func (j *JobMemory) SetTablePtr(v uintptr) { j.putU64(offTablePtr, uint64(v)) }

// SetFuncRefDesc writes the canonical funcref descriptor-array pointer. Its
// exact range is retained and validated by Instance; native code needs no count.
func (j *JobMemory) SetFuncRefDesc(ptr uintptr) {
	j.putU64(offFuncRefDescPtr, uint64(ptr))
}

// SetPassiveElemPtr writes the passive element descriptor pointer.
func (j *JobMemory) SetPassiveElemPtr(v uintptr) { j.putU64(offPassiveElemPtr, uint64(v)) }

// SetGlobalsPtr writes the globals pointer-table address at offGlobalsPtr.
func (j *JobMemory) SetGlobalsPtr(v uintptr) { j.putU64(offGlobalsPtr, uint64(v)) }

// SetPassiveDataPtr writes the passive data descriptor array address at offPassiveDataPtr.
func (j *JobMemory) SetPassiveDataPtr(v uintptr) { j.putU64(offPassiveDataPtr, uint64(v)) }

// SetTableDirPtr writes the indexed table descriptor directory pointer.
func (j *JobMemory) SetTableDirPtr(v uintptr) { j.putU64(offTableDirPtr, uint64(v)) }

// TableDirPtr returns the runtime-owned indexed table descriptor directory.
func (j *JobMemory) TableDirPtr() uintptr { return uintptr(j.getU64(offTableDirPtr)) }

// ReserveRange returns the guard-page reservation [base, base+len) for the trap
// handler's fault-address check (both zero in classic mode).
func (j *JobMemory) ReserveRange() (base, length uintptr) { return j.reserveBase, j.reserveLen }

// guardCloseHook, set by the wago_guardpage build, removes a guarded reservation
// from the trap handler's registry before it is unmapped. nil otherwise.
var guardCloseHook func(reserveBase uintptr)

// guardReleaseHook, set by the wago_guardpage build, offers a released guarded
// reservation to the guard-page reuse cache (keeping its registry entry) instead
// of unmapping it. It returns true when it took ownership; a false result means
// the caller should fall back to Close. nil otherwise (guarded memory is only
// created under the wago_guardpage build, so this is only nil in impossible
// configurations, where Close is the correct fallback).
var guardReleaseHook func(j *JobMemory) bool

func (j *JobMemory) Close() error {
	if j.reserveBase != 0 { // guard-page reservation
		if guardCloseHook != nil {
			guardCloseHook(j.reserveBase)
		}
		return munmapRange(j.reserveBase, j.reserveLen)
	}
	return munmap(j.mem)
}

// ReleaseJobMemory returns a memory to a bounded reuse cache or unmaps it.
// Guard-page reservations go through guardReleaseHook, which keeps the mapping
// (and its signal-handler registry entry) warm for the next instantiate; classic
// mappings use the small non-guarded cache. Anything the caches decline is
// unmapped.
func ReleaseJobMemory(j *JobMemory) error {
	if j == nil {
		return nil
	}
	if j.reserveBase != 0 {
		if guardReleaseHook != nil && guardReleaseHook(j) {
			return nil
		}
		return j.Close()
	}
	// Zero-reclaim the region this instance could have dirtied so the reservation
	// can be reused without a full clear(), then park it in the one-slot cache.
	// Any size fits the slot now (the reservation costs address space, not RAM,
	// once decommitted), so growable modules stop churning fresh mmaps.
	if err := j.reclaimForReuse(); err != nil {
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

func (j *JobMemory) getU64(below int) uint64 {
	return binary.LittleEndian.Uint64(j.mem[j.linOff-below:])
}
