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

type repeatLoadSite struct {
	Site     int    `json:"site"`
	Function int    `json:"function"`
	Local    int    `json:"local"`
	Opcode   string `json:"opcode"`
	Offset   int    `json:"offset"`
}

// TestRepeatedLinearLoadDynamicPHP counts source candidates entered by a real
// PHP command. It does not claim that the backend emits or repeats a native load.
func TestRepeatedLinearLoadDynamicPHP(t *testing.T) {
	if os.Getenv("WAGO_919_PROFILE") != "1" {
		t.Skip("set WAGO_919_PROFILE=1 for disposable WABT probe")
	}
	for _, tool := range []string{"python3", "wasm2wat", "wat2wasm"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("need %s: %v", tool, err)
		}
	}
	var m corpusModule
	for _, candidate := range commandCorpus(t) {
		if candidate.ID == "php-buckets" {
			m = candidate
			break
		}
	}
	if m.ID == "" {
		t.Fatal("missing php-buckets command")
	}
	stdin := commandInput(t, m)
	original, err := wago.Compile(nil, m.bytes)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	plain, err := runWagoCommand(m, original, stdin, true)
	if err != nil {
		t.Fatalf("original: %v", err)
	}
	if err := validateCommandOutput(m, plain); err != nil {
		t.Fatalf("original oracle: %v", err)
	}
	output := filepath.Join(t.TempDir(), "profiled.wasm")
	cmd := exec.Command("python3", "../experiments/919-profile-repeat-loads.py", filepath.Join(corpusDir, filepath.FromSlash(m.Artifact)), output)
	if detail, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("instrument: %v: %s", err, detail)
	}
	wasmBytes, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := os.ReadFile(filepath.Join(filepath.Dir(output), "profiled.sites.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sites []repeatLoadSite
	if err := json.Unmarshal(meta, &sites); err != nil {
		t.Fatal(err)
	}
	if len(sites) != 4242 {
		t.Fatalf("source probe sites=%d, want binary decoder's 4242", len(sites))
	}
	compiled, err := wago.Compile(nil, wasmBytes)
	if err != nil {
		t.Fatalf("compile instrumented: %v", err)
	}
	defer compiled.Close()
	var prior []uint64
	for repeat := 0; repeat < 2; repeat++ {
		observed, counts, err := runProfiledRepeatPHP(m, compiled, stdin, sites)
		if err != nil {
			t.Fatalf("profiled run %d: %v", repeat+1, err)
		}
		if err := validateCommandOutput(m, observed); err != nil {
			t.Fatalf("profiled oracle: %v", err)
		}
		if !slices.Equal(plain.results, observed.results) || !bytes.Equal(plain.stdout, observed.stdout) || !bytes.Equal(plain.stderr, observed.stderr) || !reflect.DeepEqual(plain.files, observed.files) {
			t.Fatal("instrumented PHP output differs from original")
		}
		if repeat == 1 && !slices.Equal(prior, counts) {
			t.Fatal("strict repeated-load counts changed between identical runs")
		}
		prior = counts
	}
	var total, hot uint64
	active, hotSite := 0, -1
	perFunction := map[int]uint64{}
	for i, count := range prior {
		if count != 0 {
			active++
			perFunction[sites[i].Function] += count
		}
		total += count
		if count > hot {
			hot, hotSite = count, i
		}
	}
	t.Logf("source_candidates=%d active_sites=%d executed_candidate_loads=%d active_functions=%d hottest_site=%d hottest_site_calls=%d original_bytes=%d profiled_bytes=%d original_native_bytes=%d profiled_native_bytes=%d", len(sites), active, total, len(perFunction), hotSite, hot, len(m.bytes), len(wasmBytes), original.CodeSize(), compiled.CodeSize())
	if hotSite >= 0 {
		t.Logf("hottest_source_candidate=%+v", sites[hotSite])
	}
}

func runProfiledRepeatPHP(m corpusModule, compiled *wago.Compiled, stdin []byte, sites []repeatLoadSite) (commandOutput, []uint64, error) {
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
	result, err := in.Invoke(m.Command.Export)
	if !commandExitOK(err) {
		return commandOutput{}, nil, err
	}
	counts := make([]uint64, len(sites))
	for i := range counts {
		counts[i], err = in.Global(fmt.Sprintf("__wago_repeat_%d", i))
		if err != nil {
			return commandOutput{}, nil, err
		}
	}
	files, err := commandOutputFiles(m, preopenDir)
	if err != nil {
		return commandOutput{}, nil, err
	}
	return commandOutput{results: result, stdout: stdout.Bytes(), stderr: stderr.Bytes(), files: files}, counts, nil
}
