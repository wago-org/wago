package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
		name  string
		file  bool
		count int
	}{{"file", true, 1}, {"one-catalog-module", false, 1}, {"two-catalog-modules", false, 2}} {
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
			encoded, err := json.Marshal(catalog)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(corpus, "catalog.json"), encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"-test.run=^TestValidationStatsCommand$", "--", "-runs=2", "-warmup=0"}
			if tc.file {
				args = append(args, "-file="+filepath.Join(corpus, "module0.wasm"))
			}
			cmd := exec.Command(os.Args[0], args...)
			cmd.Dir = work
			cmd.Env = append(os.Environ(), "WAGO_VALIDATESTATS_TEST_CHILD=1")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("command: %v\n%s", err, output)
			}
			if !strings.Contains(string(output), fmt.Sprintf("modules=%d", tc.count)) || !strings.Contains(string(output), "module0") {
				t.Fatalf("missing measurement: %s", output)
			}
			if strings.Contains(string(output), "CORPUS(sum/run)") != (tc.count > 1) {
				t.Fatalf("unexpected corpus summary: %s", output)
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
