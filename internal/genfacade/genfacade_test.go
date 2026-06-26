package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestFacadeUpToDate fails if the committed root wago.go does not match what the
// generator produces from src/wago — i.e. someone changed the public API (or
// wago.go) without running `go generate ./...`. It regenerates in memory and
// compares, so it never mutates the working tree.
func TestFacadeUpToDate(t *testing.T) {
	root := moduleRoot(t)
	want, err := generate(root)
	if err != nil {
		t.Fatalf("generate facade: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, outFile))
	if err != nil {
		t.Fatalf("read %s: %v", outFile, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s is out of date with src/wago's exported API; run `go generate ./...` and commit the result", outFile)
	}
}

// moduleRoot returns the repository root relative to this test file, which lives
// at <root>/internal/genfacade.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file location")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}
