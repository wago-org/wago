package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultModulePath(t *testing.T) {
	t.Setenv("WAGO_JSON_MODULE", "")
	// The command is documented to run from bench/. Package tests run two
	// directories below it, so resolve the returned path from that location.
	f, err := os.Open(filepath.Join("..", "..", modulePath()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var magic [4]byte
	if _, err := f.Read(magic[:]); err != nil {
		t.Fatal(err)
	}
	if magic != [4]byte{0, 'a', 's', 'm'} {
		t.Fatalf("default workload is not WebAssembly: %x", magic)
	}
}

func TestModulePathOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom.wasm")
	t.Setenv("WAGO_JSON_MODULE", want)
	if got := modulePath(); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func BenchmarkDefaultModulePath(b *testing.B) {
	b.Setenv("WAGO_JSON_MODULE", "")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if modulePath() == "" {
			b.Fatal("empty path")
		}
	}
}
