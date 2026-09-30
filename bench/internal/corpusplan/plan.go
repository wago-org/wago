// Package corpusplan splits the correctness corpus into deterministic CI shards.
package corpusplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const SemanticOracleRepetitions = 40

type Workload struct {
	ID            string
	Artifact      string
	ArtifactSize  int64
	Stages        []string
	ExecCount     int
	SemanticCount int
}

type Report struct {
	Schema     int      `json:"schema"`
	SourceSHA  string   `json:"source_sha"`
	Platform   string   `json:"platform"`
	Shard      int      `json:"shard"`
	ShardCount int      `json:"shard_count"`
	Selector   string   `json:"selector"`
	Workloads  []string `json:"workloads"`
	Passed     bool     `json:"passed"`
}

type catalog struct {
	Schema     int       `json:"schema"`
	Benchmarks []benchID `json:"benchmarks"`
}

type benchID struct {
	ID           string   `json:"id"`
	Artifact     string   `json:"artifact"`
	Digest       string   `json:"artifact_sha256"`
	Stages       []string `json:"stages"`
	Exec         []any    `json:"exec"`
	SemanticExec []string `json:"semantic_exec"`
}

// LoadCatalog returns the entries exercised by TestCorpus or
// TestCorpusSemanticExec. Command-only entries belong to the separate
// application corpus suite.
func LoadCatalog(catalogPath string) ([]Workload, error) {
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return nil, fmt.Errorf("read corpus catalog: %w", err)
	}
	var source catalog
	if err := json.Unmarshal(data, &source); err != nil {
		return nil, fmt.Errorf("decode corpus catalog: %w", err)
	}
	if source.Schema != 1 {
		return nil, fmt.Errorf("corpus catalog schema = %d, want 1", source.Schema)
	}

	seen := make(map[string]bool, len(source.Benchmarks))
	workloads := make([]Workload, 0, len(source.Benchmarks))
	root := filepath.Dir(catalogPath)
	for _, entry := range source.Benchmarks {
		if entry.ID == "" || strings.Contains(entry.ID, ",") {
			return nil, fmt.Errorf("invalid corpus id %q", entry.ID)
		}
		if seen[entry.ID] {
			return nil, fmt.Errorf("duplicate corpus id %q", entry.ID)
		}
		seen[entry.ID] = true
		if entry.Artifact == "" || filepath.ToSlash(filepath.Clean(filepath.FromSlash(entry.Artifact))) != entry.Artifact || !filepath.IsLocal(filepath.FromSlash(entry.Artifact)) {
			return nil, fmt.Errorf("%s: invalid artifact path %q", entry.ID, entry.Artifact)
		}
		if len(entry.Digest) != sha256.Size*2 {
			return nil, fmt.Errorf("%s: invalid artifact SHA-256 %q", entry.ID, entry.Digest)
		}
		if _, err := hex.DecodeString(entry.Digest); err != nil {
			return nil, fmt.Errorf("%s: invalid artifact SHA-256: %w", entry.ID, err)
		}
		artifact, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entry.Artifact)))
		if err != nil {
			return nil, fmt.Errorf("%s: read artifact: %w", entry.ID, err)
		}
		if digest := fmt.Sprintf("%x", sha256.Sum256(artifact)); digest != entry.Digest {
			return nil, fmt.Errorf("%s: artifact SHA-256 = %s, want %s", entry.ID, digest, entry.Digest)
		}
		workload := Workload{
			ID:            entry.ID,
			Artifact:      entry.Artifact,
			ArtifactSize:  int64(len(artifact)),
			Stages:        entry.Stages,
			ExecCount:     len(entry.Exec),
			SemanticCount: len(entry.SemanticExec),
		}
		if workload.Units() > 0 {
			workloads = append(workloads, workload)
		}
	}
	sort.Slice(workloads, func(i, j int) bool { return workloads[i].ID < workloads[j].ID })
	if len(workloads) == 0 {
		return nil, errors.New("corpus catalog has no correctness workloads")
	}
	return workloads, nil
}

func (w Workload) Units() int64 {
	var stages int64
	if len(w.Stages) == 0 {
		stages = 5 // Decode, Validate, Compile, CompileFull, and Instantiate.
		stages += int64(w.ExecCount)
	} else {
		for _, stage := range w.Stages {
			switch stage {
			case "CommandExec":
			case "Exec":
				stages += int64(w.ExecCount)
			default:
				stages++
			}
		}
	}
	return stages + int64(w.SemanticCount*SemanticOracleRepetitions)
}

func (w Workload) Weight() int64 {
	if w.ArtifactSize < 1 || w.Units() < 1 {
		return 0
	}
	return w.ArtifactSize * w.Units()
}

// Groups uses largest-workload-first bin packing, with ID and shard index as
// stable tie breakers. Every workload remains intact in one shard.
func Groups(workloads []Workload, count int) ([][]Workload, error) {
	if count < 1 {
		return nil, fmt.Errorf("shard count must be positive, got %d", count)
	}
	if len(workloads) < count {
		return nil, fmt.Errorf("%d workloads cannot fill %d nonempty shards", len(workloads), count)
	}
	ordered := append([]Workload(nil), workloads...)
	seen := make(map[string]bool, len(ordered))
	for _, workload := range ordered {
		if workload.ID == "" || strings.Contains(workload.ID, ",") || seen[workload.ID] {
			return nil, fmt.Errorf("invalid or duplicate workload id %q", workload.ID)
		}
		if workload.Weight() < 1 {
			return nil, fmt.Errorf("%s: workload has no positive weight", workload.ID)
		}
		seen[workload.ID] = true
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Weight() != ordered[j].Weight() {
			return ordered[i].Weight() > ordered[j].Weight()
		}
		return ordered[i].ID < ordered[j].ID
	})
	groups := make([][]Workload, count)
	loads := make([]int64, count)
	for _, workload := range ordered {
		shard := 0
		for i := 1; i < count; i++ {
			if loads[i] < loads[shard] {
				shard = i
			}
		}
		groups[shard] = append(groups[shard], workload)
		loads[shard] += workload.Weight()
	}
	for i := range groups {
		sort.Slice(groups[i], func(a, b int) bool { return groups[i][a].ID < groups[i][b].ID })
	}
	return groups, nil
}

func IDs(workloads []Workload) []string {
	ids := make([]string, len(workloads))
	for i, workload := range workloads {
		ids[i] = workload.ID
	}
	slices.Sort(ids)
	return ids
}

func Selector(workloads []Workload) string { return strings.Join(IDs(workloads), ",") }

func ParseShard(value string) (int, int, error) {
	indexText, countText, ok := strings.Cut(value, "/")
	if !ok || strings.Contains(countText, "/") {
		return 0, 0, fmt.Errorf("invalid shard %q; want index/count", value)
	}
	index, err := strconv.Atoi(indexText)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid shard index %q: %w", indexText, err)
	}
	count, err := strconv.Atoi(countText)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid shard count %q: %w", countText, err)
	}
	if index < 0 || count < 1 || index >= count {
		return 0, 0, fmt.Errorf("shard %d/%d is outside the valid range", index, count)
	}
	return index, count, nil
}

func NewReport(sourceSHA, platform string, shard, shardCount int, workloads []Workload) Report {
	ids := IDs(workloads)
	return Report{
		Schema:     1,
		SourceSHA:  sourceSHA,
		Platform:   platform,
		Shard:      shard,
		ShardCount: shardCount,
		Selector:   strings.Join(ids, ","),
		Workloads:  ids,
	}
}
