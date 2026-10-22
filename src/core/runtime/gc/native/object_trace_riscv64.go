//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package gc

import "errors"

// ObjectTraceCursor keeps a descriptor position across a runtime's joint-domain
// mark steps. It contains no object pointer and must be reset for each object.
type ObjectTraceCursor objectScanCursor

// ObjectRefVisitor consumes a child reference in this collector's domain.
// The visitor must not mutate collector objects, roots, or the edge source.
type ObjectRefVisitor interface{ VisitObjectRef(Ref) }

// PersistentSlotCount and PersistentSlotRef expose the stable slot directories
// to a bounded joint-domain owner walk. The collector mutator lock is required.
func (c *Collector) PersistentSlotCount(kind SlotKind) uint32 {
	switch kind {
	case SlotGlobal:
		return uint32(len(c.globalSlots))
	case SlotTable:
		return uint32(len(c.tableSlots))
	default:
		panic("gc: invalid persistent slot directory")
	}
}

func (c *Collector) PersistentSlotRef(kind SlotKind, index uint32) (Ref, error) {
	if err := c.errIfClosed(); err != nil {
		return Null(), err
	}
	switch kind {
	case SlotGlobal:
		return c.CheckedGlobalSlot(index)
	case SlotTable:
		return c.CheckedTableSlot(index)
	default:
		return Null(), errors.New("gc: invalid persistent slot directory")
	}
}

// ObjectMayHaveChildren lets a joint mark avoid a queue row for scalar leaves.
// A private Env can still carry a cross-domain producer edge in runtime metadata.
func (c *Collector) ObjectMayHaveChildren(ref Ref) (bool, error) {
	desc, err := c.refDesc(ref)
	if err != nil {
		return false, err
	}
	return desc.HasRefs || c.objectEdgeCount(ref) != 0, nil
}

// TraceObjectChildren reads at most entries descriptor or ownership entries.
// It does not change collector marks. The runtime must hold this collector's
// mutator lock; the cursor and edge indexes follow ObjectEdgeSource's rules.
func (c *Collector) TraceObjectChildren(ref Ref, cursor *ObjectTraceCursor, entries uint32, visitor ObjectRefVisitor) (used uint32, complete bool, err error) {
	if cursor == nil || visitor == nil || entries == 0 || entries > 65536 {
		return 0, false, errors.New("gc: invalid bounded object trace request")
	}
	if err := c.errIfClosed(); err != nil {
		return 0, false, err
	}
	if !ref.IsObj() || !c.validObjectRef(ref) {
		return 0, false, errors.New("gc: object trace requires a live object")
	}
	work, complete := c.scanObjectRefsRange(handleOf(ref), (*objectScanCursor)(cursor), objectScanBudget{
		ObjectRanges: 1, ScanEntries: entries, RefSlots: entries, PayloadBytes: entries * 16,
	}, objectScanVisitor{fn: visitor.VisitObjectRef})
	return work.ScanEntries, complete, nil
}

// TracePersistentRoots reads a bounded range of one persistent slot directory.
// Conditional slots are omitted: the runtime must reach their managed owner.
// The callback always receives references belonging to this collector.
func (c *Collector) TracePersistentRoots(kind SlotKind, index, entries uint32, visitor ObjectRefVisitor) (next uint32, complete bool, err error) {
	if visitor == nil || entries == 0 || entries > 65536 {
		return index, false, errors.New("gc: invalid bounded persistent root trace request")
	}
	if err := c.errIfClosed(); err != nil {
		return index, false, err
	}
	var slots []Ref
	switch kind {
	case SlotGlobal:
		slots = c.globalSlots
	case SlotTable:
		slots = c.tableSlots
	default:
		return index, false, errors.New("gc: invalid persistent root trace directory")
	}
	if uint64(index) > uint64(len(slots)) {
		return index, false, errors.New("gc: persistent root trace cursor outside directory")
	}
	for entries != 0 && uint64(index) < uint64(len(slots)) {
		if c.persistentRootIsStrong(kind, index) {
			visitor.VisitObjectRef(slots[index])
		}
		index++
		entries--
	}
	return index, uint64(index) == uint64(len(slots)), nil
}
