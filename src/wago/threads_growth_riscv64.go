//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func threadedGrowthHelpers(m *wasm.Module) bool {
	if m == nil || m.MemCount() != 1 {
		return false
	}
	memory, ok := m.MemoryType(0)
	if !ok || !memory.Shared {
		return false
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for _, fn := range m.Code {
		r := wasm.ReaderFrom(fn.BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return false
			}
			var imm wasm.InstructionImmediate
			if err = classifier.ClassifyInto(&r, op, &imm); err != nil {
				return false
			}
			if op == 0x40 {
				return true
			}
		}
	}
	return false
}

func threadedMemorySizeGrowSupported() bool { return true }

func (in *Instance) dispatchAtomicMemoryGrow(args []uint64) (uint32, error) {
	if len(args) != 1 || in.memory == nil || !in.c.threadedMemory0() || in.memoryDir == nil || len(in.memoryDir.native) < abi.MemoryDirEntryBytes {
		return 0, fmt.Errorf("invalid shared memory growth helper")
	}
	s := in.memory.state.Load()
	if s == nil {
		return 0, fmt.Errorf("shared memory growth has no lifetime state")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.has(memoryStateClosed) || !s.has(memoryStateWasmShared) || in.memory.jm == nil {
		return 0, errMemoryWaitClosed
	}
	policy := binary.LittleEndian.Uint32(in.memoryDir.native[abi.MemoryDirPolicyMaxPagesOffset:])
	old := in.memory.jm.GrowSharedMemory(uint32(args[0]), policy)
	if old != ^uint32(0) {
		pages := in.memory.jm.CurrentPages()
		binary.LittleEndian.PutUint32(in.memoryDir.native[abi.MemoryDirCurrentPagesOffset:], pages)
		binary.LittleEndian.PutUint64(in.memoryDir.native[abi.MemoryDirCurrentBytesOffset:], uint64(pages)<<16)
	}
	return old, nil
}
