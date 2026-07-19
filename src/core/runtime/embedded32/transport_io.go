package embedded32

import (
	"encoding/binary"
	"io"
)

// ServeTransportOnce reads, dispatches, and writes one complete transport frame
// using caller-owned request and response storage.
func ServeTransportOnce(endpoint *TransportEndpoint, handler TransportHandler, stream io.ReadWriter, request, response []byte) error {
	if endpoint == nil || handler == nil || stream == nil || len(request) < int(TransportHeaderBytes) {
		return ErrTransportFrame
	}
	if _, err := io.ReadFull(stream, request[:TransportHeaderBytes]); err != nil {
		return err
	}
	payloadLength := binary.LittleEndian.Uint32(request[12:16])
	if payloadLength > endpoint.MaximumPayload || uint64(TransportHeaderBytes)+uint64(payloadLength) > uint64(len(request)) {
		return ErrTransportCapacity
	}
	requestLength := TransportHeaderBytes + payloadLength
	if _, err := io.ReadFull(stream, request[TransportHeaderBytes:requestLength]); err != nil {
		return err
	}
	responseLength, err := endpoint.Dispatch(request[:requestLength], response, handler)
	if err != nil {
		return err
	}
	for written := uint32(0); written < responseLength; {
		count, err := stream.Write(response[written:responseLength])
		if count < 0 || uint64(count) > uint64(responseLength-written) {
			return io.ErrShortWrite
		}
		written += uint32(count)
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
