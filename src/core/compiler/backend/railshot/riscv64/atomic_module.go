//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func atomicHelperSlots(m *wasm.Module) int {
	if m == nil || m.MemCount() == 0 {
		return 0
	}
	threaded := moduleThreadedMemory(m)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	slots := 0
	for i := range m.Code {
		r := wasm.ReaderFrom(m.Code[i].BodyBytes)
		for r.HasNext() {
			op, err := r.Byte()
			if err != nil {
				return 0
			}
			var imm wasm.InstructionImmediate
			if err = classifier.ClassifyInto(&r, op, &imm); err != nil {
				return 0
			}
			if op == 0x40 && threaded {
				slots = max(slots, 1)
			}
			if op == 0xfe && imm.Subopcode <= 2 {
				n := 4
				if imm.Subopcode == 0 {
					n = 3
				}
				slots = max(slots, n)
			}
		}
	}
	return slots
}

// Preserve existing images when no atomic helpers are present. Each of the
// returning helpers has one checked synchronous bridge per image.
func appendAtomicHelpers(code []byte, calls []directCall) ([]byte, error) {
	var entries [4]int
	for i := range entries {
		entries[i] = -1
	}
	for _, call := range calls {
		if call.atomicHelper == 0 {
			continue
		}
		helper := call.atomicHelper &^ shared.AtomicWaitDispatchBit
		if call.atomicHelper&shared.AtomicWaitDispatchBit == 0 || helper >= uint32(len(entries)) {
			return nil, unsupported("atomic helper dispatch identity")
		}
		if entries[helper] >= 0 {
			continue
		}
		slots := 4
		if helper == shared.AtomicHelperNotify {
			slots = 3
		} else if helper == shared.AtomicHelperMemoryGrow {
			slots = 1
		}
		padding := (-len(code)) & 15
		stub := HostIndirectOwnedSyncThunk(call.atomicHelper, slots, 1)
		if len(stub) == 0 || len(stub)+padding > MaxCodeBytes-len(code) {
			return nil, unsupported("atomic helper image bound")
		}
		for ; padding > 0; padding -= 4 {
			code = append(code, 0x13, 0, 0, 0)
		}
		entries[helper] = len(code)
		code = append(code, stub...)
	}
	image := enc.Asm{B: code}
	for _, call := range calls {
		if call.atomicHelper == 0 {
			continue
		}
		target := entries[call.atomicHelper&^shared.AtomicWaitDispatchBit]
		if err := image.PatchLong(call.site, int64(target)); err != nil {
			return nil, err
		}
	}
	return code, nil
}

func moduleThreadedMemory(m *wasm.Module) bool {
	if m == nil || m.MemCount() != 1 {
		return false
	}
	memory, ok := m.MemoryType(0)
	return ok && memory.Shared
}
