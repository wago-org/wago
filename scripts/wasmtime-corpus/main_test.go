package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/wasmtimecorpus"
)

func TestAdaptSource(t *testing.T) {
	plain := []byte("(module)\n")
	got, err := adaptSource("add.wast", plain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("plain source changed: %q", got)
	}

	upstream := []byte(";; upstream\n(module $env\n)\n\n(module\n)\n")
	got, err = adaptSource("embenchen_fannkuch.wast", upstream)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(";; Wago port modification: register the named env provider explicitly so the standard WAST replay can resolve its imports.\n;; upstream\n(module $env\n)\n\n(register \"env\" $env)\n\n(module\n)\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("adapted source:\n%s\nwant:\n%s", got, want)
	}

	if _, err := adaptSource("embenchen_fannkuch.wast", got); err == nil {
		t.Fatal("already-adapted source was accepted as upstream")
	}
}

func TestNormalizeWABTJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "commands.json")
	source := "testdata/wasmtime/core/winch/use-innermost-frame/source.wast"
	malformed := `{"source_filename": "` + source + `",
 "commands": [
  {"type": "module", "line": 7, "filename": "commands.0.wasm"},
  {"type": "assert_trap", "line": 99, "action": {"type": "invoke", "field": "entry", "args": []}, "text": "unreachable", "expected": [{"type": "i32"}{"type": "i32"}]}]}
`
	if err := os.WriteFile(path, []byte(malformed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := normalizeWABTJSON("winch/use-innermost-frame.wast", source, path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) || !bytes.Contains(data, []byte(`"line":99`)) || !bytes.Contains(data, []byte(`"field":"entry"`)) || bytes.Contains(data, []byte(`"expected"`)) {
		t.Fatalf("normalized commands JSON = %s", data)
	}
	if err := normalizeWABTJSON("unknown.wast", source, path); err == nil {
		t.Fatal("unknown JSON normalization was accepted")
	}
	if err := normalizeWABTJSON("winch/use-innermost-frame.wast", source, path); err == nil {
		t.Fatal("already-valid normalized JSON was accepted as malformed input")
	}
}

func TestVerifyWABTVersionIsExact(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper")
	}
	tool := filepath.Join(t.TempDir(), "wast2json")
	writeVersion := func(version string) {
		t.Helper()
		if err := os.WriteFile(tool, []byte("#!/bin/sh\necho "+version+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeVersion("1.0.41")
	if err := verifyWABTVersion(tool, "1.0.41"); err != nil {
		t.Fatalf("exact version: %v", err)
	}
	writeVersion("1.0.410")
	if err := verifyWABTVersion(tool, "1.0.41"); err == nil {
		t.Fatal("substring version was accepted")
	}
}

func TestVerifyDirectFixtureRequiresSourceAndArtifactReview(t *testing.T) {
	dir := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("source.wast", "old source")
	write("module.0.wasm", "old artifact")
	sourceDigest, err := wasmtimecorpus.FileSHA256(filepath.Join(dir, "source.wast"))
	if err != nil {
		t.Fatal(err)
	}
	artifactDigest, err := wasmtimecorpus.DirectArtifactsSHA256(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry := wasmtimecorpus.DirectArtifact{SourceSHA256: sourceDigest, ArtifactsSHA256: artifactDigest}
	if err := verifyDirectFixture(dir, []byte("new upstream"), &entry, true); err == nil || !strings.Contains(err.Error(), "manually review") {
		t.Fatalf("unreviewed upstream change = %v", err)
	}

	write("source.wast", "new upstream")
	if err := verifyDirectFixture(dir, []byte("new upstream"), &entry, true); err == nil || !strings.Contains(err.Error(), "artifacts did not") {
		t.Fatalf("source-only change = %v", err)
	}
	write("module.0.wasm", "new artifact")
	if err := verifyDirectFixture(dir, []byte("new upstream"), &entry, true); err != nil {
		t.Fatalf("reviewed source/artifact change: %v", err)
	}
	newSource := sha256.Sum256([]byte("new upstream"))
	if entry.SourceSHA256 != hex.EncodeToString(newSource[:]) || entry.ArtifactsSHA256 == artifactDigest {
		t.Fatalf("updated direct ledger entry = %+v", entry)
	}
}

func TestCommitCorpusReplacesStagedTreeAndMetadata(t *testing.T) {
	root := t.TempDir()
	core := filepath.Join(root, "core")
	staged := filepath.Join(root, "stage", "core")
	if err := os.MkdirAll(core, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(core, "old"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "new"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Join(root, "meta")
	if err := os.WriteFile(metadata, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := commitCorpus(core, staged, map[string][]byte{metadata: []byte("new")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(core, "new")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(core, "old")); !os.IsNotExist(err) {
		t.Fatalf("old staged file survived: %v", err)
	}
	if data, err := os.ReadFile(metadata); err != nil || string(data) != "new" {
		t.Fatalf("metadata = %q, %v", data, err)
	}
}

func TestTreeDigestIsPathAndContentSensitive(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a"), []byte("bc"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := treeDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a"), []byte("bd"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := treeDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("tree digest ignored content change")
	}
	if err := os.Rename(filepath.Join(root, "a"), filepath.Join(root, "b")); err != nil {
		t.Fatal(err)
	}
	third, err := treeDigest(root)
	if err != nil {
		t.Fatal(err)
	}
	if second == third {
		t.Fatal("tree digest ignored path change")
	}
}
