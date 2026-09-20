package wagobench

import (
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
