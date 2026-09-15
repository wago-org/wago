//go:build wago_gcstats

package gc

const (
	DiagnosticSpaceInvalid uint8 = iota
	DiagnosticSpaceImmediate
	DiagnosticSpaceNursery
	DiagnosticSpaceOld
	DiagnosticSpaceLarge
	DiagnosticSpaceTiny
)

// DiagnosticObjectStore reports collector placement for a prospective object
// store. It is available only to diagnostic wago_gcstats builds.
func (c *Collector) DiagnosticObjectStore(parent, child Ref) (parentSpace, childSpace uint8, parentRemembered bool) {
	if c == nil || !parent.IsObj() || !c.validObjectRef(parent) {
		return DiagnosticSpaceInvalid, diagnosticRefSpace(c, child), false
	}
	entry := c.entry(parent)
	return diagnosticSpace(entry.space), diagnosticRefSpace(c, child), entry.remembered
}

// DiagnosticArrayCard reports whether parent has valid fixed-card metadata and
// whether one linked payload-byte range covers index.
func (c *Collector) DiagnosticArrayCard(parent Ref, index uint32) (present, covers bool) {
	if c == nil || !parent.IsObj() || !c.validObjectRef(parent) {
		return false, false
	}
	handle := handleOf(parent)
	slot := c.handles[handle].cardSlot
	if slot == 0 || !slotIndexOK(slot-1, len(c.objectCards)) {
		return false, false
	}
	d, err := c.refDesc(parent)
	if err != nil || d.Kind != KindArray || d.ElemSize == 0 {
		return false, false
	}
	off := uint64(index) * uint64(d.ElemSize)
	if off > uint64(^uint32(0)) {
		return true, false
	}
	for steps := 0; slot != 0 && steps <= len(c.objectCards); steps++ {
		if !slotIndexOK(slot-1, len(c.objectCards)) {
			return false, false
		}
		pos := slot - 1
		card := c.objectCards[pos]
		if card.handle != handle || card.end < card.index {
			return false, false
		}
		if uint32(off) >= card.index && uint32(off) <= card.end {
			return true, true
		}
		slot = card.next
	}
	return true, false
}

func diagnosticRefSpace(c *Collector, ref Ref) uint8 {
	if ref.IsNull() || ref.IsI31() {
		return DiagnosticSpaceImmediate
	}
	if c == nil || !ref.IsObj() || !c.validObjectRef(ref) {
		return DiagnosticSpaceInvalid
	}
	return diagnosticSpace(c.entry(ref).space)
}

func diagnosticSpace(space spaceKind) uint8 {
	switch space {
	case spaceNursery:
		return DiagnosticSpaceNursery
	case spaceOld:
		return DiagnosticSpaceOld
	case spaceLarge:
		return DiagnosticSpaceLarge
	case spaceTiny:
		return DiagnosticSpaceTiny
	default:
		return DiagnosticSpaceInvalid
	}
}
