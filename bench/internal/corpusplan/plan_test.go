package corpusplan

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGroupsAreDeterministicAndComplete(t *testing.T) {
	workloads := []Workload{
		{ID: "large-a", ArtifactSize: 100, Stages: []string{"Decode", "Compile"}},
		{ID: "large-b", ArtifactSize: 90, Stages: []string{"Decode", "Compile"}},
		{ID: "medium", ArtifactSize: 40, Stages: []string{"Decode", "Compile"}},
		{ID: "semantic", ArtifactSize: 4, Stages: []string{"Exec"}, SemanticCount: 1},
		{ID: "small", ArtifactSize: 2, Stages: []string{"Decode"}},
	}
	first, err := Groups(workloads, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Groups(workloads, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("shard plan changed between runs: %v != %v", first, second)
	}
	seen := make(map[string]int, len(workloads))
	loads := make([]int64, len(first))
	maxWeight := int64(0)
	for _, workload := range workloads {
		if workload.Weight() > maxWeight {
			maxWeight = workload.Weight()
		}
	}
	for shard, group := range first {
		for _, workload := range group {
			seen[workload.ID]++
			loads[shard] += workload.Weight()
		}
	}
	minLoad, maxLoad := loads[0], loads[0]
	for _, load := range loads[1:] {
		if load < minLoad {
			minLoad = load
		}
		if load > maxLoad {
			maxLoad = load
		}
	}
	if maxLoad-minLoad > maxWeight {
		t.Fatalf("shard load difference %d exceeds largest workload %d: %v", maxLoad-minLoad, maxWeight, loads)
	}
	for _, workload := range workloads {
		if seen[workload.ID] != 1 {
			t.Errorf("workload %q appears %d times", workload.ID, seen[workload.ID])
		}
	}
}

func TestWorkUnitsCountStagesAndSemanticRepetitions(t *testing.T) {
	if got := (Workload{ExecCount: 2}).Units(); got != 7 {
		t.Fatalf("default stage units = %d, want 7", got)
	}
	if got := (Workload{Stages: []string{"CommandExec"}}).Units(); got != 0 {
		t.Fatalf("command-only stage units = %d, want 0", got)
	}
	if got := (Workload{Stages: []string{"Decode", "Exec"}, ExecCount: 3, SemanticCount: 2}).Units(); got != 84 {
		t.Fatalf("explicit stage and semantic units = %d, want 84", got)
	}
}

func TestLoadCatalogFiltersCommandOnlyAndVerifiesDigests(t *testing.T) {
	root := t.TempDir()
	corpusDir := filepath.Join(root, "corpus")
	artifactDir := corpusDir
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entries := []map[string]any{
		catalogEntry(t, artifactDir, "direct", "direct.wasm", []byte("direct"), nil, []any{map[string]any{"export": "run"}}, nil),
		catalogEntry(t, artifactDir, "semantic", "semantic.wasm", []byte("semantic"), []string{"Exec"}, nil, []string{"semantic-case"}),
		catalogEntry(t, artifactDir, "command", "command.wasm", []byte("command"), []string{"CommandExec"}, nil, nil),
	}
	data, err := json.Marshal(map[string]any{"schema": 1, "benchmarks": entries})
	if err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(corpusDir, "catalog.json")
	if err := os.WriteFile(catalogPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	workloads, err := LoadCatalog(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := IDs(workloads), []string{"direct", "semantic"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("correctness workload ids = %v, want %v", got, want)
	}

	entries[0]["artifact_sha256"] = strings.Repeat("0", 64)
	data, err = json.Marshal(map[string]any{"schema": 1, "benchmarks": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCatalog(catalogPath); err == nil || !strings.Contains(err.Error(), "artifact SHA-256") {
		t.Fatalf("bad digest error = %v", err)
	}
}

func TestParseShard(t *testing.T) {
	index, count, err := ParseShard("2/4")
	if err != nil || index != 2 || count != 4 {
		t.Fatalf("parsed shard = %d/%d, %v", index, count, err)
	}
	for _, value := range []string{"", "1", "x/4", "4/4", "0/0", "0/4/2"} {
		if _, _, err := ParseShard(value); err == nil {
			t.Errorf("invalid shard %q was accepted", value)
		}
	}
}

func catalogEntry(tb testing.TB, dir, id, artifact string, data []byte, stages []string, exec []any, semantic []string) map[string]any {
	tb.Helper()
	if err := os.WriteFile(filepath.Join(dir, artifact), data, 0o644); err != nil {
		tb.Fatal(err)
	}
	return map[string]any{
		"id":              id,
		"artifact":        artifact,
		"artifact_sha256": fmt.Sprintf("%x", sha256.Sum256(data)),
		"stages":          stages,
		"exec":            exec,
		"semantic_exec":   semantic,
	}
}
