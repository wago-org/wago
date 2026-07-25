package wasmtimecorpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProvenanceStrict(t *testing.T) {
	valid := `{
  "upstream_repo":"https://example.com/upstream.git",
  "revision":"0123456789abcdef0123456789abcdef01234567",
  "revision_date":"2026-07-24",
  "source_root":"tests/corpus",
  "wabt_repo":"https://example.com/wabt.git",
  "wabt_revision":"89abcdef0123456789abcdef0123456789abcdef",
  "wabt_version":"1.0.41",
  "legacy_core_source_filenames":[],
  "normalized_wabt_json_fixtures":[],
  "fixture_tree_sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}`
	path := filepath.Join(t.TempDir(), "provenance.json")
	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProvenance(path); err != nil {
		t.Fatalf("valid provenance: %v", err)
	}
	for _, mutation := range []struct {
		name string
		data string
	}{
		{name: "unknown field", data: strings.Replace(valid, "\n}", ",\n  \"typo\":true\n}", 1)},
		{name: "trailing JSON", data: valid + `{}`},
		{name: "unsafe root", data: strings.Replace(valid, `"tests/corpus"`, `"../corpus"`, 1)},
		{name: "short revision", data: strings.Replace(valid, `"0123456789abcdef0123456789abcdef01234567"`, `"0123"`, 1)},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(mutation.data), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadProvenance(path); err == nil {
				t.Fatal("invalid provenance was accepted")
			}
		})
	}
}

func TestLoadManifestRejectsUnsafeOrUnsortedRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "MANIFEST.tsv")
	write := func(t *testing.T, data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(t, "a.wast\truntime-regression\twast-json\nb.wast\tsimd\tdirect-go\n")
	fixtures, err := LoadManifest(path)
	if err != nil || len(fixtures) != 2 {
		t.Fatalf("valid manifest = %v, %v", fixtures, err)
	}
	for _, data := range []string{
		"../a.wast\truntime-regression\twast-json\n",
		"/a.wast\truntime-regression\twast-json\n",
		"b.wast\truntime-regression\twast-json\na.wast\truntime-regression\twast-json\n",
		"a.wast\tunknown\twast-json\n",
		"a.wast\truntime-regression\tunknown\n",
	} {
		write(t, data)
		if _, err := LoadManifest(path); err == nil {
			t.Fatalf("invalid manifest was accepted: %q", data)
		}
	}
}

func TestLoadRustPortsRequiresExactSafeFunctionScopes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "RUST_PORTS.tsv")
	valid := "tests/all/func.rs::typed_v128\tTestWasmtimePortV128TypedCallBoundaries\ttyped API adaptation\n"
	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	ports, err := LoadRustPorts(path)
	if err != nil || len(ports) != 1 || ports[0].Selector != "typed_v128" {
		t.Fatalf("valid Rust ledger = %v, %v", ports, err)
	}
	for _, data := range []string{
		"../func.rs::typed_v128\tTestWasmtimePortV128TypedCallBoundaries\tnote\n",
		"tests/all/func.rs::portable-*\tTestWasmtimePortV128TypedCallBoundaries\tnote\n",
		"tests/all/func.rs::typed_v128\tbad-test\tnote\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadRustPorts(path); err == nil {
			t.Fatalf("invalid Rust ledger was accepted: %q", data)
		}
	}
}

func TestLoadDirectArtifactsRejectsUnsafeOrMalformedRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "DIRECT_ARTIFACTS.tsv")
	digest := strings.Repeat("a", 64)
	valid := "a.wast\t" + digest + "\t" + digest + "\treviewed artifact\n"
	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := LoadDirectArtifacts(path)
	if err != nil || len(entries) != 1 {
		t.Fatalf("valid direct ledger = %v, %v", entries, err)
	}
	for _, data := range []string{
		"../a.wast\t" + digest + "\t" + digest + "\tnote\n",
		"a.wast\tshort\t" + digest + "\tnote\n",
		"b.wast\t" + digest + "\t" + digest + "\tnote\na.wast\t" + digest + "\t" + digest + "\tnote\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadDirectArtifacts(path); err == nil {
			t.Fatalf("invalid direct ledger was accepted: %q", data)
		}
	}
}

func TestValidateRelativePath(t *testing.T) {
	for _, valid := range []string{"a.wast", "winch/a.wast", "tests/all/func.rs"} {
		if err := ValidateRelativePath(valid); err != nil {
			t.Errorf("%q: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", ".", "..", "../a", "a/../b", "/a", `a\b`, "C:/a"} {
		if err := ValidateRelativePath(invalid); err == nil {
			t.Errorf("unsafe path %q was accepted", invalid)
		}
	}
}
