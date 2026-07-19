package embedded32

import (
	"io"
	"testing"
)

type testTransportStream struct {
	input       []byte
	readOffset  int
	output      []byte
	writeOffset int
	chunk       int
}

func (s *testTransportStream) Read(destination []byte) (int, error) {
	if s.readOffset == len(s.input) {
		return 0, io.EOF
	}
	length := len(destination)
	if length > s.chunk {
		length = s.chunk
	}
	if length > len(s.input)-s.readOffset {
		length = len(s.input) - s.readOffset
	}
	copy(destination, s.input[s.readOffset:s.readOffset+length])
	s.readOffset += length
	return length, nil
}

func (s *testTransportStream) Write(source []byte) (int, error) {
	length := len(source)
	if length > s.chunk {
		length = s.chunk
	}
	if length > len(s.output)-s.writeOffset {
		length = len(s.output) - s.writeOffset
	}
	copy(s.output[s.writeOffset:s.writeOffset+length], source[:length])
	s.writeOffset += length
	return length, nil
}

func TestServeTransportOnceUsesCallerStorage(t *testing.T) {
	var encoded [TransportHeaderBytes]byte
	requestLength, err := EncodeTransportFrame(encoded[:], TransportFrame{Kind: TransportHello, Sequence: 17})
	if err != nil {
		t.Fatal(err)
	}
	var output [TransportHeaderBytes + TransportHelloBytes]byte
	stream := &testTransportStream{input: encoded[:requestLength], output: output[:], chunk: 3}
	endpoint := &TransportEndpoint{PayloadScratch: make([]byte, TransportHelloBytes), MaximumPayload: 64}
	handler := &testTransportHandler{}
	request := make([]byte, TransportHeaderBytes+64)
	response := make([]byte, len(output))
	if err := ServeTransportOnce(endpoint, handler, stream, request, response); err != nil {
		t.Fatal(err)
	}
	frame, consumed, err := DecodeTransportFrame(output[:stream.writeOffset], 64)
	if err != nil || consumed != uint32(stream.writeOffset) || frame.Kind != TransportHello.Response() || frame.Sequence != 17 {
		t.Fatalf("frame=%+v consumed=%d err=%v", frame, consumed, err)
	}
	allocations := testing.AllocsPerRun(100, func() {
		stream.readOffset, stream.writeOffset = 0, 0
		if err := ServeTransportOnce(endpoint, handler, stream, request, response); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("serve allocations=%v", allocations)
	}
}

func TestServeTransportOnceRejectsPayloadBeforeReadingIt(t *testing.T) {
	var encoded [TransportHeaderBytes]byte
	requestLength, err := EncodeTransportFrame(encoded[:], TransportFrame{Kind: TransportHello})
	if err != nil {
		t.Fatal(err)
	}
	encoded[12] = 65
	stream := &testTransportStream{input: encoded[:requestLength], output: make([]byte, 64), chunk: 24}
	endpoint := &TransportEndpoint{MaximumPayload: 64}
	if err := ServeTransportOnce(endpoint, &testTransportHandler{}, stream,
		make([]byte, TransportHeaderBytes+64), make([]byte, 64)); err != ErrTransportCapacity {
		t.Fatalf("err=%v", err)
	}
	if stream.readOffset != int(TransportHeaderBytes) {
		t.Fatalf("read bytes=%d", stream.readOffset)
	}
}
