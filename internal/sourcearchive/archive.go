// Package sourcearchive safely extracts source archives with one top-level directory.
package sourcearchive

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type extractionLimits struct {
	entries        int
	files          int
	directories    int
	metadataBytes  uint64
	pathBytes      int
	pathDepth      int
	componentBytes int
	fileBytes      uint64
	expandedBytes  uint64
}

type archiveEntry struct {
	file     *zip.File
	relative string
	dir      bool
}

type archiveReadCloser interface {
	io.ReaderAt
	Stat() (fs.FileInfo, error)
	Close() error
}

type extractionOperations struct {
	openArchive func(string) (archiveReadCloser, error)
	removeAll   func(string) error
}

type pathNodeKey struct {
	parent    int
	component string
}

type pathNode struct {
	id        int
	component string
	spelling  string
	directory bool
	explicit  bool
}

type validatedArchivePath struct {
	name     string
	root     string
	relative string
}

// Extract expands archive into target while removing the archive's single
// top-level directory. It rejects traversal, multiple roots, unsupported or
// colliding paths, and work beyond the package's bounded extraction policy.
// The target must not exist and is published atomically only after validation
// and extraction succeed.
func Extract(archive, target string) error {
	return ExtractContext(context.Background(), archive, target)
}

// ExtractContext is Extract with cancellation. Cancellation and every other
// failure remove the private staging tree without publishing a partial target.
func ExtractContext(ctx context.Context, archive, target string) error {
	return extractWithLimits(ctx, archive, target, productionExtractionLimits())
}

func productionExtractionLimits() extractionLimits {
	return extractionLimits{
		entries:        20_000,
		files:          16_000,
		directories:    4_000,
		metadataBytes:  16 << 20,
		pathBytes:      1_024,
		pathDepth:      64,
		componentBytes: 255,
		fileBytes:      128 << 20,
		expandedBytes:  512 << 20,
	}
}

func extractWithLimits(ctx context.Context, archive, target string, limits extractionLimits) error {
	return extractWithOperations(ctx, archive, target, limits, defaultExtractionOperations())
}

func defaultExtractionOperations() extractionOperations {
	return extractionOperations{
		openArchive: func(path string) (archiveReadCloser, error) { return os.Open(path) },
		removeAll:   os.RemoveAll,
	}
}

func extractWithOperations(ctx context.Context, archive, target string, limits extractionLimits, operations extractionOperations) (returnErr error) {
	if ctx == nil {
		return errors.New("source archive extraction requires a context")
	}
	if operations.openArchive == nil || operations.removeAll == nil {
		return errors.New("source archive extraction operations must be configured")
	}
	if err := validateLimits(limits); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	archiveFile, err := operations.openArchive(archive)
	if err != nil {
		return err
	}
	archiveOpen := true
	closeArchive := func() error {
		if !archiveOpen {
			return nil
		}
		archiveOpen = false
		if err := archiveFile.Close(); err != nil {
			return fmt.Errorf("close source archive: %w", err)
		}
		return nil
	}
	defer func() {
		if archiveOpen {
			returnErr = errors.Join(returnErr, closeArchive())
		}
	}()

	info, err := archiveFile.Stat()
	if err != nil {
		return fmt.Errorf("stat source archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("source archive must be a regular file")
	}
	directory, err := inspectZipDirectory(ctx, archiveFile, info.Size(), limits)
	if err != nil {
		return err
	}
	reader, err := zip.NewReader(archiveFile, info.Size())
	if err != nil {
		return err
	}

	entries, err := preflight(ctx, archiveFile, reader.File, directory, limits)
	if err != nil {
		return err
	}
	target = filepath.Clean(target)
	if target == "." || filepath.Base(target) == string(filepath.Separator) {
		return errors.New("source archive target must name a new directory")
	}
	if _, err := os.Lstat(target); err == nil {
		return errors.New("source archive target already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(target)+"-extract-*")
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			if cleanupErr := operations.removeAll(staging); cleanupErr != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("clean source archive staging: %w", cleanupErr))
			}
		}
	}()

	var actual uint64
	buffer := make([]byte, 32<<10)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		destination, err := containedDestination(staging, entry.relative)
		if err != nil {
			return err
		}
		if entry.dir {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		copied, err := extractFile(ctx, entry.file, destination, buffer)
		actual += copied
		if actual > limits.expandedBytes {
			return fmt.Errorf("source archive actual content exceeds %s limit", byteLimit(limits.expandedBytes))
		}
		if err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err = os.Lstat(filepath.Join(staging, "go.mod"))
	if err != nil {
		return fmt.Errorf("verify extracted root go.mod: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("source archive does not contain a regular go.mod file")
	}
	if err := closeArchive(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := publishDirectoryNoReplace(staging, target); err != nil {
		return fmt.Errorf("publish source archive: %w", err)
	}
	published = true
	return nil
}

func validateLimits(limits extractionLimits) error {
	if limits.entries <= 0 || limits.files <= 0 || limits.directories <= 0 || limits.metadataBytes == 0 ||
		limits.pathBytes <= 0 || limits.pathDepth <= 0 || limits.componentBytes <= 0 ||
		limits.fileBytes == 0 || limits.expandedBytes == 0 {
		return errors.New("source archive extraction limits must be positive")
	}
	return nil
}

func preflight(ctx context.Context, readerAt io.ReaderAt, files []*zip.File, directory zipDirectory, limits extractionLimits) ([]archiveEntry, error) {
	if len(files) == 0 {
		return nil, errors.New("source archive is empty")
	}
	if len(files) > limits.entries {
		return nil, fmt.Errorf("source archive contains more than %d entries", limits.entries)
	}
	entries := make([]archiveEntry, 0, len(files))
	nodes := make(map[pathNodeKey]*pathNode, len(files)+limits.directories)
	nextNodeID := 1
	directories := 0
	root := ""
	rootEntry := false
	rootGoMod := false
	regularFiles := 0
	var declared uint64
	central := newZipDirectoryCursor(readerAt, directory)
	nameBuffer := make([]byte, limits.pathBytes+1)
	localRecordBuffer := make([]byte, zipFileHeaderBytes+limits.pathBytes+1)
	descriptorBuffer := make([]byte, zipDataDescriptorBytes)
	localRecordEnds := make(map[int64]int64, len(files))
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, err := validateArchivePath(file.Name, limits)
		if err != nil {
			return nil, err
		}
		localOffset, err := central.next(file, nameBuffer)
		if err != nil {
			return nil, err
		}
		nameEndsInSlash := strings.HasSuffix(file.Name, "/")
		dir, localEnd, err := validateZipEntryMetadata(file, nameEndsInSlash, readerAt, localOffset, directory.offset, localRecordBuffer, descriptorBuffer)
		if err != nil {
			return nil, err
		}
		if _, exists := localRecordEnds[localOffset]; exists {
			return nil, fmt.Errorf("source archive path %q reuses a local file record", file.Name)
		}
		localRecordEnds[localOffset] = localEnd
		if root == "" {
			root = path.root
		} else if root != path.root {
			return nil, errors.New("source archive contains multiple roots")
		}
		if path.relative == "" {
			if !dir {
				return nil, errors.New("source archive root must be a directory")
			}
			if rootEntry {
				return nil, errors.New("source archive contains a duplicate root directory")
			}
			rootEntry = true
			continue
		}
		relative := path.relative
		canonical := canonicalPortablePath(relative)
		parentID := 0
		componentStart := 0
		for {
			slash := strings.IndexByte(canonical[componentStart:], '/')
			componentEnd := len(canonical)
			final := true
			if slash >= 0 {
				componentEnd = componentStart + slash
				final = false
			}
			key := pathNodeKey{parent: parentID, component: canonical[componentStart:componentEnd]}
			component := relative[componentStart:componentEnd]
			spelling := relative[:componentEnd]
			node, found := nodes[key]
			if found && node.component != component {
				return nil, fmt.Errorf("source archive contains case-colliding paths %q and %q", node.spelling, spelling)
			}
			wantDirectory := !final || dir
			if found {
				if !final && !node.directory {
					return nil, fmt.Errorf("source archive path %q is nested below a file", relative)
				}
				if final {
					if node.explicit {
						return nil, fmt.Errorf("source archive contains duplicate path %q", relative)
					}
					if node.directory != wantDirectory {
						return nil, fmt.Errorf("source archive path %q is both a file and directory", relative)
					}
					node.explicit = true
				}
			} else {
				node = &pathNode{id: nextNodeID, component: component, spelling: spelling, directory: wantDirectory, explicit: final}
				nextNodeID++
				nodes[key] = node
				if wantDirectory {
					directories++
				}
			}
			parentID = node.id
			if final {
				break
			}
			componentStart = componentEnd + 1
		}

		if dir {
			if relative == "go.mod" {
				return nil, errors.New("source archive root go.mod must be a regular file")
			}
		} else {
			regularFiles++
			if regularFiles > limits.files {
				return nil, fmt.Errorf("source archive contains more than %d files", limits.files)
			}
			if file.UncompressedSize64 > limits.fileBytes {
				return nil, fmt.Errorf("source archive file %q exceeds %s limit", relative, byteLimit(limits.fileBytes))
			}
			if file.UncompressedSize64 > limits.expandedBytes || declared > limits.expandedBytes-file.UncompressedSize64 {
				return nil, fmt.Errorf("source archive expands beyond %s limit", byteLimit(limits.expandedBytes))
			}
			declared += file.UncompressedSize64
			if relative == "go.mod" {
				rootGoMod = true
			}
		}
		if directories > limits.directories {
			return nil, fmt.Errorf("source archive contains more than %d directories", limits.directories)
		}
		entries = append(entries, archiveEntry{file: file, relative: relative, dir: dir})
	}
	if root == "" {
		return nil, errors.New("source archive is empty")
	}
	if central.remaining != 0 {
		return nil, errors.New("source archive central directory changed during preflight")
	}
	if err := validateLocalRecordCoverage(localRecordEnds, directory.offset); err != nil {
		return nil, err
	}
	if !rootGoMod {
		return nil, errors.New("source archive does not contain a regular go.mod file at the exact root path")
	}
	return entries, nil
}

func validateArchivePath(raw string, limits extractionLimits) (validatedArchivePath, error) {
	if raw == "" {
		return validatedArchivePath{}, errors.New("source archive contains an invalid path")
	}
	name := raw
	if name[len(name)-1] == '/' {
		name = name[:len(name)-1]
	}
	if len(name) > limits.pathBytes {
		return validatedArchivePath{}, fmt.Errorf("source archive path exceeds %d-byte limit", limits.pathBytes)
	}
	componentStart := 0
	components := 0
	rootEnd := -1
	for index := 0; index <= len(name); index++ {
		if index != len(name) {
			if name[index] == 0 || name[index] == '\\' {
				return validatedArchivePath{}, errors.New("source archive contains an invalid path")
			}
			if name[index] != '/' {
				continue
			}
		}
		if componentStart == index {
			return validatedArchivePath{}, errors.New("source archive contains an invalid path component")
		}
		component := name[componentStart:index]
		if component == "." || component == ".." {
			return validatedArchivePath{}, errors.New("source archive contains an invalid path component")
		}
		if len(component) > limits.componentBytes {
			return validatedArchivePath{}, fmt.Errorf("source archive path component exceeds %d-byte limit", limits.componentBytes)
		}
		if !portablePathComponent(component) {
			return validatedArchivePath{}, fmt.Errorf("source archive path component %q is not portable", component)
		}
		components++
		if components > limits.pathDepth+1 { // Include the stripped archive root.
			return validatedArchivePath{}, fmt.Errorf("source archive path exceeds %d-component depth limit", limits.pathDepth)
		}
		if rootEnd < 0 && index < len(name) {
			rootEnd = index
		}
		componentStart = index + 1
	}
	if rootEnd < 0 {
		return validatedArchivePath{name: name, root: name}, nil
	}
	return validatedArchivePath{name: name, root: name[:rootEnd], relative: name[rootEnd+1:]}, nil
}

func portablePathComponent(component string) bool {
	if component == "." || component == ".." || component[len(component)-1] == '.' || component[len(component)-1] == ' ' {
		return false
	}
	for index := 0; index < len(component); index++ {
		switch component[index] {
		case '<', '>', ':', '"', '|', '?', '*':
			return false
		}
		if component[index] < 0x20 || component[index] > 0x7e {
			return false
		}
	}
	base := component
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	for len(base) > 0 && base[len(base)-1] == ' ' {
		base = base[:len(base)-1]
	}
	if len(base) == 3 && (equalFoldASCII(base, "CON") || equalFoldASCII(base, "PRN") ||
		equalFoldASCII(base, "AUX") || equalFoldASCII(base, "NUL")) {
		return false
	}
	if len(base) == 4 && base[3] >= '1' && base[3] <= '9' &&
		(equalFoldASCII(base[:3], "COM") || equalFoldASCII(base[:3], "LPT")) {
		return false
	}
	if equalFoldASCII(base, "CONIN$") || equalFoldASCII(base, "CONOUT$") {
		return false
	}
	return true
}

func equalFoldASCII(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		value := left[index]
		if value >= 'a' && value <= 'z' {
			value -= 'a' - 'A'
		}
		if value != right[index] {
			return false
		}
	}
	return true
}

func canonicalPortablePath(path string) string {
	firstUpper := -1
	for index := range path {
		if path[index] >= 'A' && path[index] <= 'Z' {
			firstUpper = index
			break
		}
	}
	if firstUpper < 0 {
		return path
	}
	var canonical strings.Builder
	canonical.Grow(len(path))
	canonical.WriteString(path[:firstUpper])
	for index := firstUpper; index < len(path); index++ {
		value := path[index]
		if value >= 'A' && value <= 'Z' {
			value += 'a' - 'A'
		}
		canonical.WriteByte(value)
	}
	return canonical.String()
}

func validateZipEntryMetadata(file *zip.File, nameEndsInSlash bool, readerAt io.ReaderAt, localOffset, directoryOffset int64, localRecordBuffer, descriptorBuffer []byte) (bool, int64, error) {
	mode := file.Mode()
	kind := mode.Type()
	if nameEndsInSlash {
		if kind != fs.ModeDir {
			return false, 0, fmt.Errorf("source archive directory %q has unsupported file type %s", file.Name, kind)
		}
		if file.UncompressedSize64 != 0 {
			return false, 0, fmt.Errorf("source archive directory %q declares nonzero content", file.Name)
		}
	} else {
		if kind&fs.ModeDir != 0 {
			return false, 0, fmt.Errorf("source archive directory %q must end in a slash", file.Name)
		}
		if !mode.IsRegular() {
			return false, 0, fmt.Errorf("source archive path %q has unsupported file type %s", file.Name, kind)
		}
	}
	if file.Flags&(zipEncryptedFlag|zipStrongEncryptionFlag) != 0 {
		return false, 0, fmt.Errorf("source archive path %q is encrypted", file.Name)
	}
	if file.Method != zip.Store && file.Method != zip.Deflate {
		return false, 0, fmt.Errorf("source archive path %q uses unsupported compression method %d", file.Name, file.Method)
	}
	allowedFlags := uint16(zipDataDescriptorFlag | zipUTF8Flag)
	if file.Method == zip.Deflate {
		allowedFlags |= zipDeflateOptionFlags
	}
	if unsupported := file.Flags &^ allowedFlags; unsupported != 0 {
		return false, 0, fmt.Errorf("source archive path %q uses unsupported general-purpose flags %#x", file.Name, unsupported)
	}
	if file.ReaderVersion > zipVersion20 {
		return false, 0, fmt.Errorf("source archive path %q requires unsupported ZIP version %d.%d", file.Name, file.ReaderVersion/10, file.ReaderVersion%10)
	}
	if file.Method == zip.Store && file.CompressedSize64 != file.UncompressedSize64 {
		return false, 0, fmt.Errorf("source archive stored path %q has mismatched compressed and uncompressed sizes", file.Name)
	}
	dataOffset, err := file.DataOffset()
	if err != nil {
		return false, 0, fmt.Errorf("source archive path %q has a malformed local file header: %w", file.Name, err)
	}
	localRecordBytes := zipFileHeaderBytes + len(file.Name)
	if localRecordBytes > len(localRecordBuffer) {
		return false, 0, fmt.Errorf("source archive path %q exceeds the local metadata buffer", file.Name)
	}
	localRecord := localRecordBuffer[:localRecordBytes]
	localHeader := localRecord[:zipFileHeaderBytes]
	if localOffset < 0 || directoryOffset < localOffset || int64(len(localRecord)) > directoryOffset-localOffset {
		return false, 0, fmt.Errorf("source archive path %q has a local file header outside the file-data region", file.Name)
	}
	if err := readZipMetadataAt(readerAt, localRecord, localOffset); err != nil {
		return false, 0, fmt.Errorf("read source archive path %q local file header: %w", file.Name, err)
	}
	if binary.LittleEndian.Uint32(localHeader[0:4]) != zipFileHeaderSignature {
		return false, 0, fmt.Errorf("source archive path %q has a malformed local file header", file.Name)
	}
	if binary.LittleEndian.Uint16(localHeader[4:6]) != file.ReaderVersion {
		return false, 0, fmt.Errorf("source archive path %q local file header ZIP version does not match the central directory", file.Name)
	}
	localFlags := binary.LittleEndian.Uint16(localHeader[6:8])
	if localFlags != file.Flags {
		return false, 0, fmt.Errorf("source archive path %q local file header flags do not match the central directory", file.Name)
	}
	if binary.LittleEndian.Uint16(localHeader[8:10]) != file.Method {
		return false, 0, fmt.Errorf("source archive path %q local file header compression method does not match the central directory", file.Name)
	}
	localCompressed := binary.LittleEndian.Uint32(localHeader[18:22])
	localUncompressed := binary.LittleEndian.Uint32(localHeader[22:26])
	if localCompressed == ^uint32(0) || localUncompressed == ^uint32(0) {
		return false, 0, errors.New("source archive uses unsupported ZIP64 entry metadata")
	}
	nameBytes := int64(binary.LittleEndian.Uint16(localHeader[26:28]))
	extraBytes := int64(binary.LittleEndian.Uint16(localHeader[28:30]))
	wantDataOffset := localOffset + int64(len(localHeader)) + nameBytes + extraBytes
	if wantDataOffset < localOffset || wantDataOffset != dataOffset || nameBytes != int64(len(file.Name)) {
		return false, 0, fmt.Errorf("source archive path %q local file header name or extra metadata is inconsistent", file.Name)
	}
	for index := range file.Name {
		if localRecord[zipFileHeaderBytes+index] != file.Name[index] {
			return false, 0, fmt.Errorf("source archive path %q local file header name does not match the central directory", file.Name)
		}
	}
	if extraBytes != 0 {
		localExtra := io.NewSectionReader(readerAt, localOffset+int64(len(localHeader))+nameBytes, extraBytes)
		if err := validateZipExtraFields(localExtra, uint64(extraBytes), "local file header"); err != nil {
			return false, 0, err
		}
	}
	localCRC := binary.LittleEndian.Uint32(localHeader[14:18])
	if localFlags&zipDataDescriptorFlag == 0 {
		if localCRC != file.CRC32 || uint64(localCompressed) != file.CompressedSize64 || uint64(localUncompressed) != file.UncompressedSize64 {
			return false, 0, fmt.Errorf("source archive path %q local file header sizes or checksum do not match the central directory", file.Name)
		}
	} else {
		if localCRC != 0 || localCompressed != 0 || localUncompressed != 0 {
			return false, 0, fmt.Errorf("source archive path %q local file header sizes and checksum must be zero when using a data descriptor", file.Name)
		}
	}
	if dataOffset < 0 || directoryOffset < 0 || dataOffset > directoryOffset ||
		file.CompressedSize64 > uint64(directoryOffset-dataOffset) {
		return false, 0, fmt.Errorf("source archive path %q has compressed data outside the file-data region", file.Name)
	}
	dataEnd := dataOffset + int64(file.CompressedSize64)
	if localFlags&zipDataDescriptorFlag != 0 {
		descriptorEnd, err := validateZipDataDescriptor(readerAt, dataEnd, directoryOffset, file, descriptorBuffer)
		if err != nil {
			return false, 0, err
		}
		dataEnd = descriptorEnd
	}
	return nameEndsInSlash, dataEnd, nil
}

func validateZipDataDescriptor(readerAt io.ReaderAt, offset, directoryOffset int64, file *zip.File, descriptorBuffer []byte) (int64, error) {
	if len(descriptorBuffer) < zipDataDescriptorBytes {
		return 0, errors.New("source archive data descriptor buffer is too small")
	}
	descriptor := descriptorBuffer[:zipDataDescriptorBytes]
	if offset < 0 || directoryOffset < offset || zipDataDescriptorUnsignedBytes > directoryOffset-offset {
		return 0, fmt.Errorf("source archive path %q has a data descriptor outside the file-data region", file.Name)
	}
	readBytes := zipDataDescriptorBytes
	if int64(readBytes) > directoryOffset-offset {
		readBytes = zipDataDescriptorUnsignedBytes
	}
	if err := readZipMetadataAt(readerAt, descriptor[:readBytes], offset); err != nil {
		return 0, fmt.Errorf("read source archive path %q data descriptor: %w", file.Name, err)
	}
	signed := binary.LittleEndian.Uint32(descriptor[0:4]) == zipDataDescriptorSignature
	valueOffset := 0
	if signed {
		if readBytes != zipDataDescriptorBytes {
			return 0, fmt.Errorf("source archive path %q has a truncated data descriptor", file.Name)
		}
		valueOffset = 4
	}
	if binary.LittleEndian.Uint32(descriptor[valueOffset:valueOffset+4]) != file.CRC32 ||
		uint64(binary.LittleEndian.Uint32(descriptor[valueOffset+4:valueOffset+8])) != file.CompressedSize64 ||
		uint64(binary.LittleEndian.Uint32(descriptor[valueOffset+8:valueOffset+12])) != file.UncompressedSize64 {
		return 0, fmt.Errorf("source archive path %q data descriptor does not match the central directory", file.Name)
	}
	return offset + int64(valueOffset+zipDataDescriptorUnsignedBytes), nil
}

func validateLocalRecordCoverage(recordEnds map[int64]int64, directoryOffset int64) error {
	offset := int64(0)
	visited := 0
	for offset < directoryOffset {
		end, ok := recordEnds[offset]
		if !ok || end <= offset || end > directoryOffset {
			return errors.New("source archive file-data region contains gaps, overlaps, or unsupported records")
		}
		offset = end
		visited++
	}
	if offset != directoryOffset || visited != len(recordEnds) {
		return errors.New("source archive file-data region contains gaps, overlaps, or unsupported records")
	}
	return nil
}

func readZipMetadataAt(readerAt io.ReaderAt, destination []byte, offset int64) error {
	read, err := readerAt.ReadAt(destination, offset)
	if err != nil {
		return err
	}
	if read != len(destination) {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func containedDestination(root, relative string) (string, error) {
	destination := filepath.Join(root, filepath.FromSlash(relative))
	contained, err := filepath.Rel(root, destination)
	if err != nil || filepath.IsAbs(contained) || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
		return "", errors.New("source archive path escapes destination")
	}
	return destination, nil
}

func extractFile(ctx context.Context, entry *zip.File, destination string, buffer []byte) (uint64, error) {
	source, err := entry.Open()
	if err != nil {
		return 0, err
	}
	destinationFile, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, entry.Mode().Perm())
	if err != nil {
		_ = source.Close()
		return 0, err
	}
	limited := &io.LimitedReader{R: source, N: int64(entry.UncompressedSize64) + 1}
	var copied uint64
	for {
		if err := ctx.Err(); err != nil {
			return copied, closeExtractedFile(destinationFile, source, err)
		}
		read, readErr := limited.Read(buffer)
		if read > 0 {
			written, writeErr := destinationFile.Write(buffer[:read])
			copied += uint64(written)
			if writeErr != nil {
				return copied, closeExtractedFile(destinationFile, source, writeErr)
			}
			if written != read {
				return copied, closeExtractedFile(destinationFile, source, io.ErrShortWrite)
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return copied, closeExtractedFile(destinationFile, source, readErr)
			}
			break
		}
	}
	if copied != entry.UncompressedSize64 {
		return copied, closeExtractedFile(destinationFile, source,
			fmt.Errorf("source archive file %q expanded to %d bytes, declared %d", entry.Name, copied, entry.UncompressedSize64))
	}
	if err := closeExtractedFile(destinationFile, source, nil); err != nil {
		return copied, err
	}
	return copied, nil
}

func closeExtractedFile(destination *os.File, source io.Closer, extractionErr error) error {
	return errors.Join(extractionErr, destination.Close(), source.Close())
}

func byteLimit(value uint64) string {
	if value%(1<<20) == 0 {
		return fmt.Sprintf("%d MiB", value>>20)
	}
	return fmt.Sprintf("%d-byte", value)
}
