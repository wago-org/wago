package embedded32

import "testing"

type testTransportCancelHandler struct {
	cancels int
}

func (h *testTransportCancelHandler) Cancel() TransportCode {
	h.cancels++
	return TransportCodeOK
}

func TestTransportCancelObserverFindsFragmentedCancelAfterPayload(t *testing.T) {
	handler := &testTransportCancelHandler{}
	observer := TransportCancelObserver{Handler: handler, MaximumPayload: 64}
	storage := make([]byte, 128)
	payload := []byte{1, 2, 3, 4, 5}
	first, err := EncodeTransportFrame(storage, TransportFrame{Kind: TransportCall, Sequence: 1, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	second, err := EncodeTransportFrame(storage[first:], TransportFrame{Kind: TransportCancel, Sequence: 2})
	if err != nil {
		t.Fatal(err)
	}
	stream := storage[:first+second]
	for len(stream) != 0 {
		size := min(3, len(stream))
		observer.Observe(stream[:size])
		stream = stream[size:]
	}
	if handler.cancels != 1 {
		t.Fatalf("cancels=%d", handler.cancels)
	}
}

func TestTransportCancelObserverRejectsCorruptionAndResponses(t *testing.T) {
	handler := &testTransportCancelHandler{}
	observer := TransportCancelObserver{Handler: handler, MaximumPayload: 64}
	request := make([]byte, TransportHeaderBytes)
	if _, err := EncodeTransportFrame(request, TransportFrame{Kind: TransportCancel, Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	request[20] ^= 1
	observer.Observe(request)
	response := make([]byte, TransportHeaderBytes)
	if _, err := EncodeTransportFrame(response, TransportFrame{Kind: TransportCancel.Response(), Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	observer.Observe(response)
	if handler.cancels != 0 {
		t.Fatalf("cancels=%d", handler.cancels)
	}
}

func TestTransportCancelObserverIsAllocationFree(t *testing.T) {
	handler := &testTransportCancelHandler{}
	observer := TransportCancelObserver{Handler: handler, MaximumPayload: 64}
	request := make([]byte, TransportHeaderBytes)
	if _, err := EncodeTransportFrame(request, TransportFrame{Kind: TransportCancel, Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(100, func() {
		observer.Observe(request)
	})
	if allocations != 0 {
		t.Fatalf("observe allocations=%v", allocations)
	}
}
