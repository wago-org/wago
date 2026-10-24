package wago

import (
	"fmt"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

const rv64GenericArrayFixedSlots = 256

// gcStructConstructorSlotCapacity derives the largest flattened struct
// constructor shape. The trailing slot carries the type index.
func gcStructConstructorSlotCapacity(descs []gc.TypeDesc, floor int) (int, error) {
	maxSlots := floor
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

// gcSyncHostSlotCapacity derives the symmetric native/Go exchange capacity.
// The inline control frame already owns MaxHostArity slots in each direction.
func gcSyncHostSlotCapacity(descs []gc.TypeDesc) (int, error) {
	return gcStructConstructorSlotCapacity(descs, coreruntime.MaxHostArity)
}

// gcInternalHelperSlotCapacity derives the argument arena used by generated
// internal helper calls. Unlike the control frame, this arena has no 64-slot
// inline prefix: its allocation must cover the exact largest helper shape or a
// helper write would overlap the adjacent result and trap cells.
func gcInternalHelperSlotCapacity(descs []gc.TypeDesc, structHelpers, arrayHelpers, genericArrays bool) (int, error) {
	slots := 0
	if structHelpers {
		// final cast/get is the largest fixed struct helper; constructors may be
		// wider and are accounted for below.
		slots = 5
	}
	if arrayHelpers {
		// array.copy is the largest array helper.
		slots = max(slots, 7)
	}
	if genericArrays {
		// RV64 generic array.new_fixed is bounded so its values, type index, and
		// count fit the persisted extended synchronous helper frame.
		slots = max(slots, rv64GenericArrayFixedSlots)
	}
	if !structHelpers {
		return slots, nil
	}
	return gcStructConstructorSlotCapacity(descs, slots)
}

func (c *Compiled) hostCtrlFrameBytes() int {
	bytes, err := coreruntime.HostCtrlFrameBytesForSlots(int(c.syncHostSlots))
	if err != nil {
		panic(err) // validateArenaFootprint establishes this immutable cache.
	}
	return bytes
}
