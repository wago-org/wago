//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package semanticcorpus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
			content: `{"schema": 1, "checks": [{
				"id": "x/y", "artifact": "x/y.wasm",
				"artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"abi": "core", "source": {"repository":"repo", "revision":"rev", "revision_date":"date", "license":"MIT", "toolchain":"sdk", "toolchain_version":"34"}, "invoke": {"export": "f", "args": [0]},
				"expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": [],
				"bogus": true
			}]}`,
		},
		{
			name: "bad-schema",
			content: `{"schema": 2, "checks": [{
				"id": "x/y", "artifact": "x/y.wasm",
				"artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"abi": "core", "source": {"repository":"repo", "revision":"rev", "revision_date":"date", "license":"MIT", "toolchain":"sdk", "toolchain_version":"34"}, "invoke": {"export": "f", "args": [0]},
				"expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": []
			}]}`,
		},
		{
			name: "duplicate-id",
			content: `{"schema": 1, "checks": [
				{"id": "x/y", "artifact": "x/y.wasm",
				 "artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				 "abi": "core", "source": {"repository":"repo", "revision":"rev", "revision_date":"date", "license":"MIT", "toolchain":"sdk", "toolchain_version":"34"}, "invoke": {"export": "f", "args": [0]},
				 "expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": []},
				{"id": "x/y", "artifact": "x/y.wasm",
				 "artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				 "abi": "core", "source": {"repository":"repo", "revision":"rev", "revision_date":"date", "license":"MIT", "toolchain":"sdk", "toolchain_version":"34"}, "invoke": {"export": "f", "args": [0]},
				 "expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": []}
			]}`,
		},
		{
			name: "bad-artifact-digest",
			content: `{"schema": 1, "checks": [{
				"id": "x/y", "artifact": "x/y.wasm", "artifact_sha256": "zzzz",
				"abi": "core", "source": {"repository":"repo", "revision":"rev", "revision_date":"date", "license":"MIT", "toolchain":"sdk", "toolchain_version":"34"}, "invoke": {"export": "f", "args": [0]},
				"expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": []
			}]}`,
		},
		{
			name: "no-oracle",
			content: `{"schema": 1, "checks": [{
				"id": "x/y", "artifact": "x/y.wasm",
				"artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"abi": "core", "source": {"repository":"repo", "revision":"rev", "revision_date":"date", "license":"MIT", "toolchain":"sdk", "toolchain_version":"34"}, "invoke": {"export": "f", "args": [0]},
				"expect": {}, "limits": {"timeout_ms": 1}, "tags": []
			}]}`,
		},
		{
			name: "both-oracle-shapes",
			content: `{"schema": 1, "checks": [{
				"id": "x/y", "artifact": "x/y.wasm",
				"artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"abi": "core", "source": {"repository":"repo", "revision":"rev", "revision_date":"date", "license":"MIT", "toolchain":"sdk", "toolchain_version":"34"},
				"invoke": {"export": "f", "args": [0], "vectors": {
					"input_offset": 0, "output_offset": 0, "output_len": 1,
					"cases": [{"len": 0, "out": "00"}]
				}},
				"expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": []
			}]}`,
		},
		{
			name: "bad-abi",
			content: `{"schema": 1, "checks": [{
				"id": "x/y", "artifact": "x/y.wasm",
				"artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"abi": "wasi-preview1", "source": {"repository":"repo", "revision":"rev", "revision_date":"date", "license":"MIT", "toolchain":"sdk", "toolchain_version":"34"}, "invoke": {"export": "f", "args": [0]},
				"expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": []
			}]}`,
		},
		{
			name: "missing-source-provenance",
			content: `{"schema": 1, "checks": [{
				"id": "x/y", "artifact": "x/y.wasm",
				"artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"abi": "core", "source": {"repository": "https://example.com/x.git"},
				"invoke": {"export": "f", "args": [0]},
				"expect": {"return": ["0x0"]}, "limits": {"timeout_ms": 1}, "tags": []
			}]}`,
		},
		{
			name: "vector-output-length-mismatch",
			content: `{"schema": 1, "checks": [{
				"id": "x/y", "artifact": "x/y.wasm",
				"artifact_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"abi": "core", "source": {"repository":"repo", "revision":"rev", "revision_date":"date", "license":"MIT", "toolchain":"sdk", "toolchain_version":"34"},
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

func TestLoadManifestContractMutations(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		edit       func(*Manifest)
	}{
		{"valid", "", func(*Manifest) {}},
		{"duplicate-id", "duplicate module id", func(m *Manifest) { m.Modules = append(m.Modules, m.Modules[0]) }},
		{"empty-id", "empty id", func(m *Manifest) { m.Modules[0].ID = "" }},
		{"no-oracle", "exactly one oracle", func(m *Manifest) { m.Modules[0].Expect = Expect{} }},
		{"repository", "source provenance", func(m *Manifest) { m.Modules[0].Source.Repository = "" }},
		{"revision", "source provenance", func(m *Manifest) { m.Modules[0].Source.Revision = "" }},
		{"date", "source provenance", func(m *Manifest) { m.Modules[0].Source.RevisionDate = "" }},
		{"license", "source provenance", func(m *Manifest) { m.Modules[0].Source.License = "" }},
		{"toolchain", "source toolchain", func(m *Manifest) { m.Modules[0].Source.Toolchain = "" }},
		{"version", "source toolchain", func(m *Manifest) { m.Modules[0].Source.ToolchainVersion = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Manifest{Schema: 1, Modules: []Module{{ID: "example", Artifact: "example.wasm", ArtifactSHA256: strings.Repeat("a", 64), ABI: "core",
				Source: Source{Repository: "https://example.com", Revision: "revision", RevisionDate: "2026-01-01", License: "MIT", Toolchain: "WASI SDK", ToolchainVersion: "34"},
				Invoke: Invoke{Export: "run"}, Expect: Expect{Return: []string{"0x1"}}, Limits: Limits{TimeoutMS: 1000}}}}
			tc.edit(&m)
			data, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "catalog.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			_, err = LoadManifest(path)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
}
