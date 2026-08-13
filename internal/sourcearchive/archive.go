// Package sourcearchive safely extracts source archives with one top-level directory.
package sourcearchive

import (
	"archive/zip"
	"context"
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

	entries, err := preflight(ctx, reader.File, directory.offset, limits)
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

func preflight(ctx context.Context, files []*zip.File, directoryOffset int64, limits extractionLimits) ([]archiveEntry, error) {
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
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, err := validateArchivePath(file.Name, limits)
		if err != nil {
			return nil, err
		}
		nameEndsInSlash := strings.HasSuffix(file.Name, "/")
		dir, err := validateZipEntryMetadata(file, nameEndsInSlash, directoryOffset)
		if err != nil {
			return nil, err
		}
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
	if len(base) == 3 && (equalFoldASCII(base, "CON") || equalFoldASCII(base, "PRN") ||
		equalFoldASCII(base, "AUX") || equalFoldASCII(base, "NUL")) {
		return false
	}
	if len(base) == 4 && base[3] >= '1' && base[3] <= '9' &&
		(equalFoldASCII(base[:3], "COM") || equalFoldASCII(base[:3], "LPT")) {
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

func validateZipEntryMetadata(file *zip.File, nameEndsInSlash bool, directoryOffset int64) (bool, error) {
	mode := file.Mode()
	kind := mode.Type()
	if nameEndsInSlash {
		if kind != fs.ModeDir {
			return false, fmt.Errorf("source archive directory %q has unsupported file type %s", file.Name, kind)
		}
		if file.UncompressedSize64 != 0 {
			return false, fmt.Errorf("source archive directory %q declares nonzero content", file.Name)
		}
	} else {
		if kind&fs.ModeDir != 0 {
			return false, fmt.Errorf("source archive directory %q must end in a slash", file.Name)
		}
		if !mode.IsRegular() {
			return false, fmt.Errorf("source archive path %q has unsupported file type %s", file.Name, kind)
		}
	}
	if file.Flags&(1|1<<6) != 0 {
		return false, fmt.Errorf("source archive path %q is encrypted", file.Name)
	}
	if file.Method != zip.Store && file.Method != zip.Deflate {
		return false, fmt.Errorf("source archive path %q uses unsupported compression method %d", file.Name, file.Method)
	}
	if file.Method == zip.Store && file.CompressedSize64 != file.UncompressedSize64 {
		return false, fmt.Errorf("source archive stored path %q has mismatched compressed and uncompressed sizes", file.Name)
	}
	dataOffset, err := file.DataOffset()
	if err != nil {
		return false, fmt.Errorf("source archive path %q has a malformed local file header: %w", file.Name, err)
	}
	if dataOffset < 0 || directoryOffset < 0 || dataOffset > directoryOffset ||
		file.CompressedSize64 > uint64(directoryOffset-dataOffset) {
		return false, fmt.Errorf("source archive path %q has compressed data outside the file-data region", file.Name)
	}
	return nameEndsInSlash, nil
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
