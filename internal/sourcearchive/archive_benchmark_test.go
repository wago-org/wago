package sourcearchive

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkPreflightDeepPathsProduction(b *testing.B) {
	limits := productionExtractionLimits()
	archive := writeProductionPathArchive(b, limits.files)
	file, err := os.Open(archive)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = file.Close() })
	info, err := file.Stat()
	if err != nil {
		b.Fatal(err)
	}
	directory, err := locateZipDirectory(file, info.Size(), limits)
	if err != nil {
		b.Fatal(err)
	}
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		entries, err := preflight(context.Background(), reader.File, directory.offset, limits)
		if err != nil {
			b.Fatal(err)
		}
		if len(entries) != limits.files {
			b.Fatalf("preflight entries = %d, want %d", len(entries), limits.files)
		}
	}
}

func writeProductionPathArchive(tb testing.TB, files int) string {
	tb.Helper()
	directories := make([]string, 63)
	for index := range directories {
		directories[index] = fmt.Sprintf("D%013d", index)
	}
	deepPrefix := "ROOT/" + strings.Join(directories, "/") + "/"
	archive := filepath.Join(tb.TempDir(), "production-paths.zip")
	file, err := os.Create(archive)
	if err != nil {
		tb.Fatal(err)
	}
	writer := zip.NewWriter(file)
	writeEmpty := func(name string) {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetMode(0o644)
		if _, err := writer.CreateHeader(header); err != nil {
			tb.Fatal(err)
		}
	}
	writeEmpty("ROOT/go.mod")
	for index := 1; index < files; index++ {
		writeEmpty(deepPrefix + fmt.Sprintf("F%012d", index))
	}
	if err := writer.Close(); err != nil {
		tb.Fatal(err)
	}
	if err := file.Close(); err != nil {
		tb.Fatal(err)
	}
	return archive
}
