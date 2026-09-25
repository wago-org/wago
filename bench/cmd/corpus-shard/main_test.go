package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/bench/internal/corpusplan"
)

func TestPlanWritesSelectorAndCompletionReport(t *testing.T) {
	root := t.TempDir()
	artifact := []byte("wasm artifact")
	if err := os.MkdirAll(filepath.Join(root, "corpus"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "corpus", "module.wasm"), artifact, 0o644); err != nil {
		t.Fatal(err)
	}
	entry := map[string]any{
		"id":              "module-a",
		"artifact":        "module.wasm",
		"artifact_sha256": fmt.Sprintf("%x", sha256.Sum256(artifact)),
	}
	catalog, err := json.Marshal(map[string]any{"schema": 1, "benchmarks": []any{entry}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "corpus", "catalog.json"), catalog, 0o644); err != nil {
		t.Fatal(err)
	}

	reportPath := filepath.Join(root, "reports", "shard.json")
	outputPath := filepath.Join(root, "github-output")
	err = plan([]string{
		"--root", root,
		"--platform", "darwin/amd64",
		"--shard", "0/1",
		"--source-sha", strings.Repeat("a", 40),
		"--report", reportPath,
		"--output", outputPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "selector=module-a\n" {
		t.Fatalf("GitHub output = %q", output)
	}

	if err := complete([]string{"--report", reportPath}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var report corpusplan.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.Selector != "module-a" || report.SourceSHA != strings.Repeat("a", 40) {
		t.Fatalf("completed report = %+v", report)
	}
}
