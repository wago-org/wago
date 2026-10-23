//go:build linux

package wagobench

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"syscall"
	"testing"
	"time"

	wago "github.com/wago-org/wago"
)

// TestOptimizationStudySample is an opt-in fresh-process measurement using the
// pinned wasm.fyi contract. Compile RSS is captured before any guest execution.
func TestOptimizationStudySample(t *testing.T) {
	path := os.Getenv("WAGO_STUDY_MANIFEST")
	if path == "" {
		t.Skip("external optimization study")
	}
	cfg := wago.NewRuntimeConfig().WithFunctionWorkers(1)
	if os.Getenv("WAGO_STUDY_LIST") == "1" {
		raw, _ := json.Marshal(cfg.OptimizationInfos())
		fmt.Println("STUDY_OPTS " + string(raw))
		return
	}
	var manifest struct {
		Corpus []struct {
			ID       string `json:"id"`
			SHA256   string `json:"sha256"`
			Contract struct {
				Export string        `json:"export"`
				Args   []json.Number `json:"args"`
				Oracle struct {
					Kind                string   `json:"kind"`
					Expected            []string `json:"expected"`
					OutputPointerExport string   `json:"output_pointer_export"`
					Memory              []struct {
						Offset uint32 `json:"offset"`
						Hex    string `json:"hex"`
					} `json:"memory"`
				} `json:"oracle"`
			} `json:"contract"`
		} `json:"corpus"`
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	selected := loadCorpus(t)
	if len(selected) != 1 {
		t.Fatal("select exactly one workload")
	}
	m := selected[0]
	row := -1
	for i := range manifest.Corpus {
		if manifest.Corpus[i].ID == m.ID {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatal("workload outside frozen manifest")
	}
	entry := manifest.Corpus[row]
	if fmt.Sprintf("%x", sha256.Sum256(m.bytes)) != entry.SHA256 {
		t.Fatal("manifest artifact mismatch")
	}
	opt := os.Getenv("WAGO_STUDY_OPT")
	enabled := os.Getenv("WAGO_STUDY_ON") != "0"
	if opt != "" {
		cfg = cfg.WithOptimization(opt, enabled)
	}
	if err = cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after, retained runtime.MemStats
	runtime.ReadMemStats(&before)
	rss := func() int64 {
		var r syscall.Rusage
		if e := syscall.Getrusage(syscall.RUSAGE_SELF, &r); e != nil {
			panic(e)
		}
		return r.Maxrss * 1024
	}
	startup := rss()
	profile := os.Getenv("WAGO_STUDY_CPU_PROFILE")
	if profile != "" {
		f, e := os.Create(profile)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		if e = pprof.StartCPUProfile(f); e != nil {
			t.Fatal(e)
		}
	}
	started := time.Now()
	compiled, err := cfg.Compile(m.bytes)
	compileNS := time.Since(started).Nanoseconds()
	if profile != "" {
		pprof.StopCPUProfile()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	runtime.ReadMemStats(&after)
	peak := rss()
	code := compiled.CodeSize()
	runtime.GC()
	runtime.ReadMemStats(&retained)
	if path := os.Getenv("WAGO_STUDY_HEAP_PROFILE"); path != "" {
		f, e := os.Create(path)
		if e != nil {
			t.Fatal(e)
		}
		if e = pprof.WriteHeapProfile(f); e != nil {
			t.Fatal(e)
		}
		f.Close()
	}
	var invoke func() error
	if m.Command != nil {
		validateCommandInputs(t, m)
		stdin := commandInput(t, m)
		invoke = func() error {
			out, e := runWagoCommand(m, compiled, stdin, true)
			if e != nil {
				return e
			}
			return validateCommandOutput(m, out)
		}
	} else {
		in, e := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: hostStubs(compiled)})
		if e != nil {
			t.Fatal(e)
		}
		defer in.Close()
		if m.Init != "" {
			if _, e = in.Invoke(m.Init); e != nil {
				t.Fatal(e)
			}
		}
		c := entry.Contract
		if c.Oracle.Kind != "exact_u64" {
			t.Fatalf("unsupported oracle %s", c.Oracle.Kind)
		}
		args := make([]uint64, len(c.Args))
		for i, v := range c.Args {
			args[i], e = strconv.ParseUint(string(v), 10, 64)
			if e != nil {
				t.Fatal(e)
			}
		}
		expected := make([]uint64, len(c.Oracle.Expected))
		for i, v := range c.Oracle.Expected {
			expected[i], e = strconv.ParseUint(v, 10, 64)
			if e != nil {
				t.Fatal(e)
			}
		}
		fn, e := in.WasmFunc(c.Export)
		if e != nil {
			t.Fatal(e)
		}
		invoke = func() error {
			got, e := fn.Invoke(args...)
			if e != nil {
				return e
			}
			if !slices.Equal(got, expected) {
				return fmt.Errorf("result %v want %v", got, expected)
			}
			base := uint32(0)
			if c.Oracle.OutputPointerExport != "" {
				got, e = in.Invoke(c.Oracle.OutputPointerExport)
				if e != nil {
					return e
				}
				base = uint32(got[0])
			}
			for _, region := range c.Oracle.Memory {
				want, e := hex.DecodeString(region.Hex)
				if e != nil {
					return e
				}
				got, ok := in.Read(base+region.Offset, uint32(len(want)))
				if !ok || !bytes.Equal(got, want) {
					return fmt.Errorf("memory oracle mismatch at %d", base+region.Offset)
				}
			}
			return nil
		}
	}
	for i := 0; i < 3; i++ {
		if err = invoke(); err != nil {
			t.Fatalf("warmup %d: %v", i, err)
		}
	}
	target := 30 * time.Millisecond
	if d := os.Getenv("WAGO_STUDY_EXEC_TIME"); d != "" {
		target, err = time.ParseDuration(d)
		if err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	started = time.Now()
	for {
		if err = invoke(); err != nil {
			t.Fatal(err)
		}
		count++
		if time.Since(started) >= target {
			break
		}
	}
	execNS := float64(time.Since(started).Nanoseconds()) / float64(count)
	out := map[string]any{"id": m.ID, "optimization": opt, "on": enabled, "sha256": entry.SHA256, "go": runtime.Version(), "compile_ns": compileNS, "startup_peak_rss_bytes": startup, "compile_peak_rss_bytes": peak, "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs, "retained_heap_bytes": retained.HeapAlloc, "native_code_bytes": code, "exec_ns": execNS, "exec_calls": count, "oracle": "pass"}
	raw, err = json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("STUDY_SAMPLE " + string(raw))
}
