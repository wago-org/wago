//go:build linux && amd64

package runtime

import (
	"encoding/binary"
	"unsafe"
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
	offTrapHandlerPtr       = 16 // u64
	offTrapStackReentry     = 24 // u64
	offRuntimePtr           = 32 // u64
	offCustomCtx            = 40 // u64 (V2 host-import ctx pointer)
	offSpillRegion          = 48 // 8B scratch
	offJobMemoryDataPtrPtr  = 56 // u64
	offMemoryHelperPtr      = 64 // u64
	offStackFence           = 72 // u64
	offTablePtr             = 80 // u64: indirect-call table descriptor (wago extension)
	offGlobalsPtr           = 88 // u64: globals slot base pointer (wago extension)

	basedataSize = 96 // keeps linMem 16-byte aligned after appending wago extension fields
)

// JobMemory is the contiguous [ basedata | linear memory ] region that
// WARP-compiled code expects. The linMem base pointer it receives addresses the
// memSize cache, stack fence, mutable globals, import ctx, etc. at negative
// offsets. It is mmap'd off-heap so its address is stable for native code.
type JobMemory struct {
	mem    []byte
	linOff int
	linLen int
}

// NewJobMemory lays out basedata immediately before linear memory.
func NewJobMemory(linBytes int) (*JobMemory, error) {
	mem, err := mmapRW(basedataSize + linBytes)
	if err != nil {
		return nil, err
	}
	j := &JobMemory{mem: mem, linOff: basedataSize, linLen: linBytes}
	j.putU32(offActualLinMemByteSize, uint32(linBytes))
	j.putU32(offLinMemWasmSize, uint32(linBytes/65536))
	return j, nil
}

// LinearMemory returns the zero-copy []byte view of wasm linear memory that is
// shared with native code (writes on either side are visible to the other).
func (j *JobMemory) LinearMemory() []byte {
	return j.mem[j.linOff : j.linOff+j.linLen : j.linOff+j.linLen]
}

// LinMemBase is the pointer handed to native code as the linMem base
// (RSI on entry, RBX inside WARP code).
func (j *JobMemory) LinMemBase() uintptr {
	return uintptr(unsafe.Pointer(&j.mem[j.linOff]))
}

// SetStackFence writes the low stack bound checked by WARP's active stack-fence
// guard ([linMem - 72]).
func (j *JobMemory) SetStackFence(v uintptr) { j.putU64(offStackFence, uint64(v)) }

// SetCustomCtx writes the V2 host-import context pointer ([linMem - 40]).
func (j *JobMemory) SetCustomCtx(v uintptr) { j.putU64(offCustomCtx, uint64(v)) }

// SetTablePtr writes the indirect-call table descriptor pointer ([linMem - 80]).
func (j *JobMemory) SetTablePtr(v uintptr) { j.putU64(offTablePtr, uint64(v)) }

// SetGlobalsPtr writes the globals slot base pointer ([linMem - 88]).
func (j *JobMemory) SetGlobalsPtr(v uintptr) { j.putU64(offGlobalsPtr, uint64(v)) }

func (j *JobMemory) Close() error { return munmap(j.mem) }

func (j *JobMemory) putU32(below int, v uint32) {
	binary.LittleEndian.PutUint32(j.mem[j.linOff-below:], v)
}

func (j *JobMemory) putU64(below int, v uint64) {
	binary.LittleEndian.PutUint64(j.mem[j.linOff-below:], v)
}
