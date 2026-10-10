//go:build wago_codegenstats || wago_profile

package wagobench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"testing"
	"time"

	wago "github.com/wago-org/wago"
	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestLazyFunctionEntryProfile counts visited local functions on disposable
// instrumented Wasm, with the original real command as its output oracle.
func TestLazyFunctionEntryProfile(t *testing.T) {
	if os.Getenv("WAGO_925_PROFILE") != "1" {
		t.Skip("set WAGO_925_PROFILE=1 for WABT entry coverage")
	}
	for _, tool := range []string{"python3", "wasm2wat", "wat2wasm"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("need %s: %v", tool, err)
		}
	}
	ids := []string{"fasttree-phylogeny", "jq-json-transform", "quickjs-script", "sqlite3-query", "php-buckets"}
	available := map[string]corpusModule{}
	for _, m := range commandCorpus(t) {
		available[m.ID] = m
	}
	for _, id := range ids {
		m, ok := available[id]
		if !ok {
			t.Fatalf("missing corpus command %s", id)
		}
		t.Run(id, func(t *testing.T) {
			stdin := commandInput(t, m)
			original, err := wago.Compile(nil, m.bytes)
			if err != nil {
				t.Fatal(err)
			}
			defer original.Close()
			plain, err := runWagoCommand(m, original, stdin, true)
			if err != nil {
				t.Fatalf("original command: %v", err)
			}
			if err := validateCommandOutput(m, plain); err != nil {
				t.Fatalf("original oracle: %v", err)
			}
			output := filepath.Join(t.TempDir(), "profiled.wasm")
			cmd := exec.Command("python3", "../experiments/925-profile-functions.py", filepath.Join(corpusDir, filepath.FromSlash(m.Artifact)), output)
			if detail, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("instrument: %v: %s", err, detail)
			}
			wasmBytes, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			meta, err := os.ReadFile(filepath.Join(filepath.Dir(output), "profiled.functions.json"))
			if err != nil {
				t.Fatal(err)
			}
			var functions []int
			if err := json.Unmarshal(meta, &functions); err != nil {
				t.Fatal(err)
			}
			if len(functions) == 0 {
				t.Fatal("no local functions")
			}
			compiled, err := wago.Compile(nil, wasmBytes)
			if err != nil {
				t.Fatalf("compile instrumented: %v", err)
			}
			defer compiled.Close()
			var prior []int
			for repeat := 0; repeat < 2; repeat++ {
				observed, visited, err := runProfiledFunctionCommand(m, compiled, stdin, functions)
				if err != nil {
					t.Fatalf("profiled run %d: %v", repeat+1, err)
				}
				if err := validateCommandOutput(m, observed); err != nil {
					t.Fatalf("profiled oracle: %v", err)
				}
				if !slices.Equal(plain.results, observed.results) || !bytes.Equal(plain.stdout, observed.stdout) || !bytes.Equal(plain.stderr, observed.stderr) || !reflect.DeepEqual(plain.files, observed.files) {
					t.Fatal("instrumented command result differs from original")
				}
				if repeat == 1 && !slices.Equal(prior, visited) {
					t.Fatal("visited function set differs between identical runs")
				}
				prior = visited
			}
			t.Logf("local_functions=%d entered_functions=%d entered_share=%.2f%% source_bytes=%d profiled_bytes=%d native_bytes=%d profiled_native_bytes=%d", len(functions), len(prior), 100*float64(len(prior))/float64(len(functions)), len(m.bytes), len(wasmBytes), original.CodeSize(), compiled.CodeSize())
			decoded, err := wasm.DecodeModuleWithFeatures(m.bytes, wasm.ValidationFeatures{})
			if err != nil {
				t.Fatalf("decode for native-size attribution: %v", err)
			}
			var stats backend.ModuleStats
			diagnostic, err := backend.CompileModuleWith(decoded, backend.CompileOptions{Workers: 1, Stats: &stats})
			if err != nil {
				t.Fatalf("compile for native-size attribution: %v", err)
			}
			if diagnostic.CodeImage != nil {
				defer diagnostic.CodeImage.Close()
			}
			if len(stats.Funcs) != len(functions) {
				t.Fatalf("diagnostic functions=%d, instrumented=%d", len(stats.Funcs), len(functions))
			}
			imported := decoded.ImportedFuncCount()
			var allBodyBytes, enteredBodyBytes int
			for _, fn := range stats.Funcs {
				allBodyBytes += fn.CodeBytes
			}
			for _, globalIndex := range prior {
				localIndex := globalIndex - imported
				if localIndex < 0 || localIndex >= len(stats.Funcs) {
					t.Fatalf("bad local function index %d", localIndex)
				}
				enteredBodyBytes += stats.Funcs[localIndex].CodeBytes
			}
			t.Logf("standalone_backend_body_bytes=%d entered_body_bytes=%d entered_body_share=%.2f%% unentered_body_bytes=%d", allBodyBytes, enteredBodyBytes, 100*float64(enteredBodyBytes)/float64(allBodyBytes), allBodyBytes-enteredBodyBytes)
			if m.ID == "php-buckets" {
				measureUnenteredBodyCeiling(t, m, stdin, plain, prior)
			}
		})
	}
}

// measureUnenteredBodyCeiling replaces unentered function bodies with traps in
// a disposable module. It measures a hypothetical code floor for this exact
// input only; another input could call any of the replaced functions.
func measureUnenteredBodyCeiling(t *testing.T, m corpusModule, stdin []byte, plain commandOutput, entered []int) {
	t.Helper()
	root := t.TempDir()
	enteredPath := filepath.Join(root, "entered.json")
	encoded, err := json.Marshal(entered)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(enteredPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "unentered-trap-ceiling.wasm")
	cmd := exec.Command("python3", "../experiments/925-unentered-body-ceiling.py", filepath.Join(corpusDir, filepath.FromSlash(m.Artifact)), enteredPath, output)
	if detail, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build input-specific ceiling: %v: %s", err, detail)
	}
	stripped, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	compile := func(data []byte) (*wago.Compiled, time.Duration, uint64, uint64) {
		t.Helper()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		compiled, err := wago.Compile(nil, data)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		return compiled, elapsed, after.TotalAlloc - before.TotalAlloc, after.Mallocs - before.Mallocs
	}
	baseline, baseTime, baseBytes, baseAllocs := compile(m.bytes)
	baseNative := baseline.CodeSize()
	if err := baseline.Close(); err != nil {
		t.Fatal(err)
	}
	ceiling, ceilingTime, ceilingBytes, ceilingAllocs := compile(stripped)
	defer ceiling.Close()
	got, err := runWagoCommand(m, ceiling, stdin, true)
	if err != nil {
		t.Fatalf("input-specific ceiling command: %v", err)
	}
	if err := validateCommandOutput(m, got); err != nil {
		t.Fatalf("input-specific ceiling oracle: %v", err)
	}
	if !slices.Equal(plain.results, got.results) || !bytes.Equal(plain.stdout, got.stdout) || !bytes.Equal(plain.stderr, got.stderr) || !reflect.DeepEqual(plain.files, got.files) {
		t.Fatal("input-specific ceiling output differs")
	}
	t.Logf("input_specific_ceiling original_source_bytes=%d transformed_source_bytes=%d baseline_native_bytes=%d ceiling_native_bytes=%d baseline_compile_ms=%.2f ceiling_compile_ms=%.2f baseline_allocated_bytes=%d ceiling_allocated_bytes=%d baseline_mallocs=%d ceiling_mallocs=%d", len(m.bytes), len(stripped), baseNative, ceiling.CodeSize(), float64(baseTime.Microseconds())/1000, float64(ceilingTime.Microseconds())/1000, baseBytes, ceilingBytes, baseAllocs, ceilingAllocs)
}

func runProfiledFunctionCommand(m corpusModule, compiled *wago.Compiled, stdin []byte, functions []int) (commandOutput, []int, error) {
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
	visited := make([]int, 0, len(functions))
	for _, index := range functions {
		value, err := in.Global(fmt.Sprintf("__wago_visited_%d", index))
		if err != nil {
			return commandOutput{}, nil, err
		}
		if value != 0 && value != 1 {
			return commandOutput{}, nil, fmt.Errorf("function %d visited value %d", index, value)
		}
		if value == 1 {
			visited = append(visited, index)
		}
	}
	files, err := commandOutputFiles(m, preopenDir)
	if err != nil {
		return commandOutput{}, nil, err
	}
	return commandOutput{results: result, stdout: stdout.Bytes(), stderr: stderr.Bytes(), files: files}, visited, nil
}
