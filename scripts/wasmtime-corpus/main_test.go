package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
	if err := normalizeWABTJSON("winch/use-innermost-frame.wast", "testdata/wasmtime/core/winch/use-innermost-frame/source.wast", path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) || !bytes.Contains(data, []byte(`"text":"unreachable"`)) || bytes.Contains(data, []byte(`"expected"`)) {
		t.Fatalf("normalized commands JSON = %s", data)
	}
	if err := normalizeWABTJSON("unknown.wast", "source.wast", path); err == nil {
		t.Fatal("unknown JSON normalization was accepted")
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
