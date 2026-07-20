package embedded32

// FirmwareUpload owns a caller-provided fixed staging arena. Bytes become
// committed only after a complete sequential upload matches its declared
// checksum. Beginning a new upload immediately invalidates the prior commit.
type FirmwareUpload struct {
	Storage      []byte
	BaseAddress  uint32
	MaximumChunk uint32

	imageBytes    uint32
	expectedCRC   uint32
	receivedBytes uint32
	state         uint32
}

func (u *FirmwareUpload) UploadStatus() TransportUploadStatusInfo {
	if !u.valid() {
		return TransportUploadStatusInfo{}
	}
	return TransportUploadStatusInfo{
		BaseAddress:   u.BaseAddress,
		Capacity:      uint32(len(u.Storage)),
		MaximumChunk:  u.MaximumChunk,
		ImageBytes:    u.imageBytes,
		ImageChecksum: u.expectedCRC,
		State:         u.state,
	}
}

func (u *FirmwareUpload) UploadBegin(request TransportUploadBeginRequest) TransportCode {
	if !u.valid() {
		return TransportCodeState
	}
	u.imageBytes = 0
	u.expectedCRC = 0
	u.receivedBytes = 0
	u.state = TransportUploadEmpty
	if request.ImageBytes == 0 || uint64(request.ImageBytes) > uint64(len(u.Storage)) {
		return TransportCodeCapacity
	}
	u.imageBytes = request.ImageBytes
	u.expectedCRC = request.ImageChecksum
	u.state = TransportUploadReceiving
	return TransportCodeOK
}

func (u *FirmwareUpload) UploadChunk(request TransportUploadChunkRequest) TransportCode {
	if !u.valid() || u.state != TransportUploadReceiving || len(request.Bytes) == 0 ||
		uint64(len(request.Bytes)) > uint64(u.MaximumChunk) || request.Offset != u.receivedBytes ||
		uint64(request.Offset)+uint64(len(request.Bytes)) > uint64(u.imageBytes) {
		return TransportCodeState
	}
	copy(u.Storage[request.Offset:], request.Bytes)
	u.receivedBytes += uint32(len(request.Bytes))
	return TransportCodeOK
}

func (u *FirmwareUpload) UploadCommit() TransportCode {
	if !u.valid() || u.state != TransportUploadReceiving || u.receivedBytes != u.imageBytes {
		return TransportCodeState
	}
	if TransportChecksum(u.Storage[:u.imageBytes]) != u.expectedCRC {
		return TransportCodeChecksum
	}
	u.state = TransportUploadCommitted
	return TransportCodeOK
}

func (u *FirmwareUpload) CommittedImage() ([]byte, bool) {
	if !u.valid() || u.state != TransportUploadCommitted {
		return nil, false
	}
	return u.Storage[:u.imageBytes], true
}

// Discard invalidates the current upload without clearing the fixed staging
// arena. It is used when a checksummed envelope contains invalid metadata.
func (u *FirmwareUpload) Discard() {
	if u == nil {
		return
	}
	u.imageBytes = 0
	u.expectedCRC = 0
	u.receivedBytes = 0
	u.state = TransportUploadEmpty
}

func (u *FirmwareUpload) valid() bool {
	return u != nil && u.BaseAddress != 0 && u.BaseAddress&15 == 0 && len(u.Storage) != 0 &&
		uint64(len(u.Storage)) <= uint64(^uint32(0)) && u.MaximumChunk != 0 &&
		uint64(u.MaximumChunk) <= uint64(len(u.Storage))
}
