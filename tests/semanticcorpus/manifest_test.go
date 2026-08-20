//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package semanticcorpus

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadManifestRejectsMalformedDocuments locks the fail-closed manifest
// validation: malformed input must be rejected at load, never silently
// softened, matching the corpus's strict provenance discipline.
func TestLoadManifestRejectsMalformedDocuments(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "empty",
			content: `{}`,
		},
		{
			name: "unknown-field",
			content: `{"schema": 1, "modules": [{
				"id": "x/y", "artifact": "x/y.wasm",
				"artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"abi": "core", "source": {}, "invoke": {"export": "f", "args": [0]},
				"expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": [],
				"bogus": true
			}]}`,
		},
		{
			name: "bad-schema",
			content: `{"schema": 2, "modules": [{
				"id": "x/y", "artifact": "x/y.wasm",
				"artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"abi": "core", "source": {}, "invoke": {"export": "f", "args": [0]},
				"expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": []
			}]}`,
		},
		{
			name: "duplicate-id",
			content: `{"schema": 1, "modules": [
				{"id": "x/y", "artifact": "x/y.wasm",
				 "artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				 "abi": "core", "source": {}, "invoke": {"export": "f", "args": [0]},
				 "expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": []},
				{"id": "x/y", "artifact": "x/y.wasm",
				 "artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				 "abi": "core", "source": {}, "invoke": {"export": "f", "args": [0]},
				 "expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": []}
			]}`,
		},
		{
			name: "bad-artifact-digest",
			content: `{"schema": 1, "modules": [{
				"id": "x/y", "artifact": "x/y.wasm", "artifact_sha256": "zzzz",
				"abi": "core", "source": {}, "invoke": {"export": "f", "args": [0]},
				"expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": []
			}]}`,
		},
		{
			name: "no-oracle",
			content: `{"schema": 1, "modules": [{
				"id": "x/y", "artifact": "x/y.wasm",
				"artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"abi": "core", "source": {}, "invoke": {"export": "f", "args": [0]},
				"expect": {}, "limits": {"timeout_ms": 1}, "tags": []
			}]}`,
		},
		{
			name: "both-oracle-shapes",
			content: `{"schema": 1, "modules": [{
				"id": "x/y", "artifact": "x/y.wasm",
				"artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"abi": "core", "source": {},
				"invoke": {"export": "f", "args": [0], "vectors": {
					"input_offset": 0, "output_offset": 0, "output_len": 1,
					"cases": [{"len": 0, "out": "00"}]
				}},
				"expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": []
			}]}`,
		},
		{
			name: "bad-abi",
			content: `{"schema": 1, "modules": [{
				"id": "x/y", "artifact": "x/y.wasm",
				"artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"abi": "wasi-preview1", "source": {}, "invoke": {"export": "f", "args": [0]},
				"expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": []
			}]}`,
		},
		{
			name: "missing-source-provenance",
			content: `{"schema": 1, "modules": [{
				"id": "x/y", "artifact": "x/y.wasm",
				"artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"abi": "core", "source": {"repository": "https://example.com/x.git"},
				"invoke": {"export": "f", "args": [0]},
				"expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": []
			}]}`,
		},
		{
			name: "vector-output-length-mismatch",
			content: `{"schema": 1, "modules": [{
				"id": "x/y", "artifact": "x/y.wasm",
				"artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"abi": "core", "source": {},
				"invoke": {"export": "f", "vectors": {
					"input_offset": 0, "output_offset": 0, "output_len": 2,
					"cases": [{"len": 0, "out": "00"}]
				}},
				"expect": {}, "limits": {"timeout_ms": 1}, "tags": []
			}]}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := LoadManifest(write(tc.name+".json", tc.content)); err == nil {
				t.Fatalf("LoadManifest(%s) succeeded, want error", tc.name)
			}
		})
	}
}

func TestLoadManifestRejectsTrailingContent(t *testing.T) {
	manifest, err := os.ReadFile(ManifestPath())
	if err != nil {
		t.Fatal(err)
	}

	for _, suffix := range []struct {
		name string
		data string
	}{
		{name: "second-json-value", data: "\n{}\n"},
		{name: "trailing-junk", data: "\nnot-json\n"},
	} {
		t.Run(suffix.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "MANIFEST.json")
			data := append(append([]byte(nil), manifest...), suffix.data...)
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadManifest(path); err == nil {
				t.Fatal("LoadManifest succeeded, want trailing-content error")
			}
		})
	}
}
