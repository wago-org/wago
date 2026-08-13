// Package sourcearchive safely extracts source archives with one top-level directory.
package sourcearchive

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
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
	if ctx == nil {
		return errors.New("source archive extraction requires a context")
	}
	if err := validateLimits(limits); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := inspectZipDirectory(ctx, archive, limits); err != nil {
		return err
	}
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()

	entries, err := preflight(ctx, reader.File, limits)
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
	publish := false
	defer func() {
		if !publish {
			_ = os.RemoveAll(staging)
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
	info, err := os.Stat(filepath.Join(staging, "go.mod"))
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("source archive does not contain a regular go.mod file")
	}
	if err := publishDirectoryNoReplace(staging, target); err != nil {
		return fmt.Errorf("publish source archive: %w", err)
	}
	publish = true
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

func preflight(ctx context.Context, files []*zip.File, limits extractionLimits) ([]archiveEntry, error) {
	if len(files) == 0 {
		return nil, errors.New("source archive is empty")
	}
	if len(files) > limits.entries {
		return nil, fmt.Errorf("source archive contains more than %d entries", limits.entries)
	}
	entries := make([]archiveEntry, 0, len(files))
	entryPaths := make(map[string]string, len(files))
	treeKinds := make(map[string]bool, len(files)) // true means directory.
	treePaths := make(map[string]string, len(files))
	directories := make(map[string]struct{})
	root := ""
	rootEntry := false
	regularFiles := 0
	var declared uint64
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name, components, err := validateArchivePath(file.Name, limits)
		if err != nil {
			return nil, err
		}
		if root == "" {
			root = components[0]
		} else if root != components[0] {
			return nil, errors.New("source archive contains multiple roots")
		}
		dir := file.FileInfo().IsDir()
		if !dir && !file.Mode().IsRegular() {
			return nil, fmt.Errorf("source archive path %q has unsupported file type %s", name, file.Mode().Type())
		}
		if len(components) == 1 {
			if !dir {
				return nil, errors.New("source archive root must be a directory")
			}
			if rootEntry {
				return nil, errors.New("source archive contains a duplicate root directory")
			}
			rootEntry = true
			continue
		}
		relative := strings.Join(components[1:], "/")
		folded := strings.ToLower(relative)
		if previous, ok := entryPaths[folded]; ok {
			if previous == relative {
				return nil, fmt.Errorf("source archive contains duplicate path %q", relative)
			}
			return nil, fmt.Errorf("source archive contains case-colliding paths %q and %q", previous, relative)
		}
		entryPaths[folded] = relative

		parents := components[1 : len(components)-1]
		for index := range parents {
			parent := strings.Join(parents[:index+1], "/")
			key := strings.ToLower(parent)
			if previous, ok := treePaths[key]; ok && previous != parent {
				return nil, fmt.Errorf("source archive contains case-colliding paths %q and %q", previous, parent)
			}
			if kind, ok := treeKinds[key]; ok && !kind {
				return nil, fmt.Errorf("source archive path %q is nested below a file", relative)
			}
			treePaths[key] = parent
			treeKinds[key] = true
			directories[key] = struct{}{}
		}
		if previous, ok := treePaths[folded]; ok && previous != relative {
			return nil, fmt.Errorf("source archive contains case-colliding paths %q and %q", previous, relative)
		}
		if kind, ok := treeKinds[folded]; ok && kind != dir {
			return nil, fmt.Errorf("source archive path %q is both a file and directory", relative)
		}
		treePaths[folded] = relative
		treeKinds[folded] = dir
		if dir {
			directories[folded] = struct{}{}
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
		}
		if len(directories) > limits.directories {
			return nil, fmt.Errorf("source archive contains more than %d directories", limits.directories)
		}
		entries = append(entries, archiveEntry{file: file, relative: relative, dir: dir})
	}
	if root == "" {
		return nil, errors.New("source archive is empty")
	}
	return entries, nil
}

func validateArchivePath(raw string, limits extractionLimits) (string, []string, error) {
	if raw == "" || strings.ContainsRune(raw, 0) || strings.Contains(raw, "\\") || strings.HasPrefix(raw, "/") {
		return "", nil, errors.New("source archive contains an invalid path")
	}
	name := strings.TrimSuffix(raw, "/")
	if len(name) > limits.pathBytes {
		return "", nil, fmt.Errorf("source archive path exceeds %d-byte limit", limits.pathBytes)
	}
	components := strings.Split(name, "/")
	if len(components) > limits.pathDepth+1 { // Include the stripped archive root.
		return "", nil, fmt.Errorf("source archive path exceeds %d-component depth limit", limits.pathDepth)
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return "", nil, errors.New("source archive contains an invalid path component")
		}
		if len(component) > limits.componentBytes {
			return "", nil, fmt.Errorf("source archive path component exceeds %d-byte limit", limits.componentBytes)
		}
		if !portablePathComponent(component) {
			return "", nil, fmt.Errorf("source archive path component %q is not portable", component)
		}
	}
	return name, components, nil
}

func portablePathComponent(component string) bool {
	if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") || strings.ContainsAny(component, `<>:"|?*`) {
		return false
	}
	for index := 0; index < len(component); index++ {
		if component[index] < 0x20 || component[index] > 0x7e {
			return false
		}
	}
	base := component
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	}
	return true
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
	if err := destination.Close(); extractionErr == nil {
		extractionErr = err
	}
	if err := source.Close(); extractionErr == nil {
		extractionErr = err
	}
	return extractionErr
}

func byteLimit(value uint64) string {
	if value%(1<<20) == 0 {
		return fmt.Sprintf("%d MiB", value>>20)
	}
	return fmt.Sprintf("%d-byte", value)
}
