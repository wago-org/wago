package wagobench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	wago "github.com/wago-org/wago"
)

type indirectProbeSite struct {
	Site          int    `json:"site"`
	FunctionIndex int    `json:"function_index"`
	WAT           string `json:"wat"`
}

type indirectProbeCounts struct {
	First, Second         int32
	Total, FirstHits      uint64
	SecondHits, OtherHits uint64
}

// TestMutableIndirectDynamicProfile is an opt-in real-command probe. It uses
// WABT only to produce disposable instrumented Wasm and checks the command's
// original oracle. The eight candidate tables are exported but have no guest
// mutation op; the corpus host never mutates tables, so table index is a
// stable target identity for these specific runs.
func TestMutableIndirectDynamicProfile(t *testing.T) {
	if os.Getenv("WAGO_922_PROFILE") != "1" {
		t.Skip("set WAGO_922_PROFILE=1 to run the WABT real-command probe")
	}
	for _, tool := range []string{"python3", "wasm2wat", "wat2wasm"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("#922 probe requires %s: %v", tool, err)
		}
	}
	ids := []string{
		"lcs-substrings", "needleman-wunsch-dna", "smith-waterman-dna",
		"sed-records", "gnu-seq-range", "gnu-tr-uppercase",
		"seqtk-fastq-to-fasta", "fasttree-phylogeny",
	}
	available := make(map[string]corpusModule)
	for _, module := range commandCorpus(t) {
		available[module.ID] = module
	}
	for _, id := range ids {
		m, ok := available[id]
		if !ok {
			t.Fatalf("missing command corpus case %s", id)
		}
		t.Run(id, func(t *testing.T) {
			stdin := commandInput(t, m)
			baseline, err := wago.Compile(nil, m.bytes)
			if err != nil {
				t.Fatal(err)
			}
			defer baseline.Close()
			plain, err := runWagoCommand(m, baseline, stdin, true)
			if err != nil {
				t.Fatalf("original command: %v", err)
			}
			if err := validateCommandOutput(m, plain); err != nil {
				t.Fatalf("original oracle: %v", err)
			}
			output := filepath.Join(t.TempDir(), "profiled.wasm")
			cmd := exec.Command("python3", "../experiments/922-profile-indirect.py",
				filepath.Join(corpusDir, filepath.FromSlash(m.Artifact)), output)
			if detail, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("instrument Wasm: %v: %s", err, detail)
			}
			wasmBytes, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			var sites []indirectProbeSite
			meta, err := os.ReadFile(filepath.Join(filepath.Dir(output), "profiled.sites.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(meta, &sites); err != nil {
				t.Fatal(err)
			}
			compiled, err := wago.Compile(nil, wasmBytes)
			if err != nil {
				t.Fatalf("compile instrumented Wasm: %v", err)
			}
			defer compiled.Close()
			var prior []indirectProbeCounts
			for repeat := 0; repeat < 2; repeat++ {
				observed, counts, err := runProfiledIndirectCommand(m, compiled, stdin, sites)
				if err != nil {
					t.Fatalf("profiled run %d: %v", repeat+1, err)
				}
				if err := validateCommandOutput(m, observed); err != nil {
					t.Fatalf("profiled oracle: %v", err)
				}
				if !slices.Equal(plain.results, observed.results) || !bytes.Equal(plain.stdout, observed.stdout) || !bytes.Equal(plain.stderr, observed.stderr) || !reflect.DeepEqual(plain.files, observed.files) {
					t.Fatal("instrumented command result differs from original")
				}
				if repeat == 1 && !reflect.DeepEqual(prior, counts) {
					t.Fatalf("profile changed between identical runs: %v vs %v", prior, counts)
				}
				prior = counts
			}
			var total uint64
			for i, count := range prior {
				total += count.Total
				if count.Total != count.FirstHits+count.SecondHits+count.OtherHits {
					t.Fatalf("site %d bins do not sum to calls: %+v", i, count)
				}
				if count.Total != 0 {
					t.Logf("site=%d func=%d calls=%d first_index=%d first_hits=%d second_index=%d second_hits=%d other_hits=%d", i, sites[i].FunctionIndex, count.Total, count.First, count.FirstHits, count.Second, count.SecondHits, count.OtherHits)
				}
			}
			t.Logf("static_sites=%d executed_calls=%d original_bytes=%d profiled_bytes=%d", len(sites), total, len(m.bytes), len(wasmBytes))
		})
	}
}

func runProfiledIndirectCommand(m corpusModule, compiled *wago.Compiled, stdin []byte, sites []indirectProbeSite) (commandOutput, []indirectProbeCounts, error) {
	preopenDir, cleanup, err := commandScratchPreopen(m)
	if err != nil {
		return commandOutput{}, nil, err
	}
	defer cleanup()
	var stdout, stderr bytes.Buffer
	imports, err := commandRuntimeImports(m, preopenDir, stdin, &stdout, &stderr)
	if err != nil {
		return commandOutput{}, nil, err
	}
	in, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: imports})
	if err != nil {
		return commandOutput{}, nil, err
	}
	defer in.Close()
	var result []uint64
	if m.Command.Runtime == "emscripten" {
		result, err = invokeWagoEmscriptenMain(in, m.Command.Export, commandArgs(m))
	} else {
		result, err = in.Invoke(m.Command.Export)
	}
	if !commandExitOK(err) {
		return commandOutput{}, nil, err
	}
	counts := make([]indirectProbeCounts, len(sites))
	for i, site := range sites {
		read := func(key string) (uint64, error) {
			return in.Global(fmt.Sprintf("__wago_s%d_%s", site.Site, key))
		}
		if v, e := read("first"); e != nil {
			return commandOutput{}, nil, e
		} else {
			counts[i].First = int32(v)
		}
		if v, e := read("second"); e != nil {
			return commandOutput{}, nil, e
		} else {
			counts[i].Second = int32(v)
		}
		if counts[i].Total, err = read("total"); err != nil {
			return commandOutput{}, nil, err
		}
		if counts[i].FirstHits, err = read("first_hits"); err != nil {
			return commandOutput{}, nil, err
		}
		if counts[i].SecondHits, err = read("second_hits"); err != nil {
			return commandOutput{}, nil, err
		}
		if counts[i].OtherHits, err = read("other_hits"); err != nil {
			return commandOutput{}, nil, err
		}
	}
	files, err := commandOutputFiles(m, preopenDir)
	if err != nil {
		return commandOutput{}, nil, err
	}
	return commandOutput{results: result, stdout: stdout.Bytes(), stderr: stderr.Bytes(), files: files}, counts, nil
}
