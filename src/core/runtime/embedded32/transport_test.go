package embedded32

import (
	"errors"
	"slices"
	"testing"
)

func TestTransportChecksumUsesCRC32IEEE(t *testing.T) {
	if got := transportChecksum([]byte("123456789")); got != 0xcbf43926 {
		t.Fatalf("checksum=%#x", got)
	}
}

func TestTransportFrameRoundTrip(t *testing.T) {
	payload := []byte{1, 2, 3, 4, 5}
	dst := make([]byte, 64)
	n, err := EncodeTransportFrame(dst, TransportFrame{
		Kind:     TransportCall.Response(),
		Sequence: 17,
		Code:     TransportTrapCode(TrapMemoryOutOfBounds),
		Payload:  payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	dst[n] = 0xaa
	frame, consumed, err := DecodeTransportFrame(dst[:n+1], 32)
	if err != nil {
		t.Fatal(err)
	}
	if consumed != n || frame.Kind != TransportCall.Response() || !frame.Kind.IsResponse() || frame.Kind.Base() != TransportCall || frame.Sequence != 17 || frame.Code != TransportTrapCode(TrapMemoryOutOfBounds) || !slices.Equal(frame.Payload, payload) {
		t.Fatalf("frame=%+v consumed=%d want=%d", frame, consumed, n)
	}
	if trap, ok := frame.Code.Trap(); !ok || trap != TrapMemoryOutOfBounds {
		t.Fatalf("trap=%v ok=%v", trap, ok)
	}
	if _, ok := TransportCodeCapacity.Trap(); ok {
		t.Fatal("protocol status decoded as trap")
	}
}

func TestTransportFrameRejectsCorruptionAndPreflightsCapacity(t *testing.T) {
	dst := make([]byte, 27)
	for i := range dst {
		dst[i] = 0x5a
	}
	if _, err := EncodeTransportFrame(dst, TransportFrame{Kind: TransportCall, Payload: []byte{1, 2, 3, 4}}); !errors.Is(err, ErrTransportCapacity) {
		t.Fatalf("capacity error=%v", err)
	}
	for i, value := range dst {
		if value != 0x5a {
			t.Fatalf("destination mutated at %d", i)
		}
	}
	dst = make([]byte, 32)
	n, err := EncodeTransportFrame(dst, TransportFrame{Kind: TransportHello, Sequence: 1, Payload: []byte{9}})
	if err != nil {
		t.Fatal(err)
	}
	dst[n-1] ^= 1
	if _, _, err := DecodeTransportFrame(dst[:n], 8); !errors.Is(err, ErrTransportChecksum) {
		t.Fatalf("checksum error=%v", err)
	}
	if _, _, err := DecodeTransportFrame(dst[:TransportHeaderBytes], 8); !errors.Is(err, ErrTransportFrame) {
		t.Fatalf("truncated error=%v", err)
	}
	if _, _, err := DecodeTransportFrame(dst[:n], 0); !errors.Is(err, ErrTransportCapacity) {
		t.Fatalf("maximum payload error=%v", err)
	}
	if _, err := EncodeTransportFrame(make([]byte, 32), TransportFrame{Kind: TransportCall, Code: TransportCodeState}); !errors.Is(err, ErrTransportFrame) {
		t.Fatalf("request status error=%v", err)
	}
}

func TestTransportCallPayloadUsesCallerStorage(t *testing.T) {
	payload := make([]byte, 64)
	n, err := EncodeTransportCallRequest(payload, TransportCallRequest{
		ExportIndex:    3,
		ParameterSlots: []uint32{0x11223344, 0x55667788, 9},
		ResultSlots:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	parameterSlots := make([]uint32, 4)
	call, err := DecodeTransportCallRequest(payload[:n], parameterSlots, 4)
	if err != nil {
		t.Fatal(err)
	}
	if call.ExportIndex != 3 || call.ResultSlots != 2 || !slices.Equal(call.ParameterSlots, []uint32{0x11223344, 0x55667788, 9}) || &call.ParameterSlots[0] != &parameterSlots[0] {
		t.Fatalf("call=%+v", call)
	}
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := DecodeTransportCallRequest(payload[:n], parameterSlots, 4); err != nil {
			panic(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("decode allocations=%f", allocs)
	}
	if _, err := DecodeTransportCallRequest(payload[:n], parameterSlots[:2], 4); !errors.Is(err, ErrTransportFrame) {
		t.Fatalf("parameter capacity error=%v", err)
	}
	if _, err := DecodeTransportCallRequest(payload[:n], parameterSlots, 1); !errors.Is(err, ErrTransportFrame) {
		t.Fatalf("result capacity error=%v", err)
	}
}

func TestTransportResultSlotsAndHelloRoundTrip(t *testing.T) {
	payload := make([]byte, 32)
	n, err := EncodeTransportSlots(payload, []uint32{1, 0x88776655, 3})
	if err != nil {
		t.Fatal(err)
	}
	storage := make([]uint32, 4)
	slots, err := DecodeTransportSlots(payload[:n], storage, 3)
	if err != nil || !slices.Equal(slots, []uint32{1, 0x88776655, 3}) || &slots[0] != &storage[0] {
		t.Fatalf("slots=%#v err=%v", slots, err)
	}
	hello := TransportHelloInfo{
		Target:          2,
		ContextABIBytes: ContextABISize,
		CallABIBytes:    CallABIBytes,
		MaximumPayload:  1024,
	}
	if err := EncodeTransportHello(payload, hello); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeTransportHello(payload[:TransportHelloBytes])
	if err != nil || got != hello {
		t.Fatalf("hello=%+v err=%v", got, err)
	}
}

func TestTransportUploadPayloadRoundTrip(t *testing.T) {
	payload := make([]byte, 64)
	status := TransportUploadStatusInfo{
		BaseAddress:   0x20010000,
		Capacity:      256 << 10,
		MaximumChunk:  252,
		ImageBytes:    12345,
		ImageChecksum: 0x12345678,
		State:         TransportUploadCommitted,
	}
	if err := EncodeTransportUploadStatus(payload, status); err != nil {
		t.Fatal(err)
	}
	gotStatus, err := DecodeTransportUploadStatus(payload[:TransportUploadStatusBytes])
	if err != nil || gotStatus != status {
		t.Fatalf("status=%+v err=%v", gotStatus, err)
	}
	begin := TransportUploadBeginRequest{ImageBytes: 12345, ImageChecksum: 0x12345678}
	if err := EncodeTransportUploadBegin(payload, begin); err != nil {
		t.Fatal(err)
	}
	gotBegin, err := DecodeTransportUploadBegin(payload[:TransportUploadBeginBytes])
	if err != nil || gotBegin != begin {
		t.Fatalf("begin=%+v err=%v", gotBegin, err)
	}
	chunk := TransportUploadChunkRequest{Offset: 77, Bytes: []byte{1, 2, 3, 4}}
	n, err := EncodeTransportUploadChunk(payload, chunk)
	if err != nil {
		t.Fatal(err)
	}
	gotChunk, err := DecodeTransportUploadChunk(payload[:n])
	if err != nil || gotChunk.Offset != chunk.Offset || !slices.Equal(gotChunk.Bytes, chunk.Bytes) {
		t.Fatalf("chunk=%+v err=%v", gotChunk, err)
	}
	if _, err := DecodeTransportUploadStatus(payload[:TransportUploadStatusBytes-1]); !errors.Is(err, ErrTransportFrame) {
		t.Fatalf("short status error=%v", err)
	}
	if _, err := DecodeTransportUploadBegin(nil); !errors.Is(err, ErrTransportFrame) {
		t.Fatalf("empty begin error=%v", err)
	}
	if _, err := DecodeTransportUploadChunk(make([]byte, TransportUploadChunkHeader)); !errors.Is(err, ErrTransportFrame) {
		t.Fatalf("empty chunk error=%v", err)
	}
}
