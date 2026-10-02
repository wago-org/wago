package wago

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// gcRefTableKind is a ref-table state table's ownership class.
type gcRefTableKind uint8

const (
	// gcRefTableOpaque tables (funcref descriptors, plain externref handles)
	// carry no collector or conversion obligations of their own.
	gcRefTableOpaque gcRefTableKind = iota
	// gcRefTableRoot tables hold compact collector refs, each paired with a
	// checked collector root slot, or verified foreign anyref words.
	gcRefTableRoot
	// gcRefTableExtern tables transfer conversion-identity ownership on writes.
	gcRefTableExtern
)

// gcRefTestTableSpec describes one local table handed to the state.
type gcRefTestTableSpec struct {
	Descriptor []byte
	EntryBytes int
	Kind       gcRefTableKind
}

// gcRefTestTableState couples exact local reference tables to their distinct
// owners. Collector-reference tables use compact arena entries paired with
// checked collector roots. Funcref tables retain native descriptors only.
// Externref tables use public store tokens or bounded conversion identities;
// neither category is ever scanned as gc.Ref.
type gcRefTestTableState struct {
	// Descriptor, Slots and Count alias the first root table (RootTable), the
	// one the single-table products and array element helpers address.
	Descriptor    []byte
	Descriptors   [][]byte
	CanonicalType *gc.TypeCanonicalization
	Conversion    *gcExternConversionState
	Slots         []uint32
	Count         uint32
	TableCount    uint8
	RootTable     uint8
	kinds         []gcRefTableKind
	entryBytes    []int
	slots         [][]uint32
}

// newGCRefTestTableState builds the state for the historical fixed layouts:
// one root table, or [root, funcref, externref].
func newGCRefTestTableState(collector *gc.Collector, descriptors [][]byte, rootTable uint8, canonicalTypes []gc.TypeID) (*gcRefTestTableState, error) {
	specs := make([]gcRefTestTableSpec, len(descriptors))
	for i, descriptor := range descriptors {
		specs[i] = gcRefTestTableSpec{Descriptor: descriptor, EntryBytes: 8}
		switch {
		case i == int(rootTable):
			specs[i].Kind = gcRefTableRoot
		case i == 1 && len(descriptors) == 3:
			specs[i].EntryBytes = 32
		case i == 2 && len(descriptors) == 3:
			specs[i].Kind = gcRefTableExtern
		}
	}
	if len(descriptors) == 0 || int(rootTable) >= len(descriptors) {
		return nil, fmt.Errorf("GC ref.test table descriptors are unavailable")
	}
	return newGCRefTestTableStateFor(collector, specs, canonicalTypes)
}

// newGCRefTestTableStateFor builds the state for any number of local tables.
// Root tables must start null or hold compact collector refs.
func newGCRefTestTableStateFor(collector *gc.Collector, specs []gcRefTestTableSpec, canonicalTypes []gc.TypeID) (*gcRefTestTableState, error) {
	if collector == nil || len(specs) == 0 || len(specs) > 255 {
		return nil, fmt.Errorf("GC ref.test table descriptors are unavailable")
	}
	state := &gcRefTestTableState{
		TableCount:  uint8(len(specs)),
		RootTable:   uint8(len(specs)),
		Descriptors: make([][]byte, len(specs)),
		kinds:       make([]gcRefTableKind, len(specs)),
		entryBytes:  make([]int, len(specs)),
		slots:       make([][]uint32, len(specs)),
	}
	for i, spec := range specs {
		descriptor := spec.Descriptor
		if len(descriptor) < 8 || (spec.EntryBytes != 8 && spec.EntryBytes != 32) || (spec.Kind != gcRefTableOpaque && spec.EntryBytes != 8) {
			return nil, fmt.Errorf("GC ref.test table %d descriptor is unavailable", i)
		}
		size := int(binary.LittleEndian.Uint32(descriptor))
		capacity := int(binary.LittleEndian.Uint32(descriptor[4:]))
		if size < 0 || capacity < size || 8+capacity*spec.EntryBytes > len(descriptor) {
			return nil, fmt.Errorf("GC ref.test table %d shape size=%d capacity=%d bytes=%d is invalid", i, size, capacity, len(descriptor))
		}
		state.Descriptors[i] = descriptor
		state.kinds[i] = spec.Kind
		state.entryBytes[i] = spec.EntryBytes
		if spec.Kind == gcRefTableRoot && int(state.RootTable) == len(specs) {
			state.RootTable = uint8(i)
		}
	}
	if canonicalTypes != nil {
		canonical, err := collector.NewTypeCanonicalization(canonicalTypes)
		if err != nil {
			return nil, err
		}
		state.CanonicalType = canonical
	}
	for i := range specs {
		if state.kinds[i] != gcRefTableRoot {
			continue
		}
		descriptor := state.Descriptors[i]
		size := int(binary.LittleEndian.Uint32(descriptor))
		slots := make([]uint32, size)
		state.slots[i] = slots
		for j := 0; j < size; j++ {
			off := 8 + j*8
			word := binary.LittleEndian.Uint64(descriptor[off : off+8])
			if word>>32 != 0 {
				state.drop(collector)
				return nil, fmt.Errorf("GC ref.test table %d slot %d holds a non-compact initial reference", i, j)
			}
			slot, err := collector.NewCheckedTableSlot(gc.Ref(uint32(word)))
			if err != nil {
				state.drop(collector)
				return nil, err
			}
			slots[j] = slot
		}
	}
	if root := int(state.RootTable); root < len(specs) {
		state.Descriptor = state.Descriptors[root]
		state.Slots = state.slots[root]
		state.Count = uint32(len(state.Slots))
	}
	return state, nil
}

// gcRefTestTableSpecs classifies every local table of a ref-table product by
// its element type: collector-reference tables are rooted, externref tables
// carry conversion ownership, and funcref tables stay native.
func gcRefTestTableSpecs(c *Compiled, descriptors [][]byte) ([]gcRefTestTableSpec, error) {
	specs := make([]gcRefTestTableSpec, len(descriptors))
	for i, descriptor := range descriptors {
		if descriptor == nil {
			return nil, fmt.Errorf("GC ref.test product table %d is imported or unavailable", i)
		}
		specs[i] = gcRefTestTableSpec{Descriptor: descriptor, EntryBytes: c.tableEntryBytes(i)}
		switch typ := c.tableElementType(i); {
		case isGCRefValType(typ):
			specs[i].Kind = gcRefTableRoot
		case typ == ValExternRef:
			specs[i].Kind = gcRefTableExtern
		}
	}
	return specs, nil
}

func (s *gcRefTestTableState) attachConversion(conversion *gcExternConversionState) error {
	if s == nil || conversion == nil {
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
	switch s.kinds[table] {
	case gcRefTableRoot:
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
		slots := s.slots[table]
		if index >= uint64(len(slots)) {
			return fmt.Errorf("GC ref.test table %d index %d has no collector root", table, index)
		}
		if err := collector.SetTableSlot(slots[index], root); err != nil {
			return err
		}
	case gcRefTableExtern:
		if s.Conversion == nil {
			return fmt.Errorf("GC ref.test extern table has no conversion owner")
		}
		off := 8 + int(index)*8
		old := binary.LittleEndian.Uint64(descriptor[off : off+8])
		if err := s.Conversion.replaceExtern(old, word); err != nil {
			return err
		}
	}
	if s.entryBytes[table] != 8 {
		return fmt.Errorf("GC ref.test funcref table mutation must use native descriptor copying")
	}
	off := 8 + int(index)*8
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
	for table, slots := range s.slots {
		descriptor := s.Descriptors[table]
		for i, slot := range slots {
			_ = collector.SetTableSlot(slot, gc.Null())
			off := 8 + i*8
			if off+8 <= len(descriptor) {
				binary.LittleEndian.PutUint64(descriptor[off:off+8], 0)
			}
		}
	}
	if s.Conversion != nil {
		_ = s.Conversion.close()
	}
	for table, descriptor := range s.Descriptors {
		if len(descriptor) < 8 {
			continue
		}
		size := int(binary.LittleEndian.Uint32(descriptor))
		clear(descriptor[8 : 8+size*s.entryBytes[table]])
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
