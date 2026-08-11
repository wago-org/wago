package gc

import "encoding/binary"

// objectScanCursor is the next descriptor entry or array element to examine.
// It contains no heap pointer: callers may retain it across collector steps and
// reacquire the object's descriptor and bytes from the stable handle each time.
type objectScanCursor struct {
	index uint32
}

type objectScanVisitMode uint8

const (
	objectScanVisitFunc objectScanVisitMode = iota
	objectScanVisitTinyMark
)

type objectScanVisitor struct {
	mode objectScanVisitMode
	fn   func(Ref)
}

func (v objectScanVisitor) visit(c *Collector, ref Ref) {
	if v.mode == objectScanVisitTinyMark {
		c.tinyMarkRef(ref)
		return
	}
	if v.fn != nil {
		v.fn(ref)
	}
}

// objectScanWork is the deterministic work performed by one or more scan
// ranges. ObjectRanges bounds per-object setup even for pointer-free objects;
// the other dimensions prevent a single large reference-bearing object from
// defeating an incremental budget.
type objectScanWork struct {
	ObjectRanges uint32
	ScanEntries  uint32
	RefSlots     uint32
	PayloadBytes uint32
}

// objectScanBudget is a compact vector limit. A range stops before the next
// entry if consuming it would exceed any dimension.
type objectScanBudget objectScanWork

var completeObjectScanBudget = objectScanBudget{
	ObjectRanges: ^uint32(0),
	ScanEntries:  ^uint32(0),
	RefSlots:     ^uint32(0),
	PayloadBytes: ^uint32(0),
}

func (w *objectScanWork) add(other objectScanWork) {
	w.ObjectRanges += other.ObjectRanges
	w.ScanEntries += other.ScanEntries
	w.RefSlots += other.RefSlots
	w.PayloadBytes += other.PayloadBytes
}

func (b objectScanBudget) remaining(used objectScanWork) objectScanBudget {
	return objectScanBudget{
		ObjectRanges: b.ObjectRanges - used.ObjectRanges,
		ScanEntries:  b.ScanEntries - used.ScanEntries,
		RefSlots:     b.RefSlots - used.RefSlots,
		PayloadBytes: b.PayloadBytes - used.PayloadBytes,
	}
}

func (b objectScanBudget) permits(work objectScanWork) bool {
	return work.ObjectRanges <= b.ObjectRanges &&
		work.ScanEntries <= b.ScanEntries &&
		work.RefSlots <= b.RefSlots &&
		work.PayloadBytes <= b.PayloadBytes
}

// scanObjectRefsRange visits references in descriptor order, stopping before an
// entry that does not fit in budget. The caller owns cursor and may retain it
// across calls. No descriptor slice or object-byte pointer escapes this call.
func (c *Collector) scanObjectRefsRange(h uint32, cursor *objectScanCursor, budget objectScanBudget, visitor objectScanVisitor) (work objectScanWork, complete bool) {
	if cursor == nil || !budget.permits(objectScanWork{ObjectRanges: 1}) {
		return objectScanWork{}, false
	}
	work.ObjectRanges = 1
	if h == 0 || int(h) >= len(c.handles) || c.handles[h].space == spaceFree {
		return work, true
	}
	r := makeObjRef(h)
	d, err := c.refDesc(r)
	if err != nil || !d.HasRefs {
		return work, true
	}
	b := c.bytes(r)
	// Synchronous sweep barriers may request an effectively unlimited Tiny scan.
	// Avoid finite-budget checks in that cold path. Ordinary complete scans use
	// scanObjectRefsComplete below so Throughput retains its established hot loop.
	if budget == completeObjectScanBudget && cursor.index == 0 && visitor.mode == objectScanVisitTinyMark {
		switch d.Kind {
		case KindStruct:
			work.ScanEntries = uint32(len(d.Fields))
			work.PayloadBytes = d.Size
			for _, f := range d.Fields {
				if isCollectorRefKind(f.Kind) {
					c.tinyMarkRef(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+f.Offset:])))
					work.RefSlots++
				}
			}
			cursor.index = uint32(len(d.Fields))
			return work, true
		case KindArray:
			if !d.ArrayElementsAreRefs() {
				return work, true
			}
			length := c.header(r).Aux
			work.ScanEntries = length
			work.RefSlots = length
			work.PayloadBytes = length * d.ElemSize
			for cursor.index < length {
				off := uint64(PayloadOffset) + uint64(cursor.index)*uint64(d.ElemSize)
				c.tinyMarkRef(Ref(binary.LittleEndian.Uint32(b[off:])))
				cursor.index++
			}
			return work, true
		default:
			return work, true
		}
	}
	switch d.Kind {
	case KindStruct:
		for cursor.index < uint32(len(d.Fields)) {
			f := d.Fields[cursor.index]
			_, size, err := storageLayout(f.Kind)
			if err != nil {
				return work, true
			}
			isRef := isCollectorRefKind(f.Kind)
			if work.ScanEntries == budget.ScanEntries || size > budget.PayloadBytes-work.PayloadBytes || (isRef && work.RefSlots == budget.RefSlots) {
				return work, false
			}
			if isRef {
				visitor.visit(c, Ref(binary.LittleEndian.Uint32(b[PayloadOffset+f.Offset:])))
				work.RefSlots++
			}
			cursor.index++
			work.ScanEntries++
			work.PayloadBytes += size
		}
		return work, true
	case KindArray:
		if !d.ArrayElementsAreRefs() {
			return work, true
		}
		length := c.header(r).Aux
		count := length - cursor.index
		if available := budget.ScanEntries - work.ScanEntries; count > available {
			count = available
		}
		if available := budget.RefSlots - work.RefSlots; count > available {
			count = available
		}
		if available := (budget.PayloadBytes - work.PayloadBytes) / d.ElemSize; count > available {
			count = available
		}
		end := cursor.index + count
		if visitor.mode == objectScanVisitTinyMark {
			for cursor.index < end {
				off := uint64(PayloadOffset) + uint64(cursor.index)*uint64(d.ElemSize)
				c.tinyMarkRef(Ref(binary.LittleEndian.Uint32(b[off:])))
				cursor.index++
			}
		} else {
			for cursor.index < end {
				off := uint64(PayloadOffset) + uint64(cursor.index)*uint64(d.ElemSize)
				if visitor.fn != nil {
					visitor.fn(Ref(binary.LittleEndian.Uint32(b[off:])))
				}
				cursor.index++
			}
		}
		work.ScanEntries += count
		work.RefSlots += count
		work.PayloadBytes += count * d.ElemSize
		return work, cursor.index == length
	default:
		return work, true
	}
}

func (c *Collector) objectPayloadBytes(h uint32) uint32 {
	if h == 0 || int(h) >= len(c.handles) || c.handles[h].space == spaceFree || c.handles[h].size <= PayloadOffset {
		return 0
	}
	return c.handles[h].size - PayloadOffset
}

// scanObjectRefs is the complete synchronous wrapper used by Throughput/full
// collection and heap helpers. Tiny incremental marking retains the range
// primitive's cursor between bounded steps.
func (c *Collector) scanObjectRefs(h uint32, visit func(Ref)) {
	if !c.telemetryEnabled() {
		// Preserve the established synchronous Throughput loop exactly. The
		// cursor/budget bookkeeping is required only for Tiny bounded marking;
		// measurements showed that imposing it here regresses dense full scans.
		r := makeObjRef(h)
		d, err := c.refDesc(r)
		if err != nil || !d.HasRefs {
			return
		}
		hdr := c.header(r)
		b := c.bytes(r)
		if d.Kind == KindStruct {
			for _, f := range d.Fields {
				if isCollectorRefKind(f.Kind) {
					visit(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+f.Offset:])))
				}
			}
		} else if d.ArrayElementsAreRefs() {
			for i := uint32(0); i < hdr.Aux; i++ {
				off := PayloadOffset + i*d.ElemSize
				visit(Ref(binary.LittleEndian.Uint32(b[off:])))
			}
		}
		return
	}
	start := c.cfg.Telemetry.scanStart()
	cursor := objectScanCursor{}
	visitor := objectScanVisitor{fn: visit}
	var total objectScanWork
	for {
		work, complete := c.scanObjectRefsRange(h, &cursor, completeObjectScanBudget, visitor)
		total.add(work)
		if complete {
			break
		}
	}
	// PayloadBytesVisited predates resumable scanning and reports the complete
	// logical object payload once per object, including layout/alignment padding
	// and pointer-free payloads. Keep that schema meaning while MaxStepPayloadBytes
	// continues to report the actual bounded scan work.
	total.PayloadBytes = c.objectPayloadBytes(h)
	c.cfg.Telemetry.noteObjectScanWork(start, total, true, false, true)
}
