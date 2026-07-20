package embedded32

// TransportHandler is implemented by the board-specific firmware harness. All
// methods are synchronous and allocation-free from the endpoint's perspective.
// Call receives exact serialized parameter and result-slot views. It returns
// TransportCodeOK, a TransportTrapCode, or a protocol status code.
type TransportHandler interface {
	Hello() TransportHelloInfo
	Instantiate() TransportCode
	Start() TransportCode
	Call(exportIndex uint32, parameters, results []uint32) TransportCode
	Cancel() TransportCode
	Reset() TransportCode
}

// TransportUploadHandler is an optional extension implemented by persistent
// firmware that accepts host-compiled target images into fixed SRAM.
type TransportUploadHandler interface {
	UploadStatus() TransportUploadStatusInfo
	UploadBegin(TransportUploadBeginRequest) TransportCode
	UploadChunk(TransportUploadChunkRequest) TransportCode
	UploadCommit() TransportCode
}

// TransportEndpoint owns no storage. The firmware supplies bounded slot and
// payload scratch buffers once and reuses them for every request.
type TransportEndpoint struct {
	ParameterSlots []uint32
	ResultSlots    []uint32
	PayloadScratch []byte
	MaximumPayload uint32
}

func (e *TransportEndpoint) Dispatch(request, response []byte, handler TransportHandler) (uint32, error) {
	if e == nil || handler == nil || uint64(e.MaximumPayload) > uint64(^uint32(0))-uint64(TransportHeaderBytes) {
		return 0, ErrTransportFrame
	}
	frame, consumed, err := DecodeTransportFrame(request, e.MaximumPayload)
	if err != nil {
		return 0, err
	}
	if uint64(consumed) != uint64(len(request)) || frame.Kind.IsResponse() || frame.Code != TransportCodeOK {
		return 0, ErrTransportFrame
	}
	if len(response) < int(TransportHeaderBytes) {
		return 0, ErrTransportCapacity
	}
	var code TransportCode
	var payload []byte
	empty := func() bool { return len(frame.Payload) == 0 }
	switch frame.Kind {
	case TransportHello:
		if !empty() {
			return 0, ErrTransportFrame
		}
		if len(e.PayloadScratch) < int(TransportHelloBytes) || len(response) < int(TransportHeaderBytes+TransportHelloBytes) {
			return 0, ErrTransportCapacity
		}
		hello := handler.Hello()
		if hello.MaximumPayload > e.MaximumPayload {
			hello.MaximumPayload = e.MaximumPayload
		}
		if err := EncodeTransportHello(e.PayloadScratch, hello); err != nil {
			return 0, err
		}
		payload = e.PayloadScratch[:TransportHelloBytes]
	case TransportInstantiate:
		if !empty() {
			return 0, ErrTransportFrame
		}
		code = handler.Instantiate()
	case TransportStart:
		if !empty() {
			return 0, ErrTransportFrame
		}
		code = handler.Start()
	case TransportCall:
		call, err := DecodeTransportCallRequest(frame.Payload, e.ParameterSlots, uint32(len(e.ResultSlots)))
		if err != nil {
			return 0, err
		}
		resultBytes := uint64(call.ResultSlots) * 4
		if resultBytes > uint64(len(e.PayloadScratch)) || uint64(TransportHeaderBytes)+resultBytes > uint64(len(response)) {
			return 0, ErrTransportCapacity
		}
		results := e.ResultSlots[:call.ResultSlots]
		clear(results)
		code = handler.Call(call.ExportIndex, call.ParameterSlots, results)
		if code == TransportCodeOK {
			size, err := EncodeTransportSlots(e.PayloadScratch, results)
			if err != nil {
				return 0, err
			}
			payload = e.PayloadScratch[:size]
		}
	case TransportCancel:
		if !empty() {
			return 0, ErrTransportFrame
		}
		code = handler.Cancel()
	case TransportReset:
		if !empty() {
			return 0, ErrTransportFrame
		}
		code = handler.Reset()
	case TransportUploadStatus:
		if !empty() {
			return 0, ErrTransportFrame
		}
		upload, ok := handler.(TransportUploadHandler)
		if !ok {
			code = TransportCodeUnsupported
			break
		}
		if len(e.PayloadScratch) < int(TransportUploadStatusBytes) || len(response) < int(TransportHeaderBytes+TransportUploadStatusBytes) {
			return 0, ErrTransportCapacity
		}
		if err := EncodeTransportUploadStatus(e.PayloadScratch, upload.UploadStatus()); err != nil {
			return 0, err
		}
		payload = e.PayloadScratch[:TransportUploadStatusBytes]
	case TransportUploadBegin:
		upload, ok := handler.(TransportUploadHandler)
		if !ok {
			code = TransportCodeUnsupported
			break
		}
		begin, err := DecodeTransportUploadBegin(frame.Payload)
		if err != nil {
			return 0, err
		}
		code = upload.UploadBegin(begin)
	case TransportUploadChunk:
		upload, ok := handler.(TransportUploadHandler)
		if !ok {
			code = TransportCodeUnsupported
			break
		}
		chunk, err := DecodeTransportUploadChunk(frame.Payload)
		if err != nil {
			return 0, err
		}
		code = upload.UploadChunk(chunk)
	case TransportUploadCommit:
		if !empty() {
			return 0, ErrTransportFrame
		}
		upload, ok := handler.(TransportUploadHandler)
		if !ok {
			code = TransportCodeUnsupported
			break
		}
		code = upload.UploadCommit()
	default:
		return 0, ErrTransportFrame
	}
	return EncodeTransportFrame(response, TransportFrame{
		Kind:     frame.Kind.Response(),
		Sequence: frame.Sequence,
		Code:     code,
		Payload:  payload,
	})
}
