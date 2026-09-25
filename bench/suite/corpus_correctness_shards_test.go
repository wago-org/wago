package wagobench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wago-org/wago/bench/internal/corpusplan"
)

type corpusCorrectnessShardTarget struct {
	Platform string
	Count    int
}

var requiredCorpusCorrectnessShardTargets = []corpusCorrectnessShardTarget{
	{Platform: "darwin/amd64", Count: 4},
	{Platform: "windows/arm64", Count: 4},
}

const requiredCorpusCorrectnessWorkloadCount = 125

func TestVerifyCorpusCorrectnessShardReports(t *testing.T) {
	root := os.Getenv("WAGO_CORPUS_CORRECTNESS_REPORT_DIR")
	if root == "" {
		t.Skip("corpus correctness shard report directory is not configured")
	}
	wantSHA := os.Getenv("CI_SOURCE_SHA")
	if wantSHA == "" {
		t.Fatal("CI_SOURCE_SHA is required when verifying corpus correctness shards")
	}
	workloads, err := corpusplan.LoadCatalog(filepath.Join(corpusDir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(workloads) != requiredCorpusCorrectnessWorkloadCount {
		t.Fatalf("catalog has %d correctness workloads, want %d; review shard coverage before changing this contract", len(workloads), requiredCorpusCorrectnessWorkloadCount)
	}
	paths, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	expectedReports := 0
	for _, target := range requiredCorpusCorrectnessShardTargets {
		expectedReports += target.Count
	}
	if len(paths) != expectedReports {
		t.Fatalf("found %d corpus correctness shard reports, want %d", len(paths), expectedReports)
	}

	type targetReports struct {
		count  int
		shards map[int]corpusplan.Report
	}
	reports := make(map[string]*targetReports, len(requiredCorpusCorrectnessShardTargets))
	for _, target := range requiredCorpusCorrectnessShardTargets {
		reports[target.Platform] = &targetReports{count: target.Count, shards: make(map[int]corpusplan.Report, target.Count)}
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var report corpusplan.Report
		if err := json.Unmarshal(data, &report); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		target, ok := reports[report.Platform]
		if !ok {
			t.Errorf("unexpected corpus correctness platform %q", report.Platform)
			continue
		}
		if report.Schema != 1 {
			t.Errorf("%s shard %d report schema = %d, want 1", report.Platform, report.Shard, report.Schema)
		}
		if report.SourceSHA != wantSHA {
			t.Errorf("%s shard %d tested SHA %q, want %q", report.Platform, report.Shard, report.SourceSHA, wantSHA)
		}
		if report.ShardCount != target.count || report.Shard < 0 || report.Shard >= target.count {
			t.Errorf("%s shard %d/%d does not match required count %d", report.Platform, report.Shard, report.ShardCount, target.count)
			continue
		}
		if !report.Passed {
			t.Errorf("%s shard %d did not report a successful corpus run", report.Platform, report.Shard)
		}
		if _, duplicate := target.shards[report.Shard]; duplicate {
			t.Errorf("duplicate %s shard result %d", report.Platform, report.Shard)
			continue
		}
		target.shards[report.Shard] = report
	}

	for _, target := range requiredCorpusCorrectnessShardTargets {
		groups, err := corpusplan.Groups(workloads, target.Count)
		if err != nil {
			t.Fatal(err)
		}
		seen := make(map[string]int, len(workloads))
		for shard := 0; shard < target.Count; shard++ {
			report, ok := reports[target.Platform].shards[shard]
			if !ok {
				t.Errorf("missing expected %s shard result %d/%d", target.Platform, shard, target.Count)
				continue
			}
			want := corpusplan.NewReport(wantSHA, target.Platform, shard, target.Count, groups[shard])
			if report.Selector != want.Selector {
				t.Errorf("%s shard %d selector differs from its deterministic plan", target.Platform, shard)
			}
			if !reflect.DeepEqual(report.Workloads, want.Workloads) {
				t.Errorf("%s shard %d workload ids = %v, want %v", target.Platform, shard, report.Workloads, want.Workloads)
			}
			for _, id := range report.Workloads {
				seen[id]++
			}
		}
		for _, workload := range workloads {
			if seen[workload.ID] != 1 {
				t.Errorf("%s workload %q appears %d times across shards", target.Platform, workload.ID, seen[workload.ID])
			}
		}
	}
}
