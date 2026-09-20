package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultModulePath(t *testing.T) {
	for _, unset := range []bool{true, false} {
		name := "empty"
		if unset {
			name = "unset"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("WAGO_JSON_MODULE", "")
			if unset {
				if err := os.Unsetenv("WAGO_JSON_MODULE"); err != nil {
					t.Fatal(err)
				}
			}
			// The command is documented to run from bench/. Package tests run two
			// directories below it, so resolve the returned path from that location.
			f, err := os.Open(filepath.Join("..", "..", modulePath()))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			var magic [4]byte
			if _, err := io.ReadFull(f, magic[:]); err != nil {
				t.Fatal(err)
			}
			if magic != [4]byte{0, 'a', 's', 'm'} {
				t.Fatalf("default workload is not WebAssembly: %x", magic)
			}
			if got, want := modulePath(), "../corpus/workloads/assemblyscript/json-as.wasm"; got != want {
				t.Fatalf("path = %q, want %q", got, want)
			}
		})
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
