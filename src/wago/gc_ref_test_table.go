package wago

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

const maxGCRefTestTableSlots = 20

// gcRefTestTableState couples one exact local compact-reference table to checked
// collector roots. The descriptor is arena-owned; the fixed slot array contains
// only collector indexes and grows neither with invocation count nor overwrites.
type gcRefTestTableState struct {
	Descriptor []byte
	Slots      [maxGCRefTestTableSlots]uint32
	Count      uint8
}

func newGCRefTestTableState(collector *gc.Collector, descriptor []byte) (*gcRefTestTableState, error) {
	if collector == nil || len(descriptor) < 8 {
		return nil, fmt.Errorf("GC ref.test table descriptor is unavailable")
	}
	size := int(binary.LittleEndian.Uint32(descriptor))
	capacity := int(binary.LittleEndian.Uint32(descriptor[4:]))
	if size < 0 || size > maxGCRefTestTableSlots || capacity < size || 8+capacity*8 > len(descriptor) {
		return nil, fmt.Errorf("GC ref.test table shape size=%d capacity=%d bytes=%d exceeds bound %d", size, capacity, len(descriptor), maxGCRefTestTableSlots)
	}
	state := &gcRefTestTableState{Descriptor: descriptor, Count: uint8(size)}
	for i := 0; i < size; i++ {
		off := 8 + i*8
		if binary.LittleEndian.Uint64(descriptor[off:off+8]) != 0 {
			state.drop(collector)
			return nil, fmt.Errorf("GC ref.test table slot %d is not initially null", i)
		}
		slot, err := collector.NewCheckedTableSlot(gc.Null())
		if err != nil {
			state.drop(collector)
			return nil, err
		}
		state.Slots[i] = slot
	}
	return state, nil
}

func (s *gcRefTestTableState) set(collector *gc.Collector, index uint64, ref gc.Ref) error {
	if s == nil || collector == nil || index >= uint64(s.Count) {
		return fmt.Errorf("GC ref.test table index %d out of bounds", index)
	}
	if err := collector.SetTableSlot(s.Slots[index], ref); err != nil {
		return err
	}
	off := 8 + int(index)*8
	binary.LittleEndian.PutUint64(s.Descriptor[off:off+8], uint64(ref))
	return nil
}

func (s *gcRefTestTableState) drop(collector *gc.Collector) {
	if s == nil || collector == nil {
		return
	}
	for i := uint8(0); i < s.Count; i++ {
		_ = collector.SetTableSlot(s.Slots[i], gc.Null())
		off := 8 + int(i)*8
		if off+8 <= len(s.Descriptor) {
			binary.LittleEndian.PutUint64(s.Descriptor[off:off+8], 0)
		}
	}
}

func (in *Instance) existingGCRefTestTableState() *gcRefTestTableState {
	if in == nil {
		return nil
	}
	state := in.pluginState.Load()
	if state == nil {
		return nil
	}
	return state.gcRefTestTable.Load()
}
