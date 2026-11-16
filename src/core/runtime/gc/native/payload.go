package gc

import "errors"

// payloadBytes returns real mutable object storage after the physical header.
// It includes the existing allocator's tail padding; logical views must bound it
// by descriptor size/array length. No allocation, copy, collection or root change
// occurs. The caller already owns a valid reference and a serialized heap lease.
// A future distinct-layout backend must implement this boundary, never synthesize
// an expanded mutable native-header buffer. Current layouts are unchanged.
func (c *Collector) payloadBytes(r Ref) []byte { return objectPayload(c.bytes(r)) }

func objectPayload(object []byte) []byte {
	// A damaged short object must still reach the existing accessor bounds/type
	// decision instead of introducing an earlier slice panic.
	if len(object) < int(PayloadOffset) {
		return nil
	}
	return object[PayloadOffset:]
}

// refDescPtrPayload retains single-resolution typed access while keeping callers'
// field/element offsets independent of physical header layout.
func (c *Collector) refDescPtrPayload(r Ref) (*TypeDesc, []byte, error) {
	d, object, err := c.refDescPtrBytes(r)
	if err != nil {
		return nil, nil, err
	}
	return d, objectPayload(object), nil
}

// checkedHeaderPayload preserves the final-reference fastpath's explicit header
// bound failure and exposes the header only as a value, never writable storage.
func (c *Collector) checkedHeaderPayload(r Ref) (ObjHeader, []byte, error) {
	object := c.bytes(r)
	if len(object) < int(HeaderSize) {
		return ObjHeader{}, nil, errors.New("gc: object header out of bounds")
	}
	return decodeHeader(object), objectPayload(object), nil
}

func (c *Collector) loadPayloadValue(r Ref, off uint64, k StorageKind) (Value, error) {
	return loadValueBytes(c.payloadBytes(r), off, k)
}
func (c *Collector) storePayloadValue(r Ref, off uint64, k StorageKind, v Value) error {
	return storeValueBytes(c.payloadBytes(r), off, k, v)
}
