package sourcearchive

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	zipFileHeaderSignature         = 0x04034b50
	zipFileHeaderBytes             = 30
	zipDirectoryHeaderSignature    = 0x02014b50
	zipDirectoryHeaderBytes        = 46
	zipDirectoryEndSignature       = 0x06054b50
	zipDirectoryEndBytes           = 22
	zipMaximumCommentBytes         = 1<<16 - 1
	zip64ExtraFieldTag             = 0x0001
	zipDataDescriptorFlag          = 1 << 3
	zipDataDescriptorSignature     = 0x08074b50
	zipDataDescriptorBytes         = 16
	zipDataDescriptorUnsignedBytes = 12
	zipVersion20                   = 20
	zipEncryptedFlag               = 1 << 0
	zipDeflateOptionFlags          = 1<<1 | 1<<2
	zipPatchedDataFlag             = 1 << 5
	zipStrongEncryptionFlag        = 1 << 6
	zipUTF8Flag                    = 1 << 11
)

type zipDirectory struct {
	offset  int64
	size    uint64
	records uint64
}

type zipDirectoryCursor struct {
	reader    *bufio.Reader
	remaining uint64
	header    [zipDirectoryHeaderBytes]byte
}

// inspectZipDirectory bounds central-directory parsing before archive/zip
// constructs one zip.File and its variable metadata for every entry.
func inspectZipDirectory(ctx context.Context, readerAt io.ReaderAt, size int64, limits extractionLimits) (zipDirectory, error) {
	directory, err := locateZipDirectory(readerAt, size, limits)
	if err != nil {
		return zipDirectory{}, err
	}
	section := io.NewSectionReader(readerAt, directory.offset, int64(directory.size))
	reader := bufio.NewReaderSize(section, 32<<10)
	remaining := directory.size
	var records uint64
	var header [zipDirectoryHeaderBytes]byte
	for remaining != 0 {
		if err := ctx.Err(); err != nil {
			return zipDirectory{}, err
		}
		if records >= uint64(limits.entries) {
			return zipDirectory{}, fmt.Errorf("source archive contains more than %d entries", limits.entries)
		}
		if remaining < zipDirectoryHeaderBytes {
			return zipDirectory{}, errors.New("source archive has a malformed central directory")
		}
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			return zipDirectory{}, fmt.Errorf("read source archive central directory: %w", err)
		}
		if binary.LittleEndian.Uint32(header[:4]) != zipDirectoryHeaderSignature {
			return zipDirectory{}, errors.New("source archive has a malformed central directory header")
		}
		nameBytes := uint64(binary.LittleEndian.Uint16(header[28:30]))
		extraBytes := uint64(binary.LittleEndian.Uint16(header[30:32]))
		commentBytes := uint64(binary.LittleEndian.Uint16(header[32:34]))
		variable := nameBytes + extraBytes + commentBytes
		recordBytes := uint64(zipDirectoryHeaderBytes) + variable
		if recordBytes > remaining {
			return zipDirectory{}, errors.New("source archive has truncated central directory metadata")
		}
		if binary.LittleEndian.Uint32(header[20:24]) == ^uint32(0) ||
			binary.LittleEndian.Uint32(header[24:28]) == ^uint32(0) ||
			binary.LittleEndian.Uint32(header[42:46]) == ^uint32(0) {
			return zipDirectory{}, errors.New("source archive uses unsupported ZIP64 entry metadata")
		}
		if binary.LittleEndian.Uint16(header[34:36]) != 0 {
			return zipDirectory{}, errors.New("source archive uses unsupported multi-disk entry metadata")
		}
		if err := discardZipMetadata(reader, nameBytes); err != nil {
			return zipDirectory{}, fmt.Errorf("read source archive central directory metadata: %w", err)
		}
		if err := validateZipExtraFields(reader, extraBytes, "central directory"); err != nil {
			return zipDirectory{}, err
		}
		if err := discardZipMetadata(reader, commentBytes); err != nil {
			return zipDirectory{}, fmt.Errorf("read source archive central directory metadata: %w", err)
		}
		remaining -= recordBytes
		records++
	}
	if records != directory.records {
		return zipDirectory{}, fmt.Errorf("source archive central directory declares %d entries but contains %d", directory.records, records)
	}
	return directory, nil
}

func newZipDirectoryCursor(readerAt io.ReaderAt, directory zipDirectory) *zipDirectoryCursor {
	section := io.NewSectionReader(readerAt, directory.offset, int64(directory.size))
	return &zipDirectoryCursor{reader: bufio.NewReaderSize(section, 32<<10), remaining: directory.size}
}

func (cursor *zipDirectoryCursor) next(file *zip.File, nameBuffer []byte) (int64, error) {
	if cursor.remaining < zipDirectoryHeaderBytes {
		return 0, errors.New("source archive central directory changed during preflight")
	}
	if _, err := io.ReadFull(cursor.reader, cursor.header[:]); err != nil {
		return 0, fmt.Errorf("read source archive central directory during preflight: %w", err)
	}
	if binary.LittleEndian.Uint32(cursor.header[0:4]) != zipDirectoryHeaderSignature {
		return 0, errors.New("source archive central directory changed during preflight")
	}
	nameBytes := uint64(binary.LittleEndian.Uint16(cursor.header[28:30]))
	extraBytes := uint64(binary.LittleEndian.Uint16(cursor.header[30:32]))
	commentBytes := uint64(binary.LittleEndian.Uint16(cursor.header[32:34]))
	recordBytes := uint64(zipDirectoryHeaderBytes) + nameBytes + extraBytes + commentBytes
	if recordBytes > cursor.remaining || nameBytes != uint64(len(file.Name)) || nameBytes > uint64(len(nameBuffer)) {
		return 0, errors.New("source archive central directory changed during preflight")
	}
	if binary.LittleEndian.Uint16(cursor.header[6:8]) != file.ReaderVersion ||
		binary.LittleEndian.Uint16(cursor.header[8:10]) != file.Flags ||
		binary.LittleEndian.Uint16(cursor.header[10:12]) != file.Method ||
		binary.LittleEndian.Uint32(cursor.header[16:20]) != file.CRC32 ||
		uint64(binary.LittleEndian.Uint32(cursor.header[20:24])) != file.CompressedSize64 ||
		uint64(binary.LittleEndian.Uint32(cursor.header[24:28])) != file.UncompressedSize64 {
		return 0, errors.New("source archive central directory changed during preflight")
	}
	name := nameBuffer[:nameBytes]
	if _, err := io.ReadFull(cursor.reader, name); err != nil {
		return 0, fmt.Errorf("read source archive central directory name: %w", err)
	}
	for index := range name {
		if name[index] != file.Name[index] {
			return 0, errors.New("source archive central directory changed during preflight")
		}
	}
	if err := validateZipExtraFields(cursor.reader, extraBytes, "central directory"); err != nil {
		return 0, err
	}
	if err := discardZipMetadata(cursor.reader, commentBytes); err != nil {
		return 0, fmt.Errorf("read source archive central directory comment: %w", err)
	}
	cursor.remaining -= recordBytes
	return int64(binary.LittleEndian.Uint32(cursor.header[42:46])), nil
}

func validateZipExtraFields(reader io.Reader, remaining uint64, location string) error {
	var header [4]byte
	for remaining != 0 {
		if remaining < uint64(len(header)) {
			return fmt.Errorf("source archive has malformed %s extra metadata", location)
		}
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			return fmt.Errorf("read source archive %s extra metadata: %w", location, err)
		}
		remaining -= uint64(len(header))
		tag := binary.LittleEndian.Uint16(header[0:2])
		fieldBytes := uint64(binary.LittleEndian.Uint16(header[2:4]))
		if fieldBytes > remaining {
			return fmt.Errorf("source archive has malformed %s extra metadata", location)
		}
		if tag == zip64ExtraFieldTag {
			return errors.New("source archive uses unsupported ZIP64 entry metadata")
		}
		if err := discardZipMetadata(reader, fieldBytes); err != nil {
			return fmt.Errorf("read source archive %s extra metadata: %w", location, err)
		}
		remaining -= fieldBytes
	}
	return nil
}

func discardZipMetadata(reader io.Reader, size uint64) error {
	if size == 0 {
		return nil
	}
	_, err := io.CopyN(io.Discard, reader, int64(size))
	return err
}

func locateZipDirectory(readerAt io.ReaderAt, size int64, limits extractionLimits) (zipDirectory, error) {
	if size < zipDirectoryEndBytes {
		return zipDirectory{}, errors.New("source archive has no central directory")
	}
	tailBytes := int64(zipDirectoryEndBytes + zipMaximumCommentBytes)
	if tailBytes > size {
		tailBytes = size
	}
	tail := make([]byte, int(tailBytes))
	if _, err := readerAt.ReadAt(tail, size-tailBytes); err != nil {
		return zipDirectory{}, err
	}
	end := -1
	for index := len(tail) - zipDirectoryEndBytes; index >= 0; index-- {
		if binary.LittleEndian.Uint32(tail[index:index+4]) != zipDirectoryEndSignature {
			continue
		}
		commentBytes := int(binary.LittleEndian.Uint16(tail[index+20 : index+22]))
		if index+zipDirectoryEndBytes+commentBytes == len(tail) {
			end = index
			break
		}
	}
	if end < 0 {
		return zipDirectory{}, errors.New("source archive has no valid central directory end")
	}
	recordsThisDisk := uint64(binary.LittleEndian.Uint16(tail[end+8 : end+10]))
	records := uint64(binary.LittleEndian.Uint16(tail[end+10 : end+12]))
	directoryBytes := uint64(binary.LittleEndian.Uint32(tail[end+12 : end+16]))
	directoryOffset := uint64(binary.LittleEndian.Uint32(tail[end+16 : end+20]))
	if records > uint64(limits.entries) {
		return zipDirectory{}, fmt.Errorf("source archive contains more than %d entries", limits.entries)
	}
	if directoryBytes > limits.metadataBytes {
		return zipDirectory{}, fmt.Errorf("source archive central directory exceeds %s limit", byteLimit(limits.metadataBytes))
	}
	if records == 0xffff || directoryBytes == 0xffffffff || directoryOffset == 0xffffffff {
		return zipDirectory{}, errors.New("source archive ZIP64 metadata exceeds the supported extraction policy")
	}
	if binary.LittleEndian.Uint16(tail[end+4:end+6]) != 0 ||
		binary.LittleEndian.Uint16(tail[end+6:end+8]) != 0 || recordsThisDisk != records {
		return zipDirectory{}, errors.New("source archive uses unsupported multi-disk metadata")
	}
	endOffset := size - tailBytes + int64(end)
	if directoryBytes > uint64(endOffset) || directoryOffset > uint64(endOffset)-directoryBytes {
		return zipDirectory{}, errors.New("source archive central directory lies outside the archive")
	}
	baseOffset := endOffset - int64(directoryBytes) - int64(directoryOffset)
	if baseOffset != 0 {
		return zipDirectory{}, errors.New("source archive contains unsupported data before the ZIP payload")
	}
	return zipDirectory{offset: int64(directoryOffset), size: directoryBytes, records: records}, nil
}
