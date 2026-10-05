//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func swarPassiveData(data []wasm.Data) ([][]byte, error) {
	if len(data) > 256 {
		return nil, unsupported("SWAR passive data bound")
	}
	total := 0
	for _, segment := range data {
		if segment.Mode.Kind != wasm.DataPassive || segment.Mode.Mem != 0 || len(segment.Mode.Offset.BodyBytes) != 0 || len(segment.Mode.Offset.Instrs) != 0 || len(segment.Init) > MaxModuleBytes-total {
			return nil, unsupported("SWAR passive-only data shape/byte bound")
		}
		total += len(segment.Init)
	}
	owned := make([][]byte, len(data))
	for i, segment := range data {
		owned[i] = append([]byte(nil), segment.Init...)
	}
	return owned, nil
}

// The private owner requires exclusive idle access, a JobMemory kept live
// through Close, and no saved or queued contexts. Every native invocation must
// complete before Close. Payloads and mutable descriptor lengths are per owner;
// data.drop changes only len32 and never frees payloads during an invocation.
type swarDataOwner struct {
	arena     *coreruntime.Arena
	job       *coreruntime.JobMemory
	directory uintptr
}

func (image *swarModuleImage) instantiateData(job *coreruntime.JobMemory) (*swarDataOwner, error) {
	return image.instantiateDataWithArena(job, coreruntime.NewArena)
}

// Production uses NewArena; a local allocator allows prepublication failure
// ordering to be tested without changing process-wide runtime hooks.
func (image *swarModuleImage) instantiateDataWithArena(job *coreruntime.JobMemory, allocate func(int) (*coreruntime.Arena, error)) (*swarDataOwner, error) {
	if image == nil || job == nil || len(image.data) > 256 {
		return nil, unsupported("SWAR passive data owner image/job/bound")
	}
	if job.CaptureInstanceContext().PassiveDataPtr != 0 {
		return nil, unsupported("SWAR passive data owner requires unbound job")
	}
	payloadBytes, arenaBytes := 0, len(image.data)*16
	for _, payload := range image.data {
		if len(payload) > MaxModuleBytes-payloadBytes {
			return nil, unsupported("SWAR passive data owner byte bound")
		}
		payloadBytes += len(payload)
		// Arena allocations start at 8-byte boundaries, including after odd
		// payload lengths. Overestimating trailing padding remains bounded.
		arenaBytes += (len(payload) + 7) &^ 7
	}
	var memory []byte
	if len(image.activeData) != 0 {
		if image.memory == nil || image.memory.Shared || image.memory.Limits.Addr64 {
			return nil, unsupported("SWAR active data owner memory32")
		}
		memory = job.CurrentBytes()
	}
	previousIndex := -1
	for _, active := range image.activeData {
		if uint64(active.index) >= uint64(len(image.data)) || int(active.index) <= previousIndex || len(image.data[active.index]) != 0 || len(active.bytes) > MaxModuleBytes-payloadBytes {
			return nil, unsupported("SWAR active data owner index/byte bound")
		}
		previousIndex = int(active.index)
		payloadBytes += len(active.bytes)
		// Check every complete range before arena allocation, publication or
		// even the first initializer write, including zero-length segments.
		offset := uint64(active.offset)
		if offset > uint64(len(memory)) || uint64(len(active.bytes)) > uint64(len(memory))-offset {
			return nil, fmt.Errorf("rv64: active data segment %d out of bounds", active.index)
		}
	}
	owner := &swarDataOwner{}
	if arenaBytes == 0 {
		return owner, nil
	}
	arena, err := allocate(arenaBytes)
	if err != nil {
		return nil, err
	}
	directory := arena.Alloc(len(image.data) * 16)
	for i, payload := range image.data {
		cell := arena.Alloc(len(payload))
		copy(cell, payload)
		var pointer uint64
		if len(cell) != 0 {
			pointer = uint64(uintptr(unsafe.Pointer(&cell[0])))
		}
		binary.LittleEndian.PutUint64(directory[i*16:], pointer)
		binary.LittleEndian.PutUint32(directory[i*16+8:], uint32(len(cell)))
		binary.LittleEndian.PutUint32(directory[i*16+12:], 0)
	}
	// Source order is significant for overlapping initializers. All remaining
	// operations are infallible under the private exclusive live-job contract.
	for _, active := range image.activeData {
		start := int(active.offset)
		copy(memory[start:start+len(active.bytes)], active.bytes)
	}
	owner.arena, owner.job = arena, job
	owner.directory = uintptr(unsafe.Pointer(&directory[0]))
	job.SetPassiveDataPtr(owner.directory)
	return owner, nil
}

func (owner *swarDataOwner) Close() error {
	return owner.closeWith((*coreruntime.Arena).Close)
}

func (owner *swarDataOwner) closeWith(release func(*coreruntime.Arena) error) error {
	if owner == nil || owner.arena == nil {
		return nil
	}
	current := owner.job.CaptureInstanceContext().PassiveDataPtr
	if current != 0 && current != owner.directory {
		return unsupported("SWAR passive data owner binding replaced")
	}
	owner.job.SetPassiveDataPtr(0)
	if err := release(owner.arena); err != nil {
		return err
	}
	owner.arena, owner.job, owner.directory = nil, nil, 0
	return nil
}
