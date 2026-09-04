package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// gcSyncHostSlotCapacity derives the largest struct constructor helper shape.
// The trailing slot carries the flattened type index. Array fixed constructors
// retain their existing bounded/spill lowering and do not enlarge this frame.
func gcSyncHostSlotCapacity(descs []gc.TypeDesc) (int, error) {
	maxSlots := coreruntime.MaxHostArity
	for typeIndex := range descs {
		desc := &descs[typeIndex]
		if desc.Kind != gc.KindStruct {
			continue
		}
		slots := 1
		for _, field := range desc.Fields {
			if slots == coreruntime.MaxSyncHostSlots {
				return 0, fmt.Errorf("GC struct type %d helper exceeds the uint16 synchronous host slot limit", typeIndex)
			}
			slots++
			if field.Kind == gc.StorageV128 {
				if slots == coreruntime.MaxSyncHostSlots {
					return 0, fmt.Errorf("GC struct type %d helper exceeds the uint16 synchronous host slot limit", typeIndex)
				}
				slots++
			}
		}
		if slots > maxSlots {
			maxSlots = slots
		}
	}
	return maxSlots, nil
}

func moduleSyncHostSlotCapacity(m *wasm.Module) (int, error) {
	maxSlots := coreruntime.MaxHostArity
	if m == nil {
		return maxSlots, nil
	}
	functionIndex := 0
	for importIndex := range m.Imports {
		if m.Imports[importIndex].Type.Kind != wasm.ExternFunc {
			continue
		}
		ft, ok := m.ImportFuncType(importIndex)
		if !ok {
			return 0, fmt.Errorf("imported function %d signature is unavailable", functionIndex)
		}
		for _, values := range [][]wasm.ValType{ft.Params, ft.Results} {
			slots := 0
			for _, value := range values {
				slots++
				if value == wasm.V128 {
					slots++
				}
				if slots > coreruntime.MaxSyncHostSlots {
					return 0, &coreruntime.ImplementationLimitError{
						Feature: "synchronous host-call signature",
						Shape:   fmt.Sprintf("imported function %d requires more than %d ABI slots", functionIndex, coreruntime.MaxSyncHostSlots),
						Limit:   coreruntime.MaxSyncHostSlots,
					}
				}
			}
			if slots > maxSlots {
				maxSlots = slots
			}
		}
		functionIndex++
	}
	return maxSlots, nil
}

func (c *Compiled) hostCtrlFrameBytes() int {
	bytes, err := coreruntime.HostCtrlFrameBytesForSlots(int(c.syncHostSlots))
	if err != nil {
		panic(err) // validateArenaFootprint establishes this immutable cache.
	}
	return bytes
}
