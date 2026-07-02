package main

import (
	"os"
	"strings"
	"testing"
)

func TestUsageDocumentsCommandSurface(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "usage-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	usage(f)
	f.Close()
	b, _ := os.ReadFile(f.Name())
	text := string(b)
	for _, want := range []string{
		"run <file> [args...]",
		"build                     not implemented",
		"validate <file>           decode and validate a module",
		"override per-arg with a suffix",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("usage text missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "test") {
		t.Fatalf("usage should no longer mention test:\n%s", text)
	}
}

func TestValidateModuleBytesAcceptsEmptyModule(t *testing.T) {
	// Magic + version is a valid empty WebAssembly module.
	mod := []byte{'\x00', 'a', 's', 'm', 0x01, 0x00, 0x00, 0x00}
	if err := validateModuleBytes(mod); err != nil {
		t.Fatalf("validateModuleBytes(empty module): %v", err)
	}
}

func TestValidateModuleBytesRejectsDecodeErrors(t *testing.T) {
	badMagic := []byte{'n', 'o', 'p', 'e', 0x01, 0x00, 0x00, 0x00}
	err := validateModuleBytes(badMagic)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("validateModuleBytes(bad magic) = %v, want decode error", err)
	}
}

func TestUsageDoesNotAdvertiseValidateDirect(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "usage-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	usage(f)
	f.Close()
	b, _ := os.ReadFile(f.Name())
	removedAlias := "validate" + "-direct"
	if strings.Contains(string(b), removedAlias) {
		t.Fatalf("usage should not mention removed validate alias:\n%s", b)
	}
}
