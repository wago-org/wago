package embedded32

import (
	"encoding/binary"
	"errors"
)

const (
	TransportMagic        = uint32(0x4f474157) // "WAGO" in little-endian order.
	TransportVersion      = uint16(1)
	TransportHeaderBytes  = uint32(24)
	TransportResponseMask = uint16(0x8000)
)

type TransportKind uint16

const (
	TransportHello TransportKind = iota + 1
	TransportInstantiate
	TransportStart
	TransportCall
	TransportCancel
	TransportReset
	TransportUploadStatus
	TransportUploadBegin
	TransportUploadChunk
	TransportUploadCommit
)

func (k TransportKind) Base() TransportKind { return k &^ TransportKind(TransportResponseMask) }
func (k TransportKind) IsResponse() bool    { return uint16(k)&TransportResponseMask != 0 }
func (k TransportKind) Response() TransportKind {
	return k.Base() | TransportKind(TransportResponseMask)
}
func (k TransportKind) Valid() bool {
	base := k.Base()
	return base >= TransportHello && base <= TransportUploadCommit
}

type TransportCode uint32

const (
	TransportCodeOK          TransportCode = 0
	TransportCodeBadFrame    TransportCode = 0x80000001
	TransportCodeUnsupported TransportCode = 0x80000002
	TransportCodeCapacity    TransportCode = 0x80000003
	TransportCodeState       TransportCode = 0x80000004
	TransportCodeChecksum    TransportCode = 0x80000005
)

func TransportTrapCode(trap Trap) TransportCode { return TransportCode(trap) }
func (c TransportCode) Trap() (Trap, bool) {
	trap := Trap(c)
	if c == 0 || uint32(c)&0x80000000 != 0 || trap > TrapIndirectCallTypeMismatch {
		return TrapNone, false
	}
	return trap, true
}

type TransportFrame struct {
	Kind     TransportKind
	Sequence uint32
	Code     TransportCode
	Payload  []byte
}

var (
	ErrTransportFrame    = errors.New("embedded32: malformed transport frame")
	ErrTransportChecksum = errors.New("embedded32: transport checksum mismatch")
	ErrTransportCapacity = errors.New("embedded32: transport capacity exceeded")
)

func EncodeTransportFrame(dst []byte, frame TransportFrame) (uint32, error) {
	if !frame.Kind.Valid() || !frame.Kind.IsResponse() && frame.Code != TransportCodeOK || uint64(len(frame.Payload)) > uint64(^uint32(0))-uint64(TransportHeaderBytes) {
		return 0, ErrTransportFrame
	}
	total := TransportHeaderBytes + uint32(len(frame.Payload))
	if uint64(total) > uint64(len(dst)) {
		return 0, ErrTransportCapacity
	}
	out := dst[:total]
	binary.LittleEndian.PutUint32(out[0:4], TransportMagic)
	binary.LittleEndian.PutUint16(out[4:6], TransportVersion)
	binary.LittleEndian.PutUint16(out[6:8], uint16(frame.Kind))
	binary.LittleEndian.PutUint32(out[8:12], frame.Sequence)
	binary.LittleEndian.PutUint32(out[12:16], uint32(len(frame.Payload)))
	binary.LittleEndian.PutUint32(out[16:20], uint32(frame.Code))
	copy(out[TransportHeaderBytes:], frame.Payload)
	binary.LittleEndian.PutUint32(out[20:24], transportChecksum(out[:20], out[TransportHeaderBytes:]))
	return total, nil
}

func DecodeTransportFrame(src []byte, maximumPayload uint32) (TransportFrame, uint32, error) {
	if len(src) < int(TransportHeaderBytes) {
		return TransportFrame{}, 0, ErrTransportFrame
	}
	if binary.LittleEndian.Uint32(src[0:4]) != TransportMagic || binary.LittleEndian.Uint16(src[4:6]) != TransportVersion {
		return TransportFrame{}, 0, ErrTransportFrame
	}
	kind := TransportKind(binary.LittleEndian.Uint16(src[6:8]))
	code := TransportCode(binary.LittleEndian.Uint32(src[16:20]))
	if !kind.Valid() || !kind.IsResponse() && code != TransportCodeOK {
		return TransportFrame{}, 0, ErrTransportFrame
	}
	length := binary.LittleEndian.Uint32(src[12:16])
	if length > maximumPayload {
		return TransportFrame{}, 0, ErrTransportCapacity
	}
	if uint64(length)+uint64(TransportHeaderBytes) > uint64(len(src)) {
		return TransportFrame{}, 0, ErrTransportFrame
	}
	total := TransportHeaderBytes + length
	payload := src[TransportHeaderBytes:total]
	want := binary.LittleEndian.Uint32(src[20:24])
	if transportChecksum(src[:20], payload) != want {
		return TransportFrame{}, 0, ErrTransportChecksum
	}
	return TransportFrame{
		Kind:     kind,
		Sequence: binary.LittleEndian.Uint32(src[8:12]),
		Code:     code,
		Payload:  payload,
	}, total, nil
}

func transportChecksum(parts ...[]byte) uint32 {
	crc := ^uint32(0)
	for _, part := range parts {
		for _, value := range part {
			crc ^= uint32(value)
			for bit := 0; bit < 8; bit++ {
				mask := uint32(0) - (crc & 1)
				crc = crc>>1 ^ (0xedb88320 & mask)
			}
		}
	}
	return ^crc
}

// TransportChecksum returns the bitwise IEEE CRC32 used by transport frames
// and uploaded firmware images.
func TransportChecksum(src []byte) uint32 { return transportChecksum(src) }

const (
	TransportUploadStatusBytes = uint32(24)
	TransportUploadBeginBytes  = uint32(8)
	TransportUploadChunkHeader = uint32(4)
)

const (
	TransportUploadEmpty = uint32(iota)
	TransportUploadReceiving
	TransportUploadCommitted
)

// TransportUploadStatusInfo describes the target live-image base, fixed
// artifact capacity, and currently staged or committed upload.
type TransportUploadStatusInfo struct {
	BaseAddress   uint32
	Capacity      uint32
	MaximumChunk  uint32
	ImageBytes    uint32
	ImageChecksum uint32
	State         uint32
}

type TransportUploadBeginRequest struct {
	ImageBytes    uint32
	ImageChecksum uint32
}

type TransportUploadChunkRequest struct {
	Offset uint32
	Bytes  []byte
}

func EncodeTransportUploadStatus(dst []byte, status TransportUploadStatusInfo) error {
	if len(dst) < int(TransportUploadStatusBytes) || status.BaseAddress == 0 || status.Capacity == 0 ||
		status.MaximumChunk == 0 || status.MaximumChunk > status.Capacity || status.ImageBytes > status.Capacity ||
		status.State > TransportUploadCommitted || status.State == TransportUploadEmpty && (status.ImageBytes != 0 || status.ImageChecksum != 0) ||
		status.State != TransportUploadEmpty && status.ImageBytes == 0 {
		return ErrTransportFrame
	}
	binary.LittleEndian.PutUint32(dst[0:4], status.BaseAddress)
	binary.LittleEndian.PutUint32(dst[4:8], status.Capacity)
	binary.LittleEndian.PutUint32(dst[8:12], status.MaximumChunk)
	binary.LittleEndian.PutUint32(dst[12:16], status.ImageBytes)
	binary.LittleEndian.PutUint32(dst[16:20], status.ImageChecksum)
	binary.LittleEndian.PutUint32(dst[20:24], status.State)
	return nil
}

func DecodeTransportUploadStatus(src []byte) (TransportUploadStatusInfo, error) {
	if len(src) != int(TransportUploadStatusBytes) {
		return TransportUploadStatusInfo{}, ErrTransportFrame
	}
	status := TransportUploadStatusInfo{
		BaseAddress:   binary.LittleEndian.Uint32(src[0:4]),
		Capacity:      binary.LittleEndian.Uint32(src[4:8]),
		MaximumChunk:  binary.LittleEndian.Uint32(src[8:12]),
		ImageBytes:    binary.LittleEndian.Uint32(src[12:16]),
		ImageChecksum: binary.LittleEndian.Uint32(src[16:20]),
		State:         binary.LittleEndian.Uint32(src[20:24]),
	}
	if status.BaseAddress == 0 || status.Capacity == 0 || status.MaximumChunk == 0 ||
		status.MaximumChunk > status.Capacity || status.ImageBytes > status.Capacity ||
		status.State > TransportUploadCommitted || status.State == TransportUploadEmpty && (status.ImageBytes != 0 || status.ImageChecksum != 0) ||
		status.State != TransportUploadEmpty && status.ImageBytes == 0 {
		return TransportUploadStatusInfo{}, ErrTransportFrame
	}
	return status, nil
}

func EncodeTransportUploadBegin(dst []byte, request TransportUploadBeginRequest) error {
	if len(dst) < int(TransportUploadBeginBytes) || request.ImageBytes == 0 {
		return ErrTransportFrame
	}
	binary.LittleEndian.PutUint32(dst[0:4], request.ImageBytes)
	binary.LittleEndian.PutUint32(dst[4:8], request.ImageChecksum)
	return nil
}

func DecodeTransportUploadBegin(src []byte) (TransportUploadBeginRequest, error) {
	if len(src) != int(TransportUploadBeginBytes) {
		return TransportUploadBeginRequest{}, ErrTransportFrame
	}
	request := TransportUploadBeginRequest{
		ImageBytes:    binary.LittleEndian.Uint32(src[0:4]),
		ImageChecksum: binary.LittleEndian.Uint32(src[4:8]),
	}
	if request.ImageBytes == 0 {
		return TransportUploadBeginRequest{}, ErrTransportFrame
	}
	return request, nil
}

func EncodeTransportUploadChunk(dst []byte, request TransportUploadChunkRequest) (uint32, error) {
	if len(request.Bytes) == 0 || uint64(len(request.Bytes)) > uint64(^uint32(0))-uint64(TransportUploadChunkHeader) {
		return 0, ErrTransportFrame
	}
	size := TransportUploadChunkHeader + uint32(len(request.Bytes))
	if uint64(size) > uint64(len(dst)) {
		return 0, ErrTransportCapacity
	}
	binary.LittleEndian.PutUint32(dst[0:4], request.Offset)
	copy(dst[TransportUploadChunkHeader:size], request.Bytes)
	return size, nil
}

func DecodeTransportUploadChunk(src []byte) (TransportUploadChunkRequest, error) {
	if len(src) <= int(TransportUploadChunkHeader) {
		return TransportUploadChunkRequest{}, ErrTransportFrame
	}
	return TransportUploadChunkRequest{
		Offset: binary.LittleEndian.Uint32(src[0:4]),
		Bytes:  src[TransportUploadChunkHeader:],
	}, nil
}

const TransportCallHeaderBytes = uint32(12)

type TransportCallRequest struct {
	ExportIndex    uint32
	ParameterSlots []uint32
	ResultSlots    uint32
}

func TransportCallRequestBytes(parameterSlots uint32) (uint32, bool) {
	if parameterSlots > (^uint32(0)-TransportCallHeaderBytes)/4 {
		return 0, false
	}
	return TransportCallHeaderBytes + parameterSlots*4, true
}

func EncodeTransportCallRequest(dst []byte, call TransportCallRequest) (uint32, error) {
	size, ok := TransportCallRequestBytes(uint32(len(call.ParameterSlots)))
	if !ok {
		return 0, ErrTransportFrame
	}
	if uint64(size) > uint64(len(dst)) {
		return 0, ErrTransportCapacity
	}
	binary.LittleEndian.PutUint32(dst[0:4], call.ExportIndex)
	binary.LittleEndian.PutUint32(dst[4:8], uint32(len(call.ParameterSlots)))
	binary.LittleEndian.PutUint32(dst[8:12], call.ResultSlots)
	for i, slot := range call.ParameterSlots {
		binary.LittleEndian.PutUint32(dst[TransportCallHeaderBytes+uint32(i*4):], slot)
	}
	return size, nil
}

func DecodeTransportCallRequest(payload []byte, parameterSlots []uint32, maximumResultSlots uint32) (TransportCallRequest, error) {
	if len(payload) < int(TransportCallHeaderBytes) {
		return TransportCallRequest{}, ErrTransportFrame
	}
	parameterCount := binary.LittleEndian.Uint32(payload[4:8])
	resultCount := binary.LittleEndian.Uint32(payload[8:12])
	size, ok := TransportCallRequestBytes(parameterCount)
	if !ok || uint64(size) != uint64(len(payload)) || uint64(parameterCount) > uint64(len(parameterSlots)) || resultCount > maximumResultSlots {
		return TransportCallRequest{}, ErrTransportFrame
	}
	slots := parameterSlots[:parameterCount]
	for i := range slots {
		slots[i] = binary.LittleEndian.Uint32(payload[TransportCallHeaderBytes+uint32(i*4):])
	}
	return TransportCallRequest{
		ExportIndex:    binary.LittleEndian.Uint32(payload[0:4]),
		ParameterSlots: slots,
		ResultSlots:    resultCount,
	}, nil
}

func EncodeTransportSlots(dst []byte, slots []uint32) (uint32, error) {
	if uint64(len(slots))*4 > uint64(^uint32(0)) {
		return 0, ErrTransportFrame
	}
	size := uint32(len(slots)) * 4
	if uint64(size) > uint64(len(dst)) {
		return 0, ErrTransportCapacity
	}
	for i, slot := range slots {
		binary.LittleEndian.PutUint32(dst[uint32(i*4):], slot)
	}
	return size, nil
}

func DecodeTransportSlots(payload []byte, slots []uint32, expectedSlots uint32) ([]uint32, error) {
	if uint64(expectedSlots) > uint64(len(slots)) || uint64(expectedSlots)*4 != uint64(len(payload)) {
		return nil, ErrTransportFrame
	}
	out := slots[:expectedSlots]
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(payload[uint32(i*4):])
	}
	return out, nil
}

const TransportHelloBytes = uint32(16)

const (
	TransportTargetArm32   = uint32(1)
	TransportTargetRISCV32 = uint32(2)
)

type TransportHelloInfo struct {
	Target          uint32
	ContextABIBytes uint32
	CallABIBytes    uint32
	MaximumPayload  uint32
}

func EncodeTransportHello(dst []byte, hello TransportHelloInfo) error {
	if hello.ContextABIBytes == 0 || hello.CallABIBytes == 0 {
		return ErrTransportFrame
	}
	if len(dst) < int(TransportHelloBytes) {
		return ErrTransportCapacity
	}
	binary.LittleEndian.PutUint32(dst[0:4], hello.Target)
	binary.LittleEndian.PutUint32(dst[4:8], hello.ContextABIBytes)
	binary.LittleEndian.PutUint32(dst[8:12], hello.CallABIBytes)
	binary.LittleEndian.PutUint32(dst[12:16], hello.MaximumPayload)
	return nil
}

func DecodeTransportHello(payload []byte) (TransportHelloInfo, error) {
	if len(payload) != int(TransportHelloBytes) {
		return TransportHelloInfo{}, ErrTransportFrame
	}
	hello := TransportHelloInfo{
		Target:          binary.LittleEndian.Uint32(payload[0:4]),
		ContextABIBytes: binary.LittleEndian.Uint32(payload[4:8]),
		CallABIBytes:    binary.LittleEndian.Uint32(payload[8:12]),
		MaximumPayload:  binary.LittleEndian.Uint32(payload[12:16]),
	}
	if hello.ContextABIBytes == 0 || hello.CallABIBytes == 0 {
		return TransportHelloInfo{}, ErrTransportFrame
	}
	return hello, nil
}
