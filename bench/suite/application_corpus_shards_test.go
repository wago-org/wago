package wagobench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type applicationCorpusWorkloadResult struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	DurationNS int64  `json:"duration_ns"`
}

type applicationCorpusShardReport struct {
	SourceSHA  string                            `json:"source_sha"`
	Platform   string                            `json:"platform"`
	Shard      int                               `json:"shard"`
	ShardCount int                               `json:"shard_count"`
	Reference  string                            `json:"reference"`
	Workloads  []applicationCorpusWorkloadResult `json:"workloads"`
}

type applicationCorpusShardSpec struct {
	Platform string
	Runner   string
	Count    int
}

var requiredApplicationCorpusShards = []applicationCorpusShardSpec{
	{Platform: "linux/amd64", Runner: "ubuntu-24.04", Count: 2},
	{Platform: "linux/arm64", Runner: "ubuntu-24.04-arm", Count: 2},
	{Platform: "darwin/amd64", Runner: "macos-15-intel", Count: 4},
	{Platform: "darwin/arm64", Runner: "macos-15", Count: 2},
	{Platform: "windows/amd64", Runner: "windows-2025", Count: 1},
	{Platform: "windows/arm64", Runner: "windows-11-arm", Count: 1},
}

var requiredApplicationCorpusWorkloadCounts = map[string]int{
	"linux/amd64":   60,
	"linux/arm64":   60,
	"darwin/amd64":  59,
	"darwin/arm64":  60,
	"windows/amd64": 1,
	"windows/arm64": 1,
}

func applicationCorpusModules(tb testing.TB, goos, goarch string) []corpusModule {
	tb.Helper()
	var modules []corpusModule
	for _, m := range loadCorpus(tb) {
		if m.Command != nil && m.supports("CommandExec") && commandSupportsPlatform(m, goos, goarch) {
			modules = append(modules, m)
		}
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].ID < modules[j].ID })
	return modules
}

func applicationCorpusShardGroups(modules []corpusModule, count int) [][]corpusModule {
	if count < 1 {
		return nil
	}
	ordered := append([]corpusModule(nil), modules...)
	sort.Slice(ordered, func(i, j int) bool {
		if len(ordered[i].bytes) != len(ordered[j].bytes) {
			return len(ordered[i].bytes) > len(ordered[j].bytes)
		}
		return ordered[i].ID < ordered[j].ID
	})
	groups := make([][]corpusModule, count)
	loads := make([]int64, count)
	for _, module := range ordered {
		shard := 0
		for i := 1; i < count; i++ {
			if loads[i] < loads[shard] {
				shard = i
			}
		}
		groups[shard] = append(groups[shard], module)
		loads[shard] += int64(len(module.bytes))
	}
	for i := range groups {
		sort.Slice(groups[i], func(a, b int) bool { return groups[i][a].ID < groups[i][b].ID })
	}
	return groups
}

func currentApplicationCorpusShard(modules []corpusModule) (int, int, []corpusModule, error) {
	value := os.Getenv("WAGO_APP_CORPUS_SHARD")
	index, count := 0, 1
	if value != "" {
		parts := strings.Split(value, "/")
		if len(parts) != 2 {
			return 0, 0, nil, fmt.Errorf("invalid WAGO_APP_CORPUS_SHARD %q; want index/count", value)
		}
		var err error
		index, err = strconv.Atoi(parts[0])
		if err != nil {
			return 0, 0, nil, fmt.Errorf("invalid shard index %q", parts[0])
		}
		count, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, nil, fmt.Errorf("invalid shard count %q", parts[1])
		}
	}
	if count < 1 || index < 0 || index >= count {
		return 0, 0, nil, fmt.Errorf("invalid application corpus shard %d/%d", index, count)
	}
	groups := applicationCorpusShardGroups(modules, count)
	return index, count, groups[index], nil
}

func TestVerifyApplicationCorpusShardReports(t *testing.T) {
	root := os.Getenv("WAGO_APP_CORPUS_REPORT_DIR")
	if root == "" {
		t.Skip("application corpus shard report directory is not configured")
	}
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("application corpus shard reports are missing")
	}
	wantSHA := os.Getenv("CI_SOURCE_SHA")
	type platformReports struct {
		count  int
		shards map[int]applicationCorpusShardReport
	}
	reports := make(map[string]*platformReports, len(requiredApplicationCorpusShards))
	for _, spec := range requiredApplicationCorpusShards {
		reports[spec.Platform] = &platformReports{count: spec.Count, shards: make(map[int]applicationCorpusShardReport, spec.Count)}
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var report applicationCorpusShardReport
		if err := json.Unmarshal(data, &report); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		platform, ok := reports[report.Platform]
		if !ok {
			t.Errorf("unexpected application corpus platform %q", report.Platform)
			continue
		}
		if report.SourceSHA != wantSHA {
			t.Errorf("%s shard %d tested SHA %q, want %q", report.Platform, report.Shard, report.SourceSHA, wantSHA)
		}
		if report.ShardCount != platform.count || report.Shard < 0 || report.Shard >= platform.count {
			t.Errorf("%s shard %d/%d does not match required count %d", report.Platform, report.Shard, report.ShardCount, platform.count)
			continue
		}
		if _, duplicate := platform.shards[report.Shard]; duplicate {
			t.Errorf("duplicate %s shard result %d", report.Platform, report.Shard)
			continue
		}
		wantReference := "wago-only"
		if report.Platform == "linux/amd64" {
			wantReference = "all"
		}
		if report.Reference != wantReference {
			t.Errorf("%s reference mode = %q, want %q", report.Platform, report.Reference, wantReference)
		}
		platform.shards[report.Shard] = report
	}

	for _, spec := range requiredApplicationCorpusShards {
		platform := reports[spec.Platform]
		goos, goarch, _ := strings.Cut(spec.Platform, "/")
		modules := applicationCorpusModules(t, goos, goarch)
		groups := applicationCorpusShardGroups(modules, spec.Count)
		if len(modules) == 0 {
			t.Errorf("no admitted application workloads for %s", spec.Platform)
		}
		if want := requiredApplicationCorpusWorkloadCounts[spec.Platform]; len(modules) != want {
			t.Errorf("%s has %d admitted application workloads, want %d; review platform coverage before updating this contract", spec.Platform, len(modules), want)
		}
		seen := make(map[string]int, len(modules))
		for shard := 0; shard < spec.Count; shard++ {
			report, ok := platform.shards[shard]
			if !ok {
				t.Errorf("missing expected %s shard result %d/%d", spec.Platform, shard, spec.Count)
				continue
			}
			wantIDs := make(map[string]bool, len(groups[shard]))
			for _, module := range groups[shard] {
				wantIDs[module.ID] = true
			}
			gotIDs := make(map[string]bool, len(report.Workloads))
			for _, workload := range report.Workloads {
				if !wantIDs[workload.ID] {
					t.Errorf("%s shard %d ran unexpected workload %q", spec.Platform, shard, workload.ID)
				}
				if gotIDs[workload.ID] {
					t.Errorf("%s shard %d duplicated workload %q", spec.Platform, shard, workload.ID)
				}
				gotIDs[workload.ID] = true
				seen[workload.ID]++
				if workload.Status != "passed" {
					t.Errorf("%s workload %q result = %q", spec.Platform, workload.ID, workload.Status)
				}
				if workload.DurationNS < 0 {
					t.Errorf("%s workload %q has invalid duration %d", spec.Platform, workload.ID, workload.DurationNS)
				}
			}
			for id := range wantIDs {
				if !gotIDs[id] {
					t.Errorf("%s shard %d omitted planned workload %q", spec.Platform, shard, id)
				}
			}
		}
		for _, module := range modules {
			if seen[module.ID] != 1 {
				t.Errorf("%s workload %q appears %d times across shards", spec.Platform, module.ID, seen[module.ID])
			}
		}
	}
}

func TestApplicationCorpusShardPlanIsDeterministicAndComplete(t *testing.T) {
	modules := []corpusModule{
		{ID: "small-a", bytes: []byte{1}},
		{ID: "large-a", bytes: make([]byte, 100)},
		{ID: "small-b", bytes: []byte{1, 2}},
		{ID: "large-b", bytes: make([]byte, 99)},
	}
	first := applicationCorpusShardGroups(modules, 2)
	second := applicationCorpusShardGroups(modules, 2)
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("shard group count = %d and %d, want 2", len(first), len(second))
	}
	seen := make(map[string]int, len(modules))
	loads := make([]int, len(first))
	for shard, group := range first {
		if len(group) != len(second[shard]) {
			t.Fatalf("shard %d changed group size", shard)
		}
		for i, module := range group {
			if module.ID != second[shard][i].ID {
				t.Fatalf("shard %d changed assignment: %q != %q", shard, module.ID, second[shard][i].ID)
			}
			seen[module.ID]++
			loads[shard] += len(module.bytes)
		}
	}
	for _, module := range modules {
		if seen[module.ID] != 1 {
			t.Fatalf("workload %q appears %d times", module.ID, seen[module.ID])
		}
	}
	if delta := loads[0] - loads[1]; delta < -100 || delta > 100 {
		t.Fatalf("byte-weighted shard loads are not balanced: %v", loads)
	}
}

func TestApplicationCorpusWorkflowMatchesShardPlan(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("../..", ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(data)
	start := strings.Index(contents, "  app-corpus:\n")
	end := strings.Index(contents, "  app-corpus-verify:\n")
	if start < 0 || end <= start {
		t.Fatal("CI workflow has no bounded application corpus matrix")
	}
	block := contents[start:end]
	actual := make(map[string]map[int]int)
	for _, entry := range strings.Split(block, "          - name: ")[1:] {
		fields := make(map[string]string)
		for _, line := range strings.Split(entry, "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
			if ok {
				fields[key] = strings.TrimSpace(value)
			}
		}
		target, ok := fields["target"]
		if !ok {
			continue
		}
		goos, goarch, ok := strings.Cut(target, "-")
		if !ok {
			t.Errorf("invalid application corpus target %q", target)
			continue
		}
		platform := goos + "/" + goarch
		shard, shardErr := strconv.Atoi(fields["shard"])
		count, countErr := strconv.Atoi(fields["shards"])
		if shardErr != nil || countErr != nil {
			t.Errorf("invalid shard fields for %s: shard=%q count=%q", target, fields["shard"], fields["shards"])
			continue
		}
		if actual[platform] == nil {
			actual[platform] = make(map[int]int)
		}
		if prior, duplicate := actual[platform][shard]; duplicate {
			t.Errorf("duplicate workflow shard %s %d (counts %d and %d)", platform, shard, prior, count)
		}
		actual[platform][shard] = count
	}
	for _, spec := range requiredApplicationCorpusShards {
		got := actual[spec.Platform]
		if len(got) != spec.Count {
			t.Errorf("workflow defines %d shards for %s, want %d", len(got), spec.Platform, spec.Count)
		}
		for shard := 0; shard < spec.Count; shard++ {
			if got[shard] != spec.Count {
				t.Errorf("workflow shard %s %d has count %d, want %d", spec.Platform, shard, got[shard], spec.Count)
			}
		}
		delete(actual, spec.Platform)
	}
	for platform := range actual {
		t.Errorf("workflow has an unexpected application corpus platform %s", platform)
	}
}
