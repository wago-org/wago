package embedded32

import "encoding/binary"

// TransportCancelHandler is the interrupt-safe subset used to signal a running
// firmware invocation as soon as a complete cancel request reaches the board.
type TransportCancelHandler interface {
	Cancel() TransportCode
}

// TransportCancelObserver tracks transport framing without consuming the
// stream. A USB receive interrupt can feed it before buffering the same bytes
// for the normal synchronous endpoint. This lets a running native invocation
// observe cancellation while preserving the request for its ordered response.
type TransportCancelObserver struct {
	Handler        TransportCancelHandler
	MaximumPayload uint32

	header    [TransportHeaderBytes]byte
	received  uint32
	remaining uint32
}

// Observe accepts the next contiguous bytes from the transport stream.
func (o *TransportCancelObserver) Observe(src []byte) {
	if o == nil || o.Handler == nil || o.MaximumPayload == 0 {
		return
	}
	for len(src) != 0 {
		if o.remaining != 0 {
			skipped := min(uint32(len(src)), o.remaining)
			src = src[skipped:]
			o.remaining -= skipped
			continue
		}
		needed := TransportHeaderBytes - o.received
		copied := min(uint32(len(src)), needed)
		copy(o.header[o.received:o.received+copied], src[:copied])
		o.received += copied
		src = src[copied:]
		if o.received != TransportHeaderBytes {
			continue
		}

		o.received = 0
		payloadLength := binary.LittleEndian.Uint32(o.header[12:16])
		if payloadLength != 0 {
			o.remaining = payloadLength
			continue
		}
		frame, consumed, err := DecodeTransportFrame(o.header[:], o.MaximumPayload)
		if err == nil && consumed == TransportHeaderBytes && frame.Kind == TransportCancel {
			o.Handler.Cancel()
		}
	}
}
