package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var validModule = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

func TestValidationStatsCommand(t *testing.T) {
	if os.Getenv("WAGO_VALIDATESTATS_TEST_CHILD") == "1" {
		flag.CommandLine = flag.NewFlagSet("validatestats", flag.ExitOnError)
		for i, arg := range os.Args {
			if arg == "--" {
				os.Args = append([]string{"validatestats"}, os.Args[i+1:]...)
				break
			}
		}
		main()
		return
	}
	for _, tc := range []struct {
		name    string
		file    bool
		count   int
		skipped int
		runs    int
		warmup  int
	}{
		{name: "file", file: true, count: 1, runs: 2},
		{name: "one-catalog-module", count: 1, runs: 2},
		{name: "two-catalog-modules", count: 2, runs: 2},
		{name: "filtered-catalog-module", count: 1, skipped: 2, runs: 2},
		{name: "file-one-run-with-warmup", file: true, count: 1, runs: 1, warmup: 2},
		{name: "two-catalog-modules-one-run-with-warmup", count: 2, runs: 1, warmup: 2},
		{name: "no-validation-modules", skipped: 2, runs: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			work := filepath.Join(root, "bench")
			corpus := filepath.Join(root, "corpus")
			for _, dir := range []string{work, corpus} {
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			var catalog manifest
			for i := 0; i < tc.count; i++ {
				name := fmt.Sprintf("module%d.wasm", i)
				if err := os.WriteFile(filepath.Join(corpus, name), validModule, 0o600); err != nil {
					t.Fatal(err)
				}
				catalog.Modules = append(catalog.Modules, corpusModule{ID: fmt.Sprintf("module%d", i), Artifact: name})
			}
			for i := 0; i < tc.skipped; i++ {
				catalog.Modules = append(catalog.Modules, corpusModule{
					ID: fmt.Sprintf("skipped%d", i), Artifact: "not-read.wasm", Stages: []string{"Compile"},
				})
			}
			encoded, err := json.Marshal(catalog)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(corpus, "catalog.json"), encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"-test.run=^TestValidationStatsCommand$", "--", fmt.Sprintf("-runs=%d", tc.runs), fmt.Sprintf("-warmup=%d", tc.warmup)}
			if tc.file {
				args = append(args, "-file="+filepath.Join(corpus, "module0.wasm"))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], args...)
			cmd.Dir = work
			cmd.Env = append(os.Environ(), "WAGO_VALIDATESTATS_TEST_CHILD=1")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("command: %v\n%s", err, output)
			}
			text := string(output)
			header := fmt.Sprintf("runs=%d warmup=%d modules=%d\n", tc.runs, tc.warmup, tc.count)
			if !strings.Contains(text, header) {
				t.Fatalf("missing run configuration: %s", output)
			}
			rows := make(map[string]int)
			for _, line := range strings.Split(text, "\n") {
				fields := strings.Fields(line)
				if len(fields) == 5 && strings.HasPrefix(fields[0], "module") && fields[0] != "module" {
					if fields[1] != fmt.Sprint(tc.runs) {
						t.Fatalf("unexpected measured run count: %s", line)
					}
					rows[fields[0]]++
				}
			}
			if len(rows) != tc.count || strings.Contains(text, "skipped") {
				t.Fatalf("unexpected module rows: %s", output)
			}
			for i := 0; i < tc.count; i++ {
				if rows[fmt.Sprintf("module%d", i)] != 1 {
					t.Fatalf("missing or duplicate measurement: %s", output)
				}
			}
			for _, summary := range []string{"MEAN(module avg)", "CORPUS(sum/run)"} {
				if strings.Contains(text, summary) != (tc.count > 1) {
					t.Fatalf("unexpected %s summary: %s", summary, output)
				}
			}
		})
	}
}

func BenchmarkMeasureModule(b *testing.B) {
	mod := corpusModule{ID: "empty", bytes: validModule}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := measure(mod, 20, 3); err != nil {
			b.Fatal(err)
		}
	}
}
