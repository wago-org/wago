package main

import (
	"os"
	"strings"
	"testing"
)

func TestUsageDocumentsRunOnlyCommandSurface(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "usage-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	usage(f)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{
		"wago run [-e name] <file> [args...]",
		"wago compile                               not implemented",
		"wago profile                               not implemented",
		"wago validate                              not implemented",
		"a <file> must be raw .wasm",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("usage text missing %q:\n%s", want, text)
		}
	}
}
