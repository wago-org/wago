package wago

import (
	"fmt"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc"
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

func (c *Compiled) hostCtrlFrameBytes() int {
	bytes, err := coreruntime.HostCtrlFrameBytesForSlots(int(c.syncHostSlots))
	if err != nil {
		panic(err) // validateArenaFootprint establishes this immutable cache.
	}
	return bytes
}
