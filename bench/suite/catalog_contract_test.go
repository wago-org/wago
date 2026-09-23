package wagobench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/wago-org/wago/bench/internal/semanticcorpus"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestCatalogContainsOnlyExecutableWorkloads(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(corpusDir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest catalog
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, mod := range manifest.Benchmarks {
		if err := validateCorpusModule(mod); err != nil {
			t.Error(err)
		}
	}
	checks, err := semanticcorpus.LoadManifest(filepath.Join(corpusDir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCatalogLinks(manifest, checks.Modules); err != nil {
		t.Fatal(err)
	}
	const websiteMinimum = 20
	if got := len(manifest.Profiles["quick"]); got < websiteMinimum {
		t.Errorf("quick profile has %d workloads, want at least %d for the website", got, websiteMinimum)
	}
}

func TestCommandCorpusRunsOnLinuxAMD64(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(corpusDir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest catalog
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, benchmark := range manifest.Benchmarks {
		if benchmark.Command != nil && !commandSupportsPlatform(benchmark, "linux", "amd64") {
			t.Errorf("command benchmark %q silently skips linux/amd64", benchmark.ID)
		}
	}
}

func TestSightglassLibsodiumTuningInput(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(corpusDir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest catalog
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, m := range manifest.Benchmarks {
		if m.ID != "sightglass-libsodium-hash" {
			continue
		}
		const input = "libsodium-hash.input"
		module, err := os.ReadFile(filepath.Join(corpusDir, m.Artifact))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(module, []byte("./"+input)) {
			t.Fatalf("module does not read %q", input)
		}
		if m.Command == nil || m.Command.Stdin != "" {
			t.Fatal("Sightglass iterations must come from a mounted file, not stdin")
		}
		if _, ok := m.Command.Inputs[input]; !ok {
			t.Fatalf("%q is not mounted", input)
		}
		contents, err := os.ReadFile(filepath.Join(corpusDir, m.Command.Preopen, input))
		if err != nil {
			t.Fatal(err)
		}
		if string(contents) != "1061\n" {
			t.Fatalf("Sightglass iteration count = %q, want 1061", contents)
		}
		return
	}
	t.Fatal("sightglass-libsodium-hash is missing from the catalog")
}

func TestCorpusCandidatesStaySeparateFromExecutableCatalog(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(corpusDir, "candidates.json"))
	if err != nil {
		t.Fatal(err)
	}
	var queue struct {
		Schema   int `json:"schema"`
		Admitted []struct {
			Program   string `json:"program"`
			Benchmark string `json:"benchmark"`
		} `json:"admitted"`
		Groups []struct {
			Route    string   `json:"route"`
			Programs []string `json:"programs"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(data, &queue); err != nil {
		t.Fatal(err)
	}
	if queue.Schema != 1 {
		t.Fatalf("candidate schema = %d, want 1", queue.Schema)
	}
	seen := map[string]bool{}
	for _, group := range queue.Groups {
		if group.Route == "" || len(group.Programs) == 0 {
			t.Fatalf("invalid candidate group: %+v", group)
		}
		for _, program := range group.Programs {
			key := strings.ToLower(strings.TrimSpace(program))
			if key == "" || seen[key] {
				t.Fatalf("empty or duplicate candidate %q", program)
			}
			seen[key] = true
		}
	}
	var catalog catalog
	data, err = os.ReadFile(filepath.Join(corpusDir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, entry := range queue.Admitted {
		if entry.Program == "" || entry.Benchmark == "" {
			t.Fatalf("incomplete admission: %+v", entry)
		}
		found := false
		for _, benchmark := range catalog.Benchmarks {
			found = found || benchmark.ID == entry.Benchmark
		}
		if !found {
			t.Errorf("%s claims admission to missing benchmark %s", entry.Program, entry.Benchmark)
		}
	}
}

func TestValidateCorpusModuleRequiresEndToEndOracle(t *testing.T) {
	base := corpusModule{ID: "example", Artifact: "example.wasm", ArtifactSHA256: "digest"}
	cases := []struct {
		name string
		edit func(*corpusModule)
	}{
		{name: "compile-only"},
		{name: "direct-without-oracle", edit: func(m *corpusModule) {
			m.Exec = []execEntry{{Export: "run"}}
		}},
		{name: "direct-excluded-by-stages", edit: func(m *corpusModule) {
			m.Exec = []execEntry{{Export: "run", Want: []uint64{0}}}
			m.Stages = []string{"Decode", "Validate", "Compile"}
		}},
		{name: "command-without-oracle", edit: func(m *corpusModule) {
			m.Command = &commandEntry{Export: "_start"}
			m.Stages = []string{"CommandExec"}
		}},
		{name: "unknown-command-oracle", edit: func(m *corpusModule) { m.Command = &commandEntry{Export: "run", Oracle: "unknown"} }},
		{name: "return-without-results", edit: func(m *corpusModule) { m.Command = &commandEntry{Export: "run", Oracle: "return"} }},
		{name: "multiple-execution-contracts", edit: func(m *corpusModule) {
			m.Exec = []execEntry{{Export: "run", Want: []uint64{0}}}
			m.SemanticExec = []string{"example/run"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mod := base
			if tc.edit != nil {
				tc.edit(&mod)
			}
			if err := validateCorpusModule(mod); err == nil {
				t.Fatal("validation succeeded")
			}
		})
	}

	valid := base
	valid.Exec = []execEntry{{Export: "run", Want: []uint64{0}}}
	if err := validateCorpusModule(valid); err != nil {
		t.Fatalf("valid executable corpus rejected: %v", err)
	}
}

func validateCatalogLinks(c catalog, checks []semanticcorpus.Module) error {
	for _, mod := range c.Benchmarks {
		if _, err := resolveSemanticCases(mod, checks); err != nil {
			return err
		}
	}
	for _, check := range checks {
		referenced := false
		for _, mod := range c.Benchmarks {
			referenced = referenced || slices.Contains(mod.SemanticExec, check.ID)
		}
		if !referenced {
			return fmt.Errorf("unreferenced semantic check %q", check.ID)
		}
	}
	return nil
}

func TestCatalogSemanticLinks(t *testing.T) {
	check := semanticcorpus.Module{ID: "check", Artifact: "example.wasm", ArtifactSHA256: "digest"}
	mod := corpusModule{ID: "example", Artifact: check.Artifact, ArtifactSHA256: check.ArtifactSHA256, SemanticExec: []string{check.ID}}
	for _, tc := range []struct {
		name, want string
		edit       func(*corpusModule)
	}{
		{"valid", "", func(*corpusModule) {}},
		{"unknown", "unknown semantic check", func(m *corpusModule) { m.SemanticExec = []string{"missing"} }},
		{"unreferenced", "unreferenced semantic check", func(m *corpusModule) { m.SemanticExec = nil }},
		{"artifact", "artifact or digest differs", func(m *corpusModule) { m.Artifact = "other.wasm" }},
		{"digest", "artifact or digest differs", func(m *corpusModule) { m.ArtifactSHA256 = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mod
			tc.edit(&m)
			err := validateCatalogLinks(catalog{Benchmarks: []corpusModule{m}}, []semanticcorpus.Module{check})
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
}

func TestCatalogSelection(t *testing.T) {
	c := catalog{Profiles: map[string][]string{"quick": {"a"}}, Benchmarks: []corpusModule{
		{ID: "a", Tags: []string{"parser"}}, {ID: "b", Tags: []string{"vm"}},
	}}
	for _, tc := range []struct {
		selector string
		want     []string
	}{
		{"all", []string{"a", "b"}}, {"quick", []string{"a"}}, {"tag:vm", []string{"b"}}, {"a,b", []string{"a", "b"}},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			ids := selectedIDs(t, c, tc.selector)
			var got []string
			for _, m := range c.Benchmarks {
				if ids[m.ID] || selectedByTag(tc.selector, m.Tags) {
					got = append(got, m.ID)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("selected %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidateCorpusModuleStagesAndSource(t *testing.T) {
	for _, contract := range []string{"direct", "semantic", "command"} {
		t.Run(contract, func(t *testing.T) {
			m := corpusModule{ID: "example", Artifact: "example.wasm", ArtifactSHA256: "digest"}
			switch contract {
			case "direct":
				m.Exec = []execEntry{{Export: "run", Want: []uint64{1}}}
			case "semantic":
				m.SemanticExec = []string{"check"}
			case "command":
				m.Command = &commandEntry{Export: "run", Want: []uint64{1}}
			}
			m.Source = &sourceEntry{Repository: "repo", Revision: "rev", RevisionDate: "date", License: "MIT", Toolchain: "sdk", ToolchainVersion: "34"}
			if err := validateCorpusModule(m); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"repository", "revision", "date", "license", "toolchain", "version"} {
				t.Run(field, func(t *testing.T) {
					bad := m
					source := *m.Source
					bad.Source = &source
					switch field {
					case "repository":
						source.Repository = ""
					case "revision":
						source.Revision = ""
					case "date":
						source.RevisionDate = ""
					case "license":
						source.License = ""
					case "toolchain":
						source.Toolchain = ""
					case "version":
						source.ToolchainVersion = ""
					}
					if err := validateCorpusModule(bad); err == nil || !strings.Contains(err.Error(), "source provenance") {
						t.Fatalf("got %v", err)
					}
				})
			}
			m.Stages = []string{"Compile"}
			if err := validateCorpusModule(m); err == nil || !strings.Contains(err.Error(), "excluded by stages") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestCatalogCIGates(t *testing.T) {
	for _, file := range []string{".just/test.just", ".github/workflows/ci.yml"} {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../..", file))
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if !strings.Contains(text, "go test -count=1 ./internal/semanticcorpus") {
				t.Fatal("semantic manifest and provenance tests are not selected")
			}
			if file == ".just/test.just" && !strings.Contains(text, "quick corpus=env('CORPUS', 'all'):") {
				t.Fatal("ordinary test gate must default to all")
			}
			found := false
			for _, line := range strings.Split(text, "\n") {
				if !strings.Contains(line, "TestApplicationCorpusRuns") {
					continue
				}
				found = true
				fields := strings.Split(line, "'")
				if len(fields) < 3 {
					t.Fatalf("missing quoted run expression: %s", line)
				}
				pattern, err := regexp.Compile(fields[1])
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"TestCorpus", "TestCorpusSemanticExec", "TestApplicationCorpusRuns", "TestCatalogContainsOnlyExecutableWorkloads", "TestCatalogSemanticLinks", "TestCatalogSelection", "TestCatalogCIGates", "TestValidateCorpusModuleRequiresEndToEndOracle", "TestValidateCorpusModuleStagesAndSource"} {
					if !pattern.MatchString(name) {
						t.Errorf("CI expression excludes %s", name)
					}
				}
				if file == ".github/workflows/ci.yml" && !strings.Contains(line, "-wago.corpus=all") {
					t.Fatal("Windows ordinary corpus gate must select all")
				}
			}
			if !found {
				t.Fatal("missing corpus execution gate")
			}
		})
	}
}

func TestValidateCorpusModuleCommandOracles(t *testing.T) {
	for _, command := range []commandEntry{
		{Export: "_start", Oracle: "self-check"},
		{Export: "run", Oracle: "return", Want: []uint64{0}},
		{Export: "run", StdoutSHA256: "digest"},
	} {
		m := corpusModule{ID: "example", Artifact: "example.wasm", ArtifactSHA256: "digest", Command: &command}
		if err := validateCorpusModule(m); err != nil {
			t.Fatal(err)
		}
	}
}
