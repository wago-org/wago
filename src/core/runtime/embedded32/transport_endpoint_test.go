package embedded32

import (
	"errors"
	"slices"
	"testing"
)

type testTransportHandler struct {
	instantiates int
	starts       int
	calls        int
	cancels      int
	resets       int
	callCode     TransportCode
	upload       *FirmwareUpload
}

func (h *testTransportHandler) Hello() TransportHelloInfo {
	return TransportHelloInfo{Target: TransportTargetRISCV32, ContextABIBytes: ContextABISize, CallABIBytes: CallABIBytes, MaximumPayload: 4096}
}
func (h *testTransportHandler) Instantiate() TransportCode { h.instantiates++; return TransportCodeOK }
func (h *testTransportHandler) Start() TransportCode       { h.starts++; return TransportCodeOK }
func (h *testTransportHandler) Call(exportIndex uint32, parameters, results []uint32) TransportCode {
	h.calls++
	if h.callCode != TransportCodeOK {
		return h.callCode
	}
	for i := range results {
		results[i] = exportIndex
		if i < len(parameters) {
			results[i] += parameters[i]
		}
	}
	return TransportCodeOK
}
func (h *testTransportHandler) Cancel() TransportCode { h.cancels++; return TransportCodeOK }
func (h *testTransportHandler) Reset() TransportCode  { h.resets++; return TransportCodeOK }
func (h *testTransportHandler) UploadStatus() TransportUploadStatusInfo {
	return h.upload.UploadStatus()
}
func (h *testTransportHandler) UploadBegin(request TransportUploadBeginRequest) TransportCode {
	return h.upload.UploadBegin(request)
}
func (h *testTransportHandler) UploadChunk(request TransportUploadChunkRequest) TransportCode {
	return h.upload.UploadChunk(request)
}
func (h *testTransportHandler) UploadCommit() TransportCode { return h.upload.UploadCommit() }

func encodeTransportTestRequest(t *testing.T, kind TransportKind, sequence uint32, payload []byte, dst []byte) []byte {
	t.Helper()
	n, err := EncodeTransportFrame(dst, TransportFrame{Kind: kind, Sequence: sequence, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	return dst[:n]
}

func TestTransportEndpointDispatchesHelloAndCalls(t *testing.T) {
	endpoint := TransportEndpoint{
		ParameterSlots: make([]uint32, 4),
		ResultSlots:    make([]uint32, 4),
		PayloadScratch: make([]byte, 64),
		MaximumPayload: 64,
	}
	handler := &testTransportHandler{}
	requestStorage := make([]byte, 128)
	responseStorage := make([]byte, 128)
	request := encodeTransportTestRequest(t, TransportHello, 7, nil, requestStorage)
	n, err := endpoint.Dispatch(request, responseStorage, handler)
	if err != nil {
		t.Fatal(err)
	}
	response, consumed, err := DecodeTransportFrame(responseStorage[:n], 64)
	if err != nil || consumed != n || response.Kind != TransportHello.Response() || response.Sequence != 7 || response.Code != TransportCodeOK {
		t.Fatalf("hello response=%+v consumed=%d err=%v", response, consumed, err)
	}
	hello, err := DecodeTransportHello(response.Payload)
	if err != nil || hello.Target != TransportTargetRISCV32 || hello.MaximumPayload != 64 || hello.ContextABIBytes != ContextABISize || hello.CallABIBytes != CallABIBytes {
		t.Fatalf("hello=%+v err=%v", hello, err)
	}

	callPayload := make([]byte, 64)
	payloadBytes, err := EncodeTransportCallRequest(callPayload, TransportCallRequest{ExportIndex: 40, ParameterSlots: []uint32{1, 2}, ResultSlots: 3})
	if err != nil {
		t.Fatal(err)
	}
	request = encodeTransportTestRequest(t, TransportCall, 8, callPayload[:payloadBytes], requestStorage)
	n, err = endpoint.Dispatch(request, responseStorage, handler)
	if err != nil {
		t.Fatal(err)
	}
	response, _, err = DecodeTransportFrame(responseStorage[:n], 64)
	if err != nil || response.Kind != TransportCall.Response() || response.Sequence != 8 || response.Code != TransportCodeOK {
		t.Fatalf("call response=%+v err=%v", response, err)
	}
	resultStorage := make([]uint32, 3)
	results, err := DecodeTransportSlots(response.Payload, resultStorage, 3)
	if err != nil || !slices.Equal(results, []uint32{41, 42, 40}) || handler.calls != 1 {
		t.Fatalf("results=%v calls=%d err=%v", results, handler.calls, err)
	}
}

func TestTransportEndpointSuppressesResultsOnTrap(t *testing.T) {
	endpoint := TransportEndpoint{ParameterSlots: make([]uint32, 1), ResultSlots: make([]uint32, 1), PayloadScratch: make([]byte, 16), MaximumPayload: 32}
	handler := &testTransportHandler{callCode: TransportTrapCode(TrapUnreachable)}
	callPayload := make([]byte, 16)
	payloadBytes, err := EncodeTransportCallRequest(callPayload, TransportCallRequest{ResultSlots: 1})
	if err != nil {
		t.Fatal(err)
	}
	requestStorage, responseStorage := make([]byte, 64), make([]byte, 64)
	request := encodeTransportTestRequest(t, TransportCall, 9, callPayload[:payloadBytes], requestStorage)
	n, err := endpoint.Dispatch(request, responseStorage, handler)
	if err != nil {
		t.Fatal(err)
	}
	response, _, err := DecodeTransportFrame(responseStorage[:n], 32)
	if err != nil || response.Code != TransportTrapCode(TrapUnreachable) || len(response.Payload) != 0 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestTransportEndpointRejectsMalformedCommandsBeforeDispatch(t *testing.T) {
	endpoint := TransportEndpoint{ParameterSlots: make([]uint32, 1), ResultSlots: make([]uint32, 1), PayloadScratch: make([]byte, 16), MaximumPayload: 32}
	handler := &testTransportHandler{}
	requestStorage, responseStorage := make([]byte, 64), make([]byte, 64)
	request := encodeTransportTestRequest(t, TransportReset, 1, []byte{1}, requestStorage)
	if _, err := endpoint.Dispatch(request, responseStorage, handler); !errors.Is(err, ErrTransportFrame) {
		t.Fatalf("payload error=%v", err)
	}
	if handler.resets != 0 {
		t.Fatal("malformed reset dispatched")
	}
	request = encodeTransportTestRequest(t, TransportCall.Response(), 2, nil, requestStorage)
	if _, err := endpoint.Dispatch(request, responseStorage, handler); !errors.Is(err, ErrTransportFrame) {
		t.Fatalf("response error=%v", err)
	}
	request = encodeTransportTestRequest(t, TransportReset, 3, nil, requestStorage)
	request = append(request, 0)
	if _, err := endpoint.Dispatch(request, responseStorage, handler); !errors.Is(err, ErrTransportFrame) {
		t.Fatalf("trailing data error=%v", err)
	}
}

func TestTransportEndpointPreflightsResponseBeforeCall(t *testing.T) {
	endpoint := TransportEndpoint{ParameterSlots: make([]uint32, 1), ResultSlots: make([]uint32, 2), PayloadScratch: make([]byte, 8), MaximumPayload: 32}
	handler := &testTransportHandler{}
	payload := make([]byte, 16)
	payloadBytes, err := EncodeTransportCallRequest(payload, TransportCallRequest{ResultSlots: 2})
	if err != nil {
		t.Fatal(err)
	}
	requestStorage := make([]byte, 64)
	request := encodeTransportTestRequest(t, TransportCall, 4, payload[:payloadBytes], requestStorage)
	if _, err := endpoint.Dispatch(request, make([]byte, TransportHeaderBytes+7), handler); !errors.Is(err, ErrTransportCapacity) {
		t.Fatalf("response capacity error=%v", err)
	}
	if handler.calls != 0 {
		t.Fatal("call dispatched before response preflight")
	}
}

func TestTransportEndpointDispatchIsAllocationFree(t *testing.T) {
	endpoint := TransportEndpoint{ParameterSlots: make([]uint32, 2), ResultSlots: make([]uint32, 2), PayloadScratch: make([]byte, 16), MaximumPayload: 32}
	handler := &testTransportHandler{}
	payload := make([]byte, 24)
	payloadBytes, err := EncodeTransportCallRequest(payload, TransportCallRequest{ExportIndex: 1, ParameterSlots: []uint32{2, 3}, ResultSlots: 2})
	if err != nil {
		t.Fatal(err)
	}
	requestStorage, responseStorage := make([]byte, 64), make([]byte, 64)
	request := encodeTransportTestRequest(t, TransportCall, 1, payload[:payloadBytes], requestStorage)
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := endpoint.Dispatch(request, responseStorage, handler); err != nil {
			panic(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("dispatch allocations=%f", allocs)
	}
}

func TestTransportEndpointDispatchesTransactionalUpload(t *testing.T) {
	storage := make([]byte, 32)
	handler := &testTransportHandler{upload: &FirmwareUpload{Storage: storage, BaseAddress: 0x20010000, MaximumChunk: 8}}
	endpoint := TransportEndpoint{PayloadScratch: make([]byte, 32), MaximumPayload: 32}
	requestStorage, responseStorage := make([]byte, 64), make([]byte, 64)
	dispatch := func(kind TransportKind, sequence uint32, payload []byte) TransportFrame {
		t.Helper()
		request := encodeTransportTestRequest(t, kind, sequence, payload, requestStorage)
		n, err := endpoint.Dispatch(request, responseStorage, handler)
		if err != nil {
			t.Fatal(err)
		}
		frame, _, err := DecodeTransportFrame(responseStorage[:n], 32)
		if err != nil {
			t.Fatal(err)
		}
		return frame
	}
	statusFrame := dispatch(TransportUploadStatus, 1, nil)
	status, err := DecodeTransportUploadStatus(statusFrame.Payload)
	if err != nil || status.Capacity != 32 || status.MaximumChunk != 8 || status.State != TransportUploadEmpty {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	image := []byte("pico upload")
	payload := make([]byte, 32)
	if err := EncodeTransportUploadBegin(payload, TransportUploadBeginRequest{ImageBytes: uint32(len(image)), ImageChecksum: TransportChecksum(image)}); err != nil {
		t.Fatal(err)
	}
	if frame := dispatch(TransportUploadBegin, 2, payload[:TransportUploadBeginBytes]); frame.Code != TransportCodeOK {
		t.Fatalf("begin code=%#x", frame.Code)
	}
	for offset := 0; offset < len(image); {
		end := min(offset+8, len(image))
		n, err := EncodeTransportUploadChunk(payload, TransportUploadChunkRequest{Offset: uint32(offset), Bytes: image[offset:end]})
		if err != nil {
			t.Fatal(err)
		}
		if frame := dispatch(TransportUploadChunk, uint32(3+offset), payload[:n]); frame.Code != TransportCodeOK {
			t.Fatalf("chunk %d code=%#x", offset, frame.Code)
		}
		offset = end
	}
	if frame := dispatch(TransportUploadCommit, 99, nil); frame.Code != TransportCodeOK {
		t.Fatalf("commit code=%#x", frame.Code)
	}
	statusFrame = dispatch(TransportUploadStatus, 100, nil)
	status, err = DecodeTransportUploadStatus(statusFrame.Payload)
	if err != nil || status.State != TransportUploadCommitted || status.ImageBytes != uint32(len(image)) {
		t.Fatalf("committed status=%+v err=%v", status, err)
	}
}
