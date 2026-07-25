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
	write(t, "a.wast\truntime-regression\twast-json\nb.wast\tbulk-memory,simd\tdirect-go\n")
	fixtures, err := LoadManifest(path)
	if err != nil || len(fixtures) != 2 {
		t.Fatalf("valid manifest = %v, %v", fixtures, err)
	}
	for _, data := range []string{
		"../a.wast\truntime-regression\twast-json\n",
		"/a.wast\truntime-regression\twast-json\n",
		"b.wast\truntime-regression\twast-json\na.wast\truntime-regression\twast-json\n",
		"a.wast\tunknown\twast-json\n",
		"a.wast\tsimd,bulk-memory\twast-json\n",
		"a.wast\tsimd,simd\twast-json\n",
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
	digest := strings.Repeat("a", 64)
	valid := "tests/all/func.rs::typed_v128\tTestWasmtimePortV128TypedCallBoundaries\ttyped API adaptation\t" + digest + "\n"
	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	ports, err := LoadRustPorts(path)
	if err != nil || len(ports) != 1 || ports[0].Selector != "typed_v128" {
		t.Fatalf("valid Rust ledger = %v, %v", ports, err)
	}
	for _, data := range []string{
		"../func.rs::typed_v128\tTestWasmtimePortV128TypedCallBoundaries\tnote\t" + digest + "\n",
		"tests/all/func.rs::portable-*\tTestWasmtimePortV128TypedCallBoundaries\tnote\t" + digest + "\n",
		"tests/all/func.rs::typed_v128\tbad-test\tnote\t" + digest + "\n",
		"tests/all/func.rs::typed_v128\tTestWasmtimePortV128TypedCallBoundaries\tnote\tshort\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadRustPorts(path); err == nil {
			t.Fatalf("invalid Rust ledger was accepted: %q", data)
		}
	}
}

func TestLoadInventoryAndValidateCompleteness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "UPSTREAM_INVENTORY.tsv")
	valid := "a.wast\tported\tcovered\nb.wast\texcluded\tunsupported option\nc.wast\tout-of-scope\tproposal\n"
	if err := os.WriteFile(path, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := LoadInventory(path)
	if err != nil {
		t.Fatal(err)
	}
	fixtures := []Fixture{{Path: "a.wast", Coverage: "runtime-regression", Mode: ModeWASTJSON}}
	if err := ValidateInventory(entries, fixtures, []string{"a.wast", "b.wast", "c.wast"}); err != nil {
		t.Fatalf("valid inventory: %v", err)
	}
	for _, tc := range []struct {
		name     string
		data     string
		upstream []string
	}{
		{name: "unsafe", data: "../a.wast\tported\tnote\n", upstream: []string{"a.wast"}},
		{name: "unknown status", data: "a.wast\tmaybe\tnote\n", upstream: []string{"a.wast"}},
		{name: "unclassified upstream", data: "a.wast\tported\tnote\n", upstream: []string{"a.wast", "b.wast"}},
		{name: "stale entry", data: "a.wast\tported\tnote\nb.wast\texcluded\tnote\n", upstream: []string{"a.wast"}},
		{name: "manifest excluded", data: "a.wast\texcluded\tnote\n", upstream: []string{"a.wast"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.data), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := LoadInventory(path)
			if err == nil {
				err = ValidateInventory(got, fixtures, tc.upstream)
			}
			if err == nil {
				t.Fatal("invalid inventory was accepted")
			}
		})
	}
}

func TestRustFunctionSHA256IgnoresFunctionShapedCommentsAndLiterals(t *testing.T) {
	source := []byte("// fn target() {}\nconst S: &str = r#\"fn target() {}\"#;\nfn target() { let c = '{'; /* } */ }\n")
	got, err := RustFunctionSHA256(source, "target")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 64 {
		t.Fatalf("digest = %q", got)
	}
	if _, err := RustFunctionSHA256([]byte("// fn target() {}"), "target"); err == nil {
		t.Fatal("comment satisfied Rust function lookup")
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

func FuzzValidateRelativePathAndRustScannerDoNotPanic(f *testing.F) {
	for _, seed := range []string{"a.wast", "../a", "", "fn target() {}", `r#"fn target() {}"#`, "/* nested /* comment */ */ fn target() { '}' }"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		_ = ValidateRelativePath(input)
		_, _ = RustFunctionSHA256([]byte(input), "target")
	})
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
