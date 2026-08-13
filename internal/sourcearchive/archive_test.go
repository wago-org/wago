package sourcearchive

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestExtractPublishesValidSingleRootArchive(t *testing.T) {
	archive := writeArchive(t, []zipEntry{
		{name: "root/", dir: true},
		{name: "root/go.mod", data: "module example.com/test\n"},
		{name: "root/cmd/tool/main.go", data: "package main\n"},
	})
	target := filepath.Join(t.TempDir(), "out")
	if err := Extract(archive, target); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "cmd", "tool", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package main\n" {
		t.Fatalf("main.go = %q", data)
	}
	assertNoStaging(t, target)
}

func TestExtractPreflightRejectsUnsafeOrUnboundedArchives(t *testing.T) {
	base := extractionLimits{entries: 10, files: 10, directories: 10, metadataBytes: 4 << 10, pathBytes: 80, pathDepth: 5, componentBytes: 32, fileBytes: 32, expandedBytes: 64}
	tests := []struct {
		name    string
		entries []zipEntry
		limits  extractionLimits
		want    string
	}{
		{name: "traversal", entries: []zipEntry{{name: "root/../../escape", data: "bad"}}, limits: base, want: "invalid path component"},
		{name: "backslash traversal", entries: []zipEntry{{name: `root\..\escape`, data: "bad"}}, limits: base, want: "invalid path"},
		{name: "multiple roots", entries: []zipEntry{{name: "one/go.mod"}, {name: "two/file"}}, limits: base, want: "multiple roots"},
		{name: "entries", entries: []zipEntry{{name: "root/go.mod"}, {name: "root/a"}, {name: "root/b"}}, limits: withEntries(base, 2), want: "more than 2 entries"},
		{name: "files", entries: []zipEntry{{name: "root/go.mod"}, {name: "root/a"}}, limits: withFiles(base, 1), want: "more than 1 files"},
		{name: "directories", entries: []zipEntry{{name: "root/a/b/go.mod"}}, limits: withDirectories(base, 1), want: "more than 1 directories"},
		{name: "metadata bytes", entries: []zipEntry{{name: "root/go.mod"}}, limits: withMetadataBytes(base, 1), want: "central directory exceeds 1-byte"},
		{name: "path bytes", entries: []zipEntry{{name: "root/very-long-name/go.mod"}}, limits: withPathBytes(base, 12), want: "path exceeds 12-byte"},
		{name: "path depth", entries: []zipEntry{{name: "root/a/b/go.mod"}}, limits: withPathDepth(base, 2), want: "depth limit"},
		{name: "component bytes", entries: []zipEntry{{name: "root/long/go.mod"}}, limits: withComponentBytes(base, 3), want: "component exceeds 3-byte"},
		{name: "nonportable component", entries: []zipEntry{{name: "root/CON/go.mod"}}, limits: base, want: "not portable"},
		{name: "non-ASCII component", entries: []zipEntry{{name: "root/café/go.mod"}}, limits: base, want: "not portable"},
		{name: "invalid UTF-8 component", entries: []zipEntry{{name: "root/\xff/go.mod", nonUTF8: true}}, limits: base, want: "not portable"},
		{name: "file size", entries: []zipEntry{{name: "root/go.mod", data: strings.Repeat("x", 33)}}, limits: base, want: "file \"go.mod\" exceeds 32-byte"},
		{name: "expanded size", entries: []zipEntry{{name: "root/go.mod", data: strings.Repeat("x", 32)}, {name: "root/a", data: strings.Repeat("x", 32)}, {name: "root/b", data: "x"}}, limits: base, want: "expands beyond 64-byte"},
		{name: "duplicate", entries: []zipEntry{{name: "root/go.mod"}, {name: "root/go.mod"}}, limits: base, want: "duplicate path"},
		{name: "case collision", entries: []zipEntry{{name: "root/go.mod"}, {name: "root/GO.MOD"}}, limits: base, want: "case-colliding"},
		{name: "implicit directory case collision", entries: []zipEntry{{name: "root/a/one"}, {name: "root/A/two"}}, limits: base, want: "case-colliding"},
		{name: "file directory conflict", entries: []zipEntry{{name: "root/a", data: "x"}, {name: "root/a/go.mod"}}, limits: base, want: "nested below a file"},
		{name: "symlink", entries: []zipEntry{{name: "root/go.mod"}, {name: "root/link", mode: os.ModeSymlink | 0o777}}, limits: base, want: "unsupported file type"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := writeArchive(t, test.entries)
			target := filepath.Join(t.TempDir(), "out")
			err := extractWithLimits(context.Background(), archive, target, test.limits)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Extract error = %v, want containing %q", err, test.want)
			}
			assertAbsent(t, target)
			assertNoStaging(t, target)
		})
	}
}

func TestExtractRejectsForgedCentralDirectoryEntryCount(t *testing.T) {
	archive := writeArchive(t, []zipEntry{{name: "root/go.mod"}, {name: "root/a"}, {name: "root/b"}})
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	end := bytes.LastIndex(data, []byte("PK\x05\x06"))
	if end < 0 {
		t.Fatal("ZIP central directory end not found")
	}
	binary.LittleEndian.PutUint16(data[end+8:], 1)
	binary.LittleEndian.PutUint16(data[end+10:], 1)
	if err := os.WriteFile(archive, data, 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "out")
	err = Extract(archive, target)
	if err == nil || !strings.Contains(err.Error(), "declares 1 entries but contains 3") {
		t.Fatalf("Extract error = %v, want forged entry-count rejection", err)
	}
	assertAbsent(t, target)
}

func TestExtractRejectsPrependedZipPayload(t *testing.T) {
	archive := writeArchive(t, []zipEntry{{name: "root/go.mod"}})
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	data = append([]byte("self-extracting-prefix"), data...)
	if err := os.WriteFile(archive, data, 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "out")
	err = Extract(archive, target)
	if err == nil || !strings.Contains(err.Error(), "unsupported data before") {
		t.Fatalf("Extract error = %v, want prepended-data rejection", err)
	}
	assertAbsent(t, target)
}

func TestExtractRejectsDishonestExpandedSizeAndCleansStaging(t *testing.T) {
	archive := writeArchive(t, []zipEntry{{name: "root/go.mod", data: "module example.com/test\n"}})
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	central := bytes.Index(data, []byte("PK\x01\x02"))
	if central < 0 {
		t.Fatal("ZIP central directory not found")
	}
	binary.LittleEndian.PutUint32(data[central+24:], 1)
	if err := os.WriteFile(archive, data, 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "out")
	err = Extract(archive, target)
	if err == nil {
		t.Fatal("dishonest expanded size unexpectedly succeeded")
	}
	assertAbsent(t, target)
	assertNoStaging(t, target)
}

func TestExtractMissingGoModCleansStaging(t *testing.T) {
	archive := writeArchive(t, []zipEntry{{name: "root/README.md", data: "missing module\n"}})
	target := filepath.Join(t.TempDir(), "out")
	err := Extract(archive, target)
	if err == nil || !strings.Contains(err.Error(), "regular go.mod") {
		t.Fatalf("Extract error = %v, want missing go.mod", err)
	}
	assertAbsent(t, target)
	assertNoStaging(t, target)
}

func TestPreflightDeepPathsHasBoundedAllocations(t *testing.T) {
	limits := productionExtractionLimits()
	archive := writeProductionPathArchive(t, 256)
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	directory, err := locateZipDirectory(file, info.Size(), limits)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(3, func() {
		entries, err := preflight(context.Background(), reader.File, directory.offset, limits)
		if err != nil {
			panic(err)
		}
		if len(entries) != 256 {
			panic("wrong preflight entry count")
		}
	})
	if allocations >= 5_000 {
		t.Fatalf("preflight allocations = %.0f, want fewer than 5000", allocations)
	}
}

func TestExtractOpensArchiveOnce(t *testing.T) {
	archive := writeArchive(t, []zipEntry{{name: "root/go.mod", data: "module example.com/test\n"}})
	target := filepath.Join(t.TempDir(), "out")
	operations := defaultExtractionOperations()
	openArchive := operations.openArchive
	openCount := 0
	cleanupCount := 0
	operations.openArchive = func(path string) (archiveReadCloser, error) {
		openCount++
		return openArchive(path)
	}
	operations.removeAll = func(path string) error {
		cleanupCount++
		return os.RemoveAll(path)
	}
	if err := extractWithOperations(context.Background(), archive, target, productionExtractionLimits(), operations); err != nil {
		t.Fatal(err)
	}
	if openCount != 1 {
		t.Fatalf("archive open count = %d, want 1", openCount)
	}
	if cleanupCount != 0 {
		t.Fatalf("cleanup count after publication = %d, want 0", cleanupCount)
	}
}

func TestExtractUsesOpenedArchiveAfterPathReplacement(t *testing.T) {
	archive := writeArchive(t, []zipEntry{{name: "root/go.mod", data: "module original.example/test\n"}})
	replacement := writeArchive(t, []zipEntry{{name: "replacement/README.md", data: "wrong archive\n"}})
	target := filepath.Join(t.TempDir(), "out")
	operations := defaultExtractionOperations()
	operations.openArchive = func(path string) (archiveReadCloser, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		original := path + ".opened"
		if err := os.Rename(path, original); err != nil {
			_ = file.Close()
			return nil, err
		}
		if err := os.Rename(replacement, path); err != nil {
			_ = file.Close()
			return nil, err
		}
		return file, nil
	}
	if err := extractWithOperations(context.Background(), archive, target, productionExtractionLimits(), operations); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "module original.example/test\n" {
		t.Fatalf("extracted go.mod = %q", data)
	}
}

func TestZipInsecurePathErrorClosesOpenedArchive(t *testing.T) {
	t.Setenv("GODEBUG", "zipinsecurepath=0")
	archive := writeArchive(t, []zipEntry{
		{name: "root/go.mod", data: "module example.com/test\n"},
		{name: `root\bad`, data: "bad"},
	})
	target := filepath.Join(t.TempDir(), "out")
	operations := defaultExtractionOperations()
	var closed atomic.Bool
	operations.openArchive = func(path string) (archiveReadCloser, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return &trackedArchive{File: file, closed: &closed}, nil
	}
	err := extractWithOperations(context.Background(), archive, target, productionExtractionLimits(), operations)
	if !errors.Is(err, zip.ErrInsecurePath) {
		t.Fatalf("Extract error = %v, want zip.ErrInsecurePath", err)
	}
	if !closed.Load() {
		t.Fatal("archive was not closed after zip.ErrInsecurePath")
	}
	assertAbsent(t, target)
}

type trackedArchive struct {
	*os.File
	closed *atomic.Bool
}

type closeErrorArchive struct {
	*os.File
	err        error
	closeCount *atomic.Int32
}

func (archive *closeErrorArchive) Close() error {
	archive.closeCount.Add(1)
	return errors.Join(archive.File.Close(), archive.err)
}

func TestArchiveCloseFailurePreventsPublicationAndCleansStaging(t *testing.T) {
	archive := writeArchive(t, []zipEntry{{name: "root/go.mod", data: "module example.com/test\n"}})
	target := filepath.Join(t.TempDir(), "out")
	closeFailure := errors.New("injected archive close failure")
	var closeCount atomic.Int32
	operations := defaultExtractionOperations()
	operations.openArchive = func(path string) (archiveReadCloser, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return &closeErrorArchive{File: file, err: closeFailure, closeCount: &closeCount}, nil
	}
	err := extractWithOperations(context.Background(), archive, target, productionExtractionLimits(), operations)
	if !errors.Is(err, closeFailure) {
		t.Fatalf("Extract error = %v, want close failure", err)
	}
	if closeCount.Load() != 1 {
		t.Fatalf("archive close count = %d, want 1", closeCount.Load())
	}
	assertAbsent(t, target)
	assertNoStaging(t, target)
}

func (archive *trackedArchive) Close() error {
	archive.closed.Store(true)
	return archive.File.Close()
}

func TestExtractRejectsStrictMetadataBeforeFilesystemMutation(t *testing.T) {
	tests := []struct {
		name   string
		bad    zipEntry
		mutate func(*testing.T, string)
		want   string
	}{
		{name: "trailing slash symlink", bad: zipEntry{name: "root/bad/", mode: os.ModeSymlink | 0o777}, want: "unsupported file type"},
		{name: "trailing slash device", bad: zipEntry{name: "root/bad/", mode: os.ModeDevice | 0o600}, want: "unsupported file type"},
		{name: "trailing slash named pipe", bad: zipEntry{name: "root/bad/", mode: os.ModeNamedPipe | 0o600}, want: "unsupported file type"},
		{name: "trailing slash socket", bad: zipEntry{name: "root/bad/", mode: os.ModeSocket | 0o600}, want: "unsupported file type"},
		{name: "mode directory without slash", bad: zipEntry{name: "root/bad", mode: os.ModeDir | 0o755}, want: "must end in a slash"},
		{name: "directory with content", bad: zipEntry{name: "root/bad/", dir: true}, mutate: func(t *testing.T, archive string) {
			mutateZipEntry(t, archive, "root/bad/", func(data []byte, central, _, _ int) {
				binary.LittleEndian.PutUint32(data[central+24:], 1)
			})
		}, want: "declares nonzero content"},
		{name: "unsupported compression", bad: zipEntry{name: "root/bad", data: "x"}, mutate: func(t *testing.T, archive string) {
			mutateZipEntry(t, archive, "root/bad", func(data []byte, central, local, _ int) {
				binary.LittleEndian.PutUint16(data[central+10:], 99)
				binary.LittleEndian.PutUint16(data[local+8:], 99)
			})
		}, want: "unsupported compression method"},
		{name: "encrypted", bad: zipEntry{name: "root/bad", data: "x"}, mutate: func(t *testing.T, archive string) {
			mutateZipEntry(t, archive, "root/bad", func(data []byte, central, local, _ int) {
				binary.LittleEndian.PutUint16(data[central+8:], binary.LittleEndian.Uint16(data[central+8:])|1)
				binary.LittleEndian.PutUint16(data[local+6:], binary.LittleEndian.Uint16(data[local+6:])|1)
			})
		}, want: "is encrypted"},
		{name: "malformed local header", bad: zipEntry{name: "root/bad", data: "x"}, mutate: func(t *testing.T, archive string) {
			mutateZipEntry(t, archive, "root/bad", func(data []byte, _, local, _ int) {
				binary.LittleEndian.PutUint32(data[local:], 0)
			})
		}, want: "malformed local file header"},
		{name: "compressed range enters directory", bad: zipEntry{name: "root/bad", data: "x"}, mutate: func(t *testing.T, archive string) {
			mutateZipEntry(t, archive, "root/bad", func(data []byte, central, local, directory int) {
				nameBytes := int(binary.LittleEndian.Uint16(data[local+26:]))
				extraBytes := int(binary.LittleEndian.Uint16(data[local+28:]))
				dataOffset := local + 30 + nameBytes + extraBytes
				binary.LittleEndian.PutUint32(data[central+20:], uint32(directory-dataOffset+1))
			})
		}, want: "compressed data outside"},
		{name: "stored size mismatch", bad: zipEntry{name: "root/bad", data: "x", store: true}, mutate: func(t *testing.T, archive string) {
			mutateZipEntry(t, archive, "root/bad", func(data []byte, central, _, _ int) {
				binary.LittleEndian.PutUint32(data[central+20:], 2)
			})
		}, want: "mismatched compressed and uncompressed sizes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := writeArchive(t, []zipEntry{
				{name: "root/go.mod", data: "module example.com/test\n"},
				test.bad,
			})
			if test.mutate != nil {
				test.mutate(t, archive)
			}
			assertPreflightFailureBeforeMutation(t, archive, test.want)
		})
	}
}

func TestExtractRequiresExactRootGoModBeforeFilesystemMutation(t *testing.T) {
	tests := []struct {
		name    string
		entries []zipEntry
		want    string
	}{
		{name: "missing", entries: []zipEntry{{name: "root/README.md"}}, want: "regular go.mod file at the exact root path"},
		{name: "uppercase", entries: []zipEntry{{name: "root/GO.MOD"}}, want: "regular go.mod file at the exact root path"},
		{name: "mixed case", entries: []zipEntry{{name: "root/Go.Mod"}}, want: "regular go.mod file at the exact root path"},
		{name: "directory", entries: []zipEntry{{name: "root/go.mod/", dir: true}}, want: "root go.mod must be a regular file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := writeArchive(t, test.entries)
			assertPreflightFailureBeforeMutation(t, archive, test.want)
		})
	}
}

func TestCleanupFailureIsJoinedWithExtractionFailure(t *testing.T) {
	archive := writeArchive(t, []zipEntry{{name: "root/go.mod", data: "module example.com/test\n", store: true}})
	mutateZipEntry(t, archive, "root/go.mod", func(data []byte, _, local, _ int) {
		nameBytes := int(binary.LittleEndian.Uint16(data[local+26:]))
		extraBytes := int(binary.LittleEndian.Uint16(data[local+28:]))
		data[local+30+nameBytes+extraBytes] ^= 0xff
	})
	target := filepath.Join(t.TempDir(), "parent", "out")
	cleanupFailure := errors.New("injected cleanup failure")
	operations := defaultExtractionOperations()
	operations.removeAll = func(path string) error {
		err := os.RemoveAll(path)
		return errors.Join(err, cleanupFailure)
	}
	err := extractWithOperations(context.Background(), archive, target, productionExtractionLimits(), operations)
	if !errors.Is(err, zip.ErrChecksum) {
		t.Fatalf("Extract error = %v, want zip.ErrChecksum", err)
	}
	if !errors.Is(err, cleanupFailure) {
		t.Fatalf("Extract error = %v, want cleanup failure", err)
	}
	assertAbsent(t, target)
	assertNoStaging(t, target)
}

func TestExtractCancellationLeavesNoTargetOrStaging(t *testing.T) {
	archive := writeArchive(t, []zipEntry{
		{name: "root/go.mod", data: "module example.com/test\n"},
		{name: "root/large", data: strings.Repeat("x", 1<<20)},
	})
	ctx := &cancelAfterChecks{Context: context.Background(), remaining: 8}
	target := filepath.Join(t.TempDir(), "out")
	err := ExtractContext(ctx, archive, target)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Extract error = %v, want context canceled", err)
	}
	assertAbsent(t, target)
	assertNoStaging(t, target)
}

type cancelAfterChecks struct {
	context.Context
	remaining int
}

func (ctx *cancelAfterChecks) Err() error {
	ctx.remaining--
	if ctx.remaining <= 0 {
		return context.Canceled
	}
	return nil
}

func TestExtractRefusesExistingTarget(t *testing.T) {
	archive := writeArchive(t, []zipEntry{{name: "root/go.mod", data: "module example.com/test\n"}})
	target := filepath.Join(t.TempDir(), "out")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Extract(archive, target); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Extract error = %v, want existing-target rejection", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
		t.Fatalf("existing target changed: data %q, error %v", data, err)
	}
}

func TestPublishDoesNotReplaceTargetCreatedAfterInitialCheck(t *testing.T) {
	parent := t.TempDir()
	staging := filepath.Join(parent, "staging")
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := publishDirectoryNoReplace(staging, target); err == nil {
		t.Fatal("no-replace publication replaced an existing target")
	}
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("failed publication removed staging: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("failed publication removed target: %v", err)
	}
}

type zipEntry struct {
	name    string
	data    string
	dir     bool
	mode    os.FileMode
	store   bool
	nonUTF8 bool
}

func writeArchive(t *testing.T, entries []zipEntry) string {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "source.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		method := uint16(zip.Deflate)
		if entry.store {
			method = zip.Store
		}
		header := &zip.FileHeader{Name: entry.name, Method: method, NonUTF8: entry.nonUTF8}
		if entry.dir {
			header.Name = strings.TrimSuffix(header.Name, "/") + "/"
			header.SetMode(os.ModeDir | 0o755)
		} else if entry.mode != 0 {
			header.SetMode(entry.mode)
		} else {
			header.SetMode(0o644)
		}
		destination, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := destination.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archive
}

func mutateZipEntry(t *testing.T, archive, name string, mutate func(data []byte, central, local, directory int)) {
	t.Helper()
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	directory := bytes.Index(data, []byte("PK\x01\x02"))
	if directory < 0 {
		t.Fatal("ZIP central directory not found")
	}
	for central := directory; central+46 <= len(data) && binary.LittleEndian.Uint32(data[central:]) == zipDirectoryHeaderSignature; {
		nameBytes := int(binary.LittleEndian.Uint16(data[central+28:]))
		extraBytes := int(binary.LittleEndian.Uint16(data[central+30:]))
		commentBytes := int(binary.LittleEndian.Uint16(data[central+32:]))
		if central+46+nameBytes > len(data) {
			t.Fatal("truncated test ZIP central directory")
		}
		if string(data[central+46:central+46+nameBytes]) == name {
			local := int(binary.LittleEndian.Uint32(data[central+42:]))
			mutate(data, central, local, directory)
			if err := os.WriteFile(archive, data, 0o600); err != nil {
				t.Fatal(err)
			}
			return
		}
		central += 46 + nameBytes + extraBytes + commentBytes
	}
	t.Fatalf("ZIP entry %q not found", name)
}

func assertPreflightFailureBeforeMutation(t *testing.T, archive, want string) {
	t.Helper()
	parent := filepath.Join(t.TempDir(), "must-not-exist")
	target := filepath.Join(parent, "out")
	err := Extract(archive, target)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Extract error = %v, want containing %q", err, want)
	}
	assertAbsent(t, parent)
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("path %s exists after failed extraction: %v", path, err)
	}
}

func assertNoStaging(t *testing.T, target string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+"-extract-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("staging paths remain: %v", matches)
	}
}

func withEntries(limits extractionLimits, value int) extractionLimits {
	limits.entries = value
	return limits
}
func withFiles(limits extractionLimits, value int) extractionLimits {
	limits.files = value
	return limits
}
func withDirectories(limits extractionLimits, value int) extractionLimits {
	limits.directories = value
	return limits
}
func withMetadataBytes(limits extractionLimits, value uint64) extractionLimits {
	limits.metadataBytes = value
	return limits
}
func withPathBytes(limits extractionLimits, value int) extractionLimits {
	limits.pathBytes = value
	return limits
}
func withPathDepth(limits extractionLimits, value int) extractionLimits {
	limits.pathDepth = value
	return limits
}
func withComponentBytes(limits extractionLimits, value int) extractionLimits {
	limits.componentBytes = value
	return limits
}
