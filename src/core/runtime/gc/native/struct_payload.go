package gc

import "fmt"

// StructPayloadReadOnly returns a borrowed immutable struct payload. The caller
// must prevent collection and relocation until the view is no longer used.
// Writes through this view are forbidden: reference writes need barriers.
func (c *Collector) StructPayloadReadOnly(ref Ref, expected TypeID) ([]byte, error) {
	d, err := c.refDesc(ref)
	if err != nil {
		return nil, err
	}
	if d.Kind != KindStruct || d.ID != expected {
		return nil, fmt.Errorf("gc: unexpected struct payload type")
	}
	object := c.bytes(ref)
	if uint64(PayloadOffset)+uint64(d.Size) > uint64(len(object)) {
		return nil, fmt.Errorf("gc: truncated struct payload")
	}
	return object[PayloadOffset : PayloadOffset+d.Size : PayloadOffset+d.Size], nil
}

// TypeCount reports the immutable descriptor count under the collector lease.
func (c *Collector) TypeCount() uint32 { return uint32(len(c.types)) }
