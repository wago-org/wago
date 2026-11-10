package wagobench

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestCatalogGrainStdlibHostContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(corpusDir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"grain-stdlib-array": false, "grain-stdlib-string": false, "grain-stdlib-json-subset": false}
	empty := fmt.Sprintf("%x", sha256.Sum256(nil))
	for _, entry := range catalog.Benchmarks {
		if !strings.HasPrefix(entry.ID, "grain-stdlib-") {
			continue
		}
		seen, ok := want[entry.ID]
		if !ok || seen {
			t.Fatalf("unexpected or duplicate Grain fixture %q", entry.ID)
		}
		want[entry.ID] = true
		for _, tag := range []string{"tag:application", "tag:grain-stdlib"} {
			if !selectedByTag(tag, entry.Tags) {
				t.Fatalf("%s is missing from %s corpus selection", entry.ID, tag)
			}
		}
		c := entry.Command
		if c == nil || c.Runtime != "wasi" || c.Export != "_start" || c.Oracle != "self-check" || c.ReferenceRuntime != "" {
			t.Fatalf("%s changed its self-checking reference contract", entry.ID)
		}
		if c.Preopen != "" || c.ReadOnlyPreopen != "" || c.Stdin != "" || len(c.Args)+len(c.Inputs)+len(c.Outputs) != 0 {
			t.Fatalf("%s unexpectedly needs host inputs", entry.ID)
		}
		if c.StdoutSHA256 != empty || c.StderrSHA256 != empty {
			t.Fatalf("%s must enforce both empty success streams", entry.ID)
		}
		artifact, err := os.ReadFile(filepath.Join(corpusDir, entry.Artifact))
		if err != nil {
			t.Fatal(err)
		}
		module, err := wasm.DecodeModule(artifact)
		if err != nil {
			t.Fatal(err)
		}
		if len(module.Imports) != 1 || module.Imports[0].Module != "wasi_snapshot_preview1" || module.Imports[0].Name != "fd_write" || module.Imports[0].Type.Kind != wasm.ExternFunc {
			t.Fatalf("%s changed its fd_write-only host boundary", entry.ID)
		}
		if module.Start != nil || len(module.Tags) != 0 || len(module.Memories) != 1 {
			t.Fatalf("%s changed its command/memory/exception contract", entry.ID)
		}
	}
	for id, seen := range want {
		if !seen {
			t.Errorf("missing Grain fixture %s", id)
		}
	}
}

func TestCatalogGrainStdlibSourceBundle(t *testing.T) {
	root := filepath.Join(corpusDir, "workloads", "applications", "grain-stdlib")
	data, err := os.ReadFile(filepath.Join(root, "SOURCE_MANIFEST.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		ArchiveSHA256 string `json:"archive_sha256"`
		Files         []struct{ Path, SHA256 string }
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	want := make(map[string]string, len(manifest.Files))
	for _, file := range manifest.Files {
		if _, exists := want["grain/"+file.Path]; exists {
			t.Fatalf("duplicate source %s", file.Path)
		}
		want["grain/"+file.Path] = file.SHA256
	}
	// Three tests (JSON is a documented subset), 48 runtime/stdlib dependencies, and both upstream licenses.
	if len(want) != 53 {
		t.Fatalf("source inventory has %d files, want 53", len(want))
	}
	archive, err := os.ReadFile(filepath.Join(root, "source.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(archive)); got != manifest.ArchiveSHA256 {
		t.Fatalf("source archive SHA-256 = %s, want %s", got, manifest.ArchiveSHA256)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		digest, ok := want[header.Name]
		if !ok || header.Typeflag != tar.TypeReg || header.Size > 4<<20 {
			t.Fatalf("unexpected source archive entry %s", header.Name)
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, tr); err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", hash.Sum(nil)); got != digest {
			t.Fatalf("source %s SHA-256 = %s, want %s", header.Name, got, digest)
		}
		delete(want, header.Name)
	}
	if len(want) != 0 {
		t.Fatalf("source archive is missing %d declared files", len(want))
	}
}

func TestCatalogGrainStdlibCore2Profile(t *testing.T) {
	for _, name := range []string{"array", "string", "json-subset"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(corpusDir, "workloads", "applications", "grain-stdlib", name+".wasm"))
			if err != nil {
				t.Fatal(err)
			}
			// Windows uses this profile for CompileFull even when the WASI
			// command host is excluded by its execution platform allowlist.
			compiled, err := wago.Compile(wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV2), data)
			if err != nil {
				t.Fatalf("Core 2 compile: %v", err)
			}
			if err := compiled.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
