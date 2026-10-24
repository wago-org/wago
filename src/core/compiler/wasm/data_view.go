package wasm

// Each span indexes one original section payload. Active offsets include their
// terminating end; a zero offsetEnd denotes passive mode. No valid memory index
// or offset value is reserved as a tag.
type dataSegmentSpan struct {
	memory, offsetStart, offsetEnd, initStart, initEnd uint32
}

type dataSectionView struct {
	bytes []byte
	spans []dataSegmentSpan
}

// DataSegmentCount includes compiler byte-backed and ordinary expanded records.
func (m *Module) DataSegmentCount() int {
	if m.dataView != nil && m.Data == nil {
		return len(m.dataView.spans)
	}
	return len(m.Data)
}

// DataSegment projects a read-only compiler record without allocating. All
// slices borrow the original input, as ordinary byte-backed decoding does.
func (m *Module) DataSegment(i int) Data {
	if m.dataView == nil || m.Data != nil {
		return m.Data[i]
	}
	v := m.dataView
	s := v.spans[i]
	d := Data{Init: v.bytes[s.initStart:s.initEnd:s.initEnd]}
	if s.offsetEnd == 0 {
		d.Mode.Kind = DataPassive
	} else {
		d.Mode = DataMode{Kind: DataActive, Mem: MemIdx(s.memory), Offset: Expr{BodyBytes: v.bytes[s.offsetStart:s.offsetEnd:s.offsetEnd]}}
	}
	return d
}

// ExpandedDataSegments preserves slice-based consumers. Compiler paths should
// prefer DataSegment so large data vectors stay compact. Expanding a private
// view does not mutate the module or retain the new slice in its identity.
func (m *Module) ExpandedDataSegments() []Data {
	if m.dataView == nil || m.Data != nil {
		return m.Data
	}
	out := make([]Data, m.DataSegmentCount())
	for i := range out {
		out[i] = m.DataSegment(i)
	}
	return out
}

// DataSegmentIsPassive avoids constructing an expression when only mode is used.
func (m *Module) DataSegmentIsPassive(i int) bool {
	if m.dataView != nil && m.Data == nil {
		return m.dataView.spans[i].offsetEnd == 0
	}
	return m.Data[i].Mode.Kind == DataPassive
}
