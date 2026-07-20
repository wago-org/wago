package embedded32

import (
	"slices"
	"testing"
)

func TestFirmwareUploadCommitsOnlyCompleteChecksummedImage(t *testing.T) {
	storage := make([]byte, 64)
	upload := FirmwareUpload{Storage: storage, BaseAddress: 0x20010000, MaximumChunk: 8}
	image := []byte("transactional pico image")
	begin := TransportUploadBeginRequest{ImageBytes: uint32(len(image)), ImageChecksum: TransportChecksum(image)}
	if code := upload.UploadBegin(begin); code != TransportCodeOK {
		t.Fatalf("begin code=%#x", code)
	}
	if _, ok := upload.CommittedImage(); ok {
		t.Fatal("partial image published")
	}
	if code := upload.UploadChunk(TransportUploadChunkRequest{Offset: 1, Bytes: image[:4]}); code != TransportCodeState {
		t.Fatalf("out-of-order code=%#x", code)
	}
	for offset := 0; offset < len(image); {
		end := min(offset+8, len(image))
		if code := upload.UploadChunk(TransportUploadChunkRequest{Offset: uint32(offset), Bytes: image[offset:end]}); code != TransportCodeOK {
			t.Fatalf("chunk %d code=%#x", offset, code)
		}
		offset = end
	}
	if code := upload.UploadCommit(); code != TransportCodeOK {
		t.Fatalf("commit code=%#x", code)
	}
	committed, ok := upload.CommittedImage()
	if !ok || !slices.Equal(committed, image) {
		t.Fatalf("committed=%q ok=%v", committed, ok)
	}
	status := upload.UploadStatus()
	if status.State != TransportUploadCommitted || status.ImageBytes != uint32(len(image)) || status.ImageChecksum != begin.ImageChecksum {
		t.Fatalf("status=%+v", status)
	}
}

func TestFirmwareUploadRejectsCapacityChunkAndChecksumFailures(t *testing.T) {
	upload := FirmwareUpload{Storage: make([]byte, 16), BaseAddress: 0x20010000, MaximumChunk: 4}
	if code := upload.UploadBegin(TransportUploadBeginRequest{ImageBytes: 17}); code != TransportCodeCapacity {
		t.Fatalf("capacity code=%#x", code)
	}
	if code := upload.UploadBegin(TransportUploadBeginRequest{ImageBytes: 5, ImageChecksum: 1}); code != TransportCodeOK {
		t.Fatalf("begin code=%#x", code)
	}
	if code := upload.UploadChunk(TransportUploadChunkRequest{Bytes: make([]byte, 5)}); code != TransportCodeState {
		t.Fatalf("large chunk code=%#x", code)
	}
	if code := upload.UploadChunk(TransportUploadChunkRequest{Bytes: []byte{1, 2, 3, 4}}); code != TransportCodeOK {
		t.Fatalf("first chunk code=%#x", code)
	}
	if code := upload.UploadCommit(); code != TransportCodeState {
		t.Fatalf("partial commit code=%#x", code)
	}
	if code := upload.UploadChunk(TransportUploadChunkRequest{Offset: 4, Bytes: []byte{5}}); code != TransportCodeOK {
		t.Fatalf("final chunk code=%#x", code)
	}
	if code := upload.UploadCommit(); code != TransportCodeChecksum {
		t.Fatalf("checksum code=%#x", code)
	}
	if _, ok := upload.CommittedImage(); ok {
		t.Fatal("checksum-invalid image published")
	}
}

func TestFirmwareUploadHotPathDoesNotAllocate(t *testing.T) {
	image := []byte{1, 2, 3, 4}
	upload := FirmwareUpload{Storage: make([]byte, 16), BaseAddress: 0x20010000, MaximumChunk: 4}
	begin := TransportUploadBeginRequest{ImageBytes: 4, ImageChecksum: TransportChecksum(image)}
	allocs := testing.AllocsPerRun(100, func() {
		if upload.UploadBegin(begin) != TransportCodeOK ||
			upload.UploadChunk(TransportUploadChunkRequest{Bytes: image}) != TransportCodeOK ||
			upload.UploadCommit() != TransportCodeOK {
			panic("upload failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("upload allocations=%f", allocs)
	}
}

func TestFirmwareUploadDiscardInvalidatesCommit(t *testing.T) {
	image := []byte{1, 2, 3, 4}
	upload := FirmwareUpload{Storage: make([]byte, 8), BaseAddress: 0x20010000, MaximumChunk: 8}
	if upload.UploadBegin(TransportUploadBeginRequest{ImageBytes: 4, ImageChecksum: TransportChecksum(image)}) != TransportCodeOK ||
		upload.UploadChunk(TransportUploadChunkRequest{Bytes: image}) != TransportCodeOK || upload.UploadCommit() != TransportCodeOK {
		t.Fatal("commit failed")
	}
	upload.Discard()
	if _, ok := upload.CommittedImage(); ok || upload.UploadStatus().State != TransportUploadEmpty {
		t.Fatal("discard retained committed upload")
	}
}
