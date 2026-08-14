package shared

import (
	"os"
	"testing"
)

func TestNativeSizeReportSeparatesRawAndMappedBytes(t *testing.T) {
	page := os.Getpagesize()
	var projected NativeSizeReport
	projected.SetExecutableMapping(page+1, 0)
	if projected.ExecutableMappingBytes != 2*page || projected.ExecutableMappingPages != 2 {
		t.Fatalf("projected mapping = %d bytes / %d pages, want %d / 2", projected.ExecutableMappingBytes, projected.ExecutableMappingPages, 2*page)
	}
	var actual NativeSizeReport
	actual.SetExecutableMapping(1, 4*page)
	if actual.ExecutableMappingBytes != page || actual.ExecutableMappingPages != 1 || actual.CompilerCodeArenaBytes != 4*page {
		t.Fatalf("actual mapping = %d bytes / %d pages with %d-byte arena, want %d / 1 with %d", actual.ExecutableMappingBytes, actual.ExecutableMappingPages, actual.CompilerCodeArenaBytes, page, 4*page)
	}
}

func TestAdapterShapeHashNormalizesRelocation(t *testing.T) {
	a := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	b := append([]byte(nil), a...)
	copy(b[2:6], []byte{9, 10, 11, 12})
	if got, want := AdapterShapeHash(b, 2, 4), AdapterShapeHash(a, 2, 4); got != want {
		t.Fatalf("relocation changed shape hash: got %#x, want %#x", got, want)
	}
	b[0]++
	if AdapterShapeHash(b, 2, 4) == AdapterShapeHash(a, 2, 4) {
		t.Fatal("non-relocation byte did not change shape hash")
	}
}
