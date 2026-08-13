package sourcearchive

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	zipDirectoryHeaderSignature = 0x02014b50
	zipDirectoryHeaderBytes     = 46
	zipDirectoryEndSignature    = 0x06054b50
	zipDirectoryEndBytes        = 22
	zipMaximumCommentBytes      = 1<<16 - 1
)

type zipDirectory struct {
	offset  int64
	size    uint64
	records uint64
}

// inspectZipDirectory bounds central-directory parsing before archive/zip
// constructs one zip.File and its variable metadata for every entry.
func inspectZipDirectory(ctx context.Context, archive string, limits extractionLimits) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	directory, err := locateZipDirectory(file, info.Size(), limits)
	if err != nil {
		return err
	}
	section := io.NewSectionReader(file, directory.offset, int64(directory.size))
	reader := bufio.NewReaderSize(section, 32<<10)
	remaining := directory.size
	var records uint64
	var header [zipDirectoryHeaderBytes]byte
	for remaining != 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		if records >= uint64(limits.entries) {
			return fmt.Errorf("source archive contains more than %d entries", limits.entries)
		}
		if remaining < zipDirectoryHeaderBytes {
			return errors.New("source archive has a malformed central directory")
		}
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			return fmt.Errorf("read source archive central directory: %w", err)
		}
		if binary.LittleEndian.Uint32(header[:4]) != zipDirectoryHeaderSignature {
			return errors.New("source archive has a malformed central directory header")
		}
		variable := uint64(binary.LittleEndian.Uint16(header[28:30])) +
			uint64(binary.LittleEndian.Uint16(header[30:32])) +
			uint64(binary.LittleEndian.Uint16(header[32:34]))
		recordBytes := uint64(zipDirectoryHeaderBytes) + variable
		if recordBytes > remaining {
			return errors.New("source archive has truncated central directory metadata")
		}
		if _, err := io.CopyN(io.Discard, reader, int64(variable)); err != nil {
			return fmt.Errorf("read source archive central directory metadata: %w", err)
		}
		remaining -= recordBytes
		records++
	}
	if records != directory.records {
		return fmt.Errorf("source archive central directory declares %d entries but contains %d", directory.records, records)
	}
	return nil
}

func locateZipDirectory(file *os.File, size int64, limits extractionLimits) (zipDirectory, error) {
	if size < zipDirectoryEndBytes {
		return zipDirectory{}, errors.New("source archive has no central directory")
	}
	tailBytes := int64(zipDirectoryEndBytes + zipMaximumCommentBytes)
	if tailBytes > size {
		tailBytes = size
	}
	tail := make([]byte, int(tailBytes))
	if _, err := file.ReadAt(tail, size-tailBytes); err != nil {
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
