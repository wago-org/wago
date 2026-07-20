//go:build tinygo && pico2

package main

import (
	"time"
	"unsafe"

	"machine"

	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

const (
	maximumPayload      = uint32(256)
	uploadArenaBytes    = uint32(128 << 10)
	liveImageArenaBytes = uint32(128 << 10)
	maximumContexts     = 64
	maximumFunctions    = 128
	maximumSlots        = maximumPayload / 4
	nativeStackBytes    = uint32(32 << 10)
	boardWatchdogMillis = uint32(8000)
)

var (
	parameterSlots     [maximumSlots]uint32
	resultSlots        [maximumSlots]uint32
	payloadScratch     [maximumPayload]byte
	requestStorage     [embedded32.TransportHeaderBytes + maximumPayload]byte
	responseStorage    [embedded32.TransportHeaderBytes + maximumPayload]byte
	uploadStorage      [uploadArenaBytes + 15]byte
	liveImageStorage   [liveImageArenaBytes + 15]byte
	nativeStackStorage [nativeStackBytes + 15]byte
	board              boardHandler
	cancelObserver     embedded32.TransportCancelObserver
)

// usbStream adapts TinyGo's non-blocking USB CDC serial interface to the
// blocking io.ReadWriter contract used by ServeTransportOnce.
type usbStream struct{}

func (usbStream) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	for machine.Serial.Buffered() == 0 {
		machine.Watchdog.Update()
		time.Sleep(time.Millisecond)
	}
	read := 0
	for read < len(dst) && machine.Serial.Buffered() != 0 {
		value, err := machine.Serial.ReadByte()
		if err != nil {
			break
		}
		dst[read] = value
		read++
	}
	return read, nil
}

func (usbStream) Write(src []byte) (int, error) {
	return machine.Serial.Write(src)
}

func observeUSB(src []byte) {
	cancelObserver.Observe(src)
}

type boardHandler struct {
	upload      *embedded32.FirmwareUpload
	liveImage   []byte
	liveAddress uint32
	stackLimit  uint32
	stackTop    uint32
	contexts    [maximumContexts]uint32
	functions   [maximumFunctions]embedded32.FirmwareTransportFunction
	descriptor  embedded32.FirmwareImageDescriptor
	invoker     embedded32.FirmwareImageInvoker
	runner      embedded32.FirmwareTransportRunner
}

func (*boardHandler) Hello() embedded32.TransportHelloInfo {
	return embedded32.TransportHelloInfo{
		Target:          boardTransportTarget,
		ContextABIBytes: embedded32.ContextABISize,
		CallABIBytes:    embedded32.CallABIBytes,
		MaximumPayload:  maximumPayload,
	}
}

func (h *boardHandler) Instantiate() embedded32.TransportCode {
	return h.runner.Instantiate()
}
func (h *boardHandler) Start() embedded32.TransportCode { return h.runner.Start() }
func (h *boardHandler) Call(export uint32, parameters, results []uint32) embedded32.TransportCode {
	return h.runner.Call(export, parameters, results)
}
func (h *boardHandler) Cancel() embedded32.TransportCode { return h.runner.Cancel() }
func (h *boardHandler) Reset() embedded32.TransportCode  { return h.runner.Reset() }
func (h *boardHandler) UploadStatus() embedded32.TransportUploadStatusInfo {
	return h.upload.UploadStatus()
}
func (h *boardHandler) UploadBegin(request embedded32.TransportUploadBeginRequest) embedded32.TransportCode {
	h.runner = embedded32.FirmwareTransportRunner{}
	h.descriptor = embedded32.FirmwareImageDescriptor{}
	h.invoker = embedded32.FirmwareImageInvoker{}
	return h.upload.UploadBegin(request)
}
func (h *boardHandler) UploadChunk(request embedded32.TransportUploadChunkRequest) embedded32.TransportCode {
	return h.upload.UploadChunk(request)
}
func (h *boardHandler) UploadCommit() embedded32.TransportCode {
	if code := h.upload.UploadCommit(); code != embedded32.TransportCodeOK {
		return code
	}
	encoded, ok := h.upload.CommittedImage()
	if !ok {
		return embedded32.TransportCodeState
	}
	artifact, err := embedded32.DecodeFirmwareArtifact(encoded, h.contexts[:], h.functions[:])
	if err != nil || artifact.Target != boardTransportTarget || artifact.ImageAddress != h.liveAddress ||
		len(artifact.Image) > len(h.liveImage) {
		h.upload.Discard()
		return embedded32.TransportCodeState
	}
	h.descriptor = embedded32.FirmwareImageDescriptor{
		Target:            artifact.Target,
		MaximumPayload:    maximumPayload,
		ImageAddress:      artifact.ImageAddress,
		InitialImageBytes: artifact.Image,
		Image:             h.liveImage[:len(artifact.Image)],
		ContextAddress:    artifact.ContextAddress,
		StartAddress:      artifact.StartAddress,
		Functions:         artifact.Functions,
		Contexts:          artifact.Contexts,
		HelperEntries:     boardHelperEntries(),
		StackLimit:        h.stackLimit,
	}
	if !h.descriptor.Valid() {
		h.upload.Discard()
		return embedded32.TransportCodeState
	}
	h.invoker = embedded32.FirmwareImageInvoker{
		Descriptor: &h.descriptor,
		Native:     boardNative{stackTop: h.stackTop},
		Publish: func(uint32, []byte) {
			nativePublish()
		},
	}
	h.runner = embedded32.FirmwareTransportRunner{
		Target:         artifact.Target,
		MaximumPayload: maximumPayload,
		ContextAddress: artifact.ContextAddress,
		StartAddress:   artifact.StartAddress,
		Functions:      artifact.Functions,
		Invoker:        &h.invoker,
	}
	return embedded32.TransportCodeOK
}

func alignedArena(storage []byte, size uint32) ([]byte, uint32) {
	start := uintptr(unsafe.Pointer(&storage[0]))
	aligned := (start + 15) &^ uintptr(15)
	return unsafe.Slice((*byte)(unsafe.Pointer(aligned)), size), uint32(aligned)
}

func main() {
	machine.LED.Configure(machine.PinConfig{Mode: machine.PinOutput})
	for count := 0; count < 50; count++ {
		machine.LED.Set(!machine.LED.Get())
		time.Sleep(100 * time.Millisecond)
	}
	machine.LED.Low()
	if err := machine.Watchdog.Configure(machine.WatchdogConfig{TimeoutMillis: boardWatchdogMillis}); err != nil {
		machine.LED.High()
		return
	}
	if err := machine.Watchdog.Start(); err != nil {
		machine.LED.High()
		return
	}

	endpoint := embedded32.TransportEndpoint{
		ParameterSlots: parameterSlots[:],
		ResultSlots:    resultSlots[:],
		PayloadScratch: payloadScratch[:],
		MaximumPayload: maximumPayload,
	}
	uploadArena, _ := alignedArena(uploadStorage[:], uploadArenaBytes)
	liveArena, liveAddress := alignedArena(liveImageStorage[:], liveImageArenaBytes)
	nativeStack, stackLimit := alignedArena(nativeStackStorage[:], nativeStackBytes)
	stackTop := uint32(uintptr(unsafe.Pointer(&nativeStack[0]))+uintptr(len(nativeStack))) &^ uint32(15)
	upload := embedded32.FirmwareUpload{
		Storage:      uploadArena,
		BaseAddress:  liveAddress,
		MaximumChunk: maximumPayload - embedded32.TransportUploadChunkHeader,
	}
	stream := usbStream{}
	board = boardHandler{upload: &upload, liveImage: liveArena, liveAddress: liveAddress, stackLimit: stackLimit, stackTop: stackTop}
	cancelObserver = embedded32.TransportCancelObserver{Handler: &board, MaximumPayload: maximumPayload}
	machine.SetUSBCDCRxHandler(observeUSB)
	for {
		machine.Watchdog.Update()
		if err := embedded32.ServeTransportOnce(
			&endpoint,
			&board,
			stream,
			requestStorage[:],
			responseStorage[:],
		); err != nil {
			machine.LED.High()
		}
	}
}
