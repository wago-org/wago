package gc

import "errors"

// NewArrayDataWithRoots initializes a numeric array directly from stable source
// bytes. The source must not alias this collector's movable heap. All checks
// precede allocation; no collection occurs between allocation and publication
// of the completely initialized result.
func (c *Collector) NewArrayDataWithRoots(typeID TypeID, length uint32, data []byte, source uint32, roots RootSet) (Ref, error) {
	d, err := c.desc(typeID)
	if err != nil {
		return Null(), err
	}
	if d.Kind != KindArray || !isNumericStorage(d.Elem) {
		return Null(), errors.New("gc: array data destination is not numeric")
	}
	size, err := ArraySize(d, length)
	if err != nil {
		return Null(), err
	}
	bytes := uint64(length) * uint64(d.ElemSize)
	end := uint64(source) + bytes
	if end > uint64(len(data)) {
		return Null(), errors.New("gc: data source out of range")
	}
	r, err := c.alloc(d, size, length, roots)
	if err != nil {
		return Null(), err
	}
	payloadEnd := uint32(uint64(PayloadOffset) + bytes)
	copy(c.bytes(r)[PayloadOffset:payloadEnd], data[uint64(source):end])
	// Clear alignment and size-class padding without overwriting the payload.
	e := c.entry(r)
	start, limit := e.off+payloadEnd, e.off+e.size
	switch e.space {
	case spaceNursery:
		clear(c.nursery[start:limit])
	case spaceOld, spaceLarge:
		limit = e.off + c.throughput.handleAllocSize(*e)
		clear(c.throughput.mem[start:limit])
	case spaceTiny:
		clear(c.tiny.mem[start:limit])
	}
	return r, nil
}
