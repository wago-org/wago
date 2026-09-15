package wagobench

import (
	"encoding/json"
	"os"
	"path/filepath"
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
