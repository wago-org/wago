package gc

import (
	"errors"
	"fmt"
)

// ArrayPayloadView is a zero-copy view of one live array payload. The caller
// must serialize collector mutation for the complete lifetime of Bytes. Wago's
// host-call guest-storage API does this by holding the collector-domain lease
// until the host callback returns.
//
// Bytes contains exactly Length logical elements in their in-memory scalar
// representation. It contains no object header or allocation padding.
type ArrayPayloadView struct {
	Storage StorageKind
	Length  uint32
	Bytes   []byte
}

// ArrayPayload returns the contiguous payload of a live non-reference array.
// The returned slice is valid only while the caller prevents collection or
// relocation. Reference arrays must use ArrayGet so write barriers and typed
// reference semantics cannot be bypassed.
func (c *Collector) ArrayPayload(ref Ref) (ArrayPayloadView, error) {
	d, err := c.refDesc(ref)
	if err != nil {
		return ArrayPayloadView{}, err
	}
	if d.Kind != KindArray {
		return ArrayPayloadView{}, errors.New("gc: not array")
	}
	if isAnyReferenceStorage(d.Elem) {
		return ArrayPayloadView{}, errors.New("gc: reference array has no raw payload view")
	}
	length := c.header(ref).Aux
	payloadBytes := uint64(length) * uint64(d.ElemSize)
	if length != 0 && payloadBytes/uint64(length) != uint64(d.ElemSize) {
		return ArrayPayloadView{}, errors.New("gc: array payload size overflow")
	}
	object := c.bytes(ref)
	if uint64(len(object)) < uint64(PayloadOffset) || payloadBytes > uint64(len(object))-uint64(PayloadOffset) {
		return ArrayPayloadView{}, fmt.Errorf("gc: array payload %d bytes exceeds object extent %d", payloadBytes, len(object))
	}
	start := int(PayloadOffset)
	end := start + int(payloadBytes)
	return ArrayPayloadView{Storage: d.Elem, Length: length, Bytes: object[start:end:end]}, nil
}
