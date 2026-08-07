package wasmtimecore3

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCorpus(t *testing.T) {
	root := filepath.Join("..", "regressions", "wasmtime-core3")
	fixtures, err := LoadManifest(filepath.Join(root, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := LoadInventory(filepath.Join(root, "UPSTREAM_INVENTORY.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	statusCounts := map[string]int{}
	inventoryStatus := map[string]string{}
	for _, row := range inventory {
		statusCounts[row.Status]++
		inventoryStatus[row.Path] = row.Status
	}
	if len(inventory) != 369 || statusCounts["ported-runtime"] != 104 || statusCounts["ported-core3"] != 103 || statusCounts["ported-adapted"] != 5 || statusCounts["outside-core3"] != 155 || statusCounts["excluded-nonstandard"] != 2 {
		t.Fatalf("inventory totals = %d, statuses %v", len(inventory), statusCounts)
	}
	if len(fixtures) != 103 {
		t.Fatalf("fixture count = %d, want 103", len(fixtures))
	}
	provenance, err := LoadProvenance(filepath.Join(root, "PROVENANCE.json"))
	if err != nil {
		t.Fatal(err)
	}
	if provenance.UpstreamRepo != "https://github.com/bytecodealliance/wasmtime" || provenance.SourceRoot != "tests/misc_testsuite" || provenance.InterpreterRepo != "https://github.com/WebAssembly/spec" || provenance.Converter != "scripts/spec-interpreter-json.py" || provenance.AlternateConverter != "scripts/wasm-tools-wast-json.py" || provenance.WasmToolsVersion != WasmToolsVersion {
		t.Fatalf("unexpected provenance origin: %+v", provenance)
	}
	reusePath := filepath.Join(root, "RUNTIME_REUSE.tsv")
	reuseDigest, err := FileSHA256(reusePath)
	if err != nil {
		t.Fatal(err)
	}
	if reuseDigest != provenance.RuntimeReuseSHA256 {
		t.Fatalf("runtime reuse digest = %s, want %s", reuseDigest, provenance.RuntimeReuseSHA256)
	}
	reused, err := LoadRuntimeReuse(reusePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reused) != 104 {
		t.Fatalf("runtime reuse rows = %d, want 104", len(reused))
	}
	reuseSeen := make(map[string]bool, len(reused))
	relationCounts := map[string]int{}
	for _, row := range reused {
		if inventoryStatus[row.Path] != "ported-runtime" || reuseSeen[row.Path] {
			t.Fatalf("invalid runtime reuse row %q", row.Path)
		}
		reuseSeen[row.Path] = true
		relationCounts[row.Relation]++
		sourcePath := filepath.Join("..", "regressions", "runtime", "core", filepath.FromSlash(strings.TrimSuffix(row.Path, ".wast")), "source.wast")
		runtimeDigest, err := FileSHA256(sourcePath)
		if err != nil {
			t.Fatalf("runtime reuse source %s: %v", row.Path, err)
		}
		if runtimeDigest != row.RuntimeSourceSHA256 {
			t.Fatalf("runtime reuse source %s digest = %s, want %s", row.Path, runtimeDigest, row.RuntimeSourceSHA256)
		}
		switch row.Relation {
		case "byte-identical":
			if row.PinnedSourceSHA256 != row.RuntimeSourceSHA256 {
				t.Fatalf("runtime reuse %s claims byte identity with differing hashes", row.Path)
			}
		case "diagnostic-text-only":
			if row.Path != "no-panic-on-invalid.wast" || row.PinnedSourceSHA256 == row.RuntimeSourceSHA256 {
				t.Fatalf("unexpected diagnostic-only runtime reuse row %+v", row)
			}
		}
	}
	if relationCounts["byte-identical"] != 103 || relationCounts["diagnostic-text-only"] != 1 {
		t.Fatalf("runtime reuse relations = %v", relationCounts)
	}
	for path, status := range inventoryStatus {
		if status == "ported-runtime" && !reuseSeen[path] {
			t.Fatalf("ported-runtime path %s has no reuse proof", path)
		}
	}
	digest, err := TreeSHA256(filepath.Join(root, "core"))
	if err != nil {
		t.Fatal(err)
	}
	if digest != provenance.FixtureTreeSHA256 {
		t.Fatalf("fixture tree digest = %s, want %s", digest, provenance.FixtureTreeSHA256)
	}
	adaptedDigest, err := TreeSHA256(filepath.Join(root, "adapted"))
	if err != nil {
		t.Fatal(err)
	}
	if adaptedDigest != provenance.AdaptedTreeSHA256 {
		t.Fatalf("adapted tree digest = %s, want %s", adaptedDigest, provenance.AdaptedTreeSHA256)
	}
	var modules, assertions int
	seen := make(map[string]bool, len(fixtures))
	for _, fixture := range fixtures {
		seen[fixture.Path] = true
		if inventoryStatus[fixture.Path] != "ported-core3" {
			t.Errorf("%s inventory status = %q, want ported-core3", fixture.Path, inventoryStatus[fixture.Path])
		}
		dir := filepath.Join(root, "core", fixture.Path[:len(fixture.Path)-len(".wast")])
		raw, err := os.ReadFile(filepath.Join(dir, "source.wast"))
		if err != nil || len(raw) == 0 {
			t.Fatalf("%s source: size %d, err %v", fixture.Path, len(raw), err)
		}
		m, a, err := ValidateFixture(dir)
		if err != nil {
			t.Fatalf("%s: %v", fixture.Path, err)
		}
		modules += m
		assertions += a
	}
	if err := filepath.WalkDir(filepath.Join(root, "core"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "source.wast" {
			return err
		}
		rel, err := filepath.Rel(filepath.Join(root, "core"), filepath.Dir(path))
		if err != nil {
			return err
		}
		manifestPath := filepath.ToSlash(rel) + ".wast"
		if !seen[manifestPath] {
			t.Errorf("orphan fixture %q", manifestPath)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if modules != 215 || assertions != 690 {
		t.Fatalf("corpus totals = %d modules, %d assertions; want 215, 690", modules, assertions)
	}
	adaptations, err := os.Open(filepath.Join(root, "ADAPTATIONS.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer adaptations.Close()
	adapted := map[string]bool{}
	equivalentCounts := map[string][2]int{
		"big-memory-behavior.wast":    {1, 6},
		"memory-combos.wast":          {1, 64},
		"memory64/more-than-4gb.wast": {8, 7},
		"memory64/table-too-big.wast": {1, 2},
		"memory_fill.wast":            {1, 26},
	}
	scanner := bufio.NewScanner(adaptations)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 || inventoryStatus[fields[0]] != "ported-adapted" || fields[1] == "" || fields[2] != "equivalent" || fields[3] == "" {
			t.Fatalf("malformed adaptation %q", line)
		}
		adapted[fields[0]] = true
		adaptedDir := filepath.Join(root, "adapted", strings.TrimSuffix(fields[0], ".wast"))
		if raw, err := os.ReadFile(filepath.Join(adaptedDir, "source.wast")); err != nil || len(raw) == 0 {
			t.Fatalf("adapted source %s: size %d, err %v", fields[0], len(raw), err)
		}
		if want, ok := equivalentCounts[fields[0]]; ok {
			modules, assertions, err := ValidateFixture(filepath.Join(adaptedDir, "equivalent"))
			if err != nil {
				t.Fatalf("adapted equivalent %s: %v", fields[0], err)
			}
			if modules != want[0] || assertions != want[1] {
				t.Fatalf("adapted equivalent %s = %d modules/%d assertions, want %d/%d", fields[0], modules, assertions, want[0], want[1])
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(adapted) != 5 {
		t.Fatalf("adaptation count = %d, want 5", len(adapted))
	}
}
