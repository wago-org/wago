package wago

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

const (
	maxGCRefTestTableSlots = 20
	maxGCRefTestTables     = 3
)

// gcRefTestTableState couples exact local mixed-reference tables to their
// distinct owners. The any/data table uses compact arena entries paired with
// checked collector roots. The funcref table retains native descriptors only.
// The externref table uses public store tokens or bounded conversion identities;
// neither category is ever scanned as gc.Ref.
type gcRefTestTableState struct {
	Descriptor    []byte
	Descriptors   [maxGCRefTestTables][]byte
	CanonicalType *gc.TypeCanonicalization
	Conversion    *gcExternConversionState
	Slots         [maxGCRefTestTableSlots]uint32
	Count         uint8
	TableCount    uint8
	RootTable     uint8
}

func newGCRefTestTableState(collector *gc.Collector, descriptors [][]byte, rootTable uint8, canonicalTypes []gc.TypeID) (*gcRefTestTableState, error) {
	if collector == nil || len(descriptors) == 0 || len(descriptors) > maxGCRefTestTables || int(rootTable) >= len(descriptors) {
		return nil, fmt.Errorf("GC ref.test table descriptors are unavailable")
	}
	state := &gcRefTestTableState{TableCount: uint8(len(descriptors)), RootTable: rootTable}
	for i, descriptor := range descriptors {
		if len(descriptor) < 8 {
			return nil, fmt.Errorf("GC ref.test table %d descriptor is unavailable", i)
		}
		size := int(binary.LittleEndian.Uint32(descriptor))
		capacity := int(binary.LittleEndian.Uint32(descriptor[4:]))
		entryBytes := 8
		if i == 1 && len(descriptors) == 3 {
			entryBytes = 32
		}
		if size < 0 || capacity < size || 8+capacity*entryBytes > len(descriptor) {
			return nil, fmt.Errorf("GC ref.test table %d shape size=%d capacity=%d bytes=%d is invalid", i, size, capacity, len(descriptor))
		}
		state.Descriptors[i] = descriptor
	}
	state.Descriptor = state.Descriptors[rootTable]
	size := int(binary.LittleEndian.Uint32(state.Descriptor))
	if size > maxGCRefTestTableSlots {
		return nil, fmt.Errorf("GC ref.test root table size %d exceeds bound %d", size, maxGCRefTestTableSlots)
	}
	state.Count = uint8(size)
	if canonicalTypes != nil {
		canonical, err := collector.NewTypeCanonicalization(canonicalTypes)
		if err != nil {
			return nil, err
		}
		state.CanonicalType = canonical
	}
	for i := 0; i < size; i++ {
		off := 8 + i*8
		if binary.LittleEndian.Uint64(state.Descriptor[off:off+8]) != 0 {
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

func (s *gcRefTestTableState) attachConversion(conversion *gcExternConversionState) error {
	if s == nil || conversion == nil || (s.TableCount != 1 && s.TableCount != 3) {
		return fmt.Errorf("GC conversion table state is unavailable")
	}
	if s.Conversion != nil {
		return fmt.Errorf("GC ref.test mixed-table conversion state is already attached")
	}
	s.Conversion = conversion
	return nil
}

func (s *gcRefTestTableState) set(collector *gc.Collector, index uint64, ref gc.Ref) error {
	return s.setTable(collector, uint64(s.RootTable), index, uint64(ref))
}

func (s *gcRefTestTableState) setTable(collector *gc.Collector, table, index, word uint64) error {
	if s == nil || collector == nil || table >= uint64(s.TableCount) {
		return fmt.Errorf("GC ref.test table %d is unavailable", table)
	}
	descriptor := s.Descriptors[table]
	size := uint64(binary.LittleEndian.Uint32(descriptor))
	if index >= size {
		return fmt.Errorf("GC ref.test table %d index %d out of bounds", table, index)
	}
	if table == uint64(s.RootTable) {
		root := gc.Null()
		if word>>32 != 0 {
			if s.Conversion == nil {
				return fmt.Errorf("GC ref.test foreign anyref has no conversion owner")
			}
			foreign, err := s.Conversion.isForeignAny(word)
			if err != nil {
				return err
			}
			if !foreign {
				return fmt.Errorf("invalid or forged internal anyref word %#x", word)
			}
		} else {
			root = gc.Ref(uint32(word))
		}
		if err := collector.SetTableSlot(s.Slots[index], root); err != nil {
			return err
		}
	} else if s.TableCount == 3 && table == 2 {
		if s.Conversion == nil {
			return fmt.Errorf("GC ref.test extern table has no conversion owner")
		}
		off := 8 + int(index)*8
		old := binary.LittleEndian.Uint64(descriptor[off : off+8])
		if err := s.Conversion.replaceExtern(old, word); err != nil {
			return err
		}
	}
	entryBytes := 8
	if s.TableCount == 3 && table == 1 {
		entryBytes = 32
	}
	off := 8 + int(index)*entryBytes
	if entryBytes != 8 {
		return fmt.Errorf("GC ref.test funcref table mutation must use native descriptor copying")
	}
	binary.LittleEndian.PutUint64(descriptor[off:off+8], word)
	return nil
}

func (s *gcRefTestTableState) refCast(collector *gc.Collector, word uint64, target gc.RefTestTarget) (uint64, error) {
	matched, err := s.refTest(collector, word, target)
	if err != nil {
		return 0, err
	}
	if !matched {
		return 0, gc.ErrCastFailure
	}
	return word, nil
}

func (s *gcRefTestTableState) refTest(collector *gc.Collector, word uint64, target gc.RefTestTarget) (bool, error) {
	if word>>32 != 0 {
		if s == nil || s.Conversion == nil {
			return false, fmt.Errorf("GC ref.test foreign anyref has no conversion owner")
		}
		foreign, err := s.Conversion.isForeignAny(word)
		if err != nil {
			return false, err
		}
		if !foreign {
			return false, fmt.Errorf("invalid or forged internal anyref word %#x", word)
		}
		switch target.Kind {
		case gc.RefTestAny:
			return true, nil
		case gc.RefTestEq, gc.RefTestI31, gc.RefTestStruct, gc.RefTestArray, gc.RefTestNone, gc.RefTestDefined:
			return false, nil
		default:
			return false, fmt.Errorf("unsupported foreign anyref test kind %d", target.Kind)
		}
	}
	ref := gc.Ref(uint32(word))
	if s != nil && s.CanonicalType != nil {
		return collector.RefTestCanonical(ref, target, s.CanonicalType)
	}
	return collector.RefTest(ref, target)
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
	if s.Conversion != nil {
		_ = s.Conversion.close()
	}
	for table := uint8(0); table < s.TableCount; table++ {
		descriptor := s.Descriptors[table]
		if len(descriptor) < 8 {
			continue
		}
		entryBytes := 8
		if s.TableCount == 3 && table == 1 {
			entryBytes = 32
		}
		size := int(binary.LittleEndian.Uint32(descriptor))
		clear(descriptor[8 : 8+size*entryBytes])
	}
}

func (in *Instance) existingGCExternConversionState() *gcExternConversionState {
	state := in.existingGCRefTestTableState()
	if state == nil {
		return nil
	}
	return state.Conversion
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
