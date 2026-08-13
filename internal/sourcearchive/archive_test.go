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
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate, NonUTF8: entry.nonUTF8}
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
