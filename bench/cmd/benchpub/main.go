// Command benchpub runs the wago benchmark suite (or reads a saved `go test
// -bench` output), records the results as a versioned JSON run appended to a
// rolling history, and renders charts: per-stage latency for the latest run and
// per-stage trends across versions. It is the data side of "perf over time".
//
// Typical use is via scripts/publish-bench.sh, which points -history at a clone
// of the docs repo so runs accumulate. Standalone:
//
//	cd bench && go run ./cmd/benchpub -out out            # run + render locally
//	go run ./cmd/benchpub -in saved.txt -out out          # chart a saved run
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// suiteRegex selects the wago stage suite plus the cross-engine wazero
// benchmarks (compare_test.go). The fixed wago-vs-wazero set in bench_test.go is
// excluded — these fan out over the same corpus as the wago stages.
const suiteRegex = `^(BenchmarkDecode|BenchmarkValidate|BenchmarkCompile|BenchmarkCompileFull|BenchmarkInstantiate|BenchmarkExec|BenchmarkWazeroCompile|BenchmarkWazeroExec)$`

// defaultWarpHarness is the prebuilt WARP comparison binary (relative to bench/).
const defaultWarpHarness = "../warp/bazel-bin/wagobench_warp/harness"

// stageOrder fixes chart/JSON ordering and is the canonical pipeline sequence.
var stageOrder = []string{"Decode", "Validate", "Compile", "CompileFull", "Instantiate", "Exec"}

// Metric is one benchmark's central result.
type Metric struct {
	Ns     float64 `json:"ns"`
	Bytes  int64   `json:"bytes"`
	Allocs int64   `json:"allocs"`
}

// ModuleInfo is corpus metadata for one module, recorded so charts can select
// and label modules (e.g. the real-world subset) without re-reading the manifest.
type ModuleInfo struct {
	Category string `json:"category"`
	Bytes    int64  `json:"bytes"` // wasm file size
}

// Run is one version's full result set.
type Run struct {
	Version string                `json:"version"`
	Commit  string                `json:"commit"`
	Date    string                `json:"date"` // ISO-8601, commit date
	Goos    string                `json:"goos"`
	Goarch  string                `json:"goarch"`
	CPU     string                `json:"cpu"`
	Modules map[string]ModuleInfo `json:"modules,omitempty"` // module -> corpus metadata
	Metrics map[string]Metric     `json:"metrics"`           // "Stage/key" -> result
}

// History is the rolling time series, oldest first.
type History struct {
	Runs []Run `json:"runs"`
}

func main() {
	in := flag.String("in", "", "read `go test -bench` output from this file instead of running the suite")
	out := flag.String("out", "bench-out", "output directory for bench.json/history.json and charts")
	historyPath := flag.String("history", "", "existing history.json to read and append to (defaults to <out>/history.json)")
	benchtime := flag.String("benchtime", "1s", "benchtime for the suite run")
	count := flag.Int("count", 6, "count for the suite run (median is taken)")
	warp := flag.String("warp", "", "WARP harness path for compile-time comparison; \"auto\" uses the prebuilt one; empty skips")
	flag.Parse()

	var raw string
	if *in != "" {
		b, err := os.ReadFile(*in)
		must(err)
		raw = string(b)
	} else {
		raw = runSuite(*benchtime, *count)
	}

	run := parseRun(raw)
	cor := readCorpus()
	run.Modules = map[string]ModuleInfo{}
	for _, c := range cor {
		run.Modules[c.Name] = ModuleInfo{Category: c.Category, Bytes: c.Bytes}
	}
	if *warp != "" {
		collectWarp(&run, cor, *warp)
	}
	gitInfo(&run)

	hp := *historyPath
	if hp == "" {
		hp = filepath.Join(*out, "history.json")
	}
	hist := loadHistory(hp)
	hist.upsert(run)

	must(os.MkdirAll(filepath.Join(*out, "charts"), 0o755))
	writeJSON(filepath.Join(*out, "bench.json"), run)
	writeJSON(filepath.Join(*out, "history.json"), hist)
	renderCharts(*out, run, hist)

	fmt.Printf("benchpub: %s @ %s — %d benchmarks, %d runs in history\n",
		run.Version, run.Date[:10], len(run.Metrics), len(hist.Runs))
	fmt.Printf("benchpub: wrote %s/{bench.json,history.json,charts/*.svg}\n", *out)
}

func runSuite(benchtime string, count int) string {
	args := []string{"test", "-run", "^$", "-bench", suiteRegex, "-benchmem",
		"-benchtime", benchtime, "-count", strconv.Itoa(count), "."}
	cmd := exec.Command("go", args...)
	fmt.Printf("benchpub: running suite (benchtime=%s count=%d)...\n", benchtime, count)
	// CombinedOutput so a build error or panic is captured too. A single flaky
	// benchmark (a b.Fatal) makes `go test` exit non-zero but the benchmarks that
	// already printed results are still in the output — parse those rather than
	// discarding the whole run; surface the failures so they aren't silent.
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("benchpub: WARNING go test exited non-zero (%v); using completed benchmarks. Failures:\n", err)
		for _, ln := range strings.Split(string(out), "\n") {
			t := strings.TrimSpace(ln)
			if strings.HasPrefix(t, "---") || strings.HasPrefix(t, "panic") || (strings.Contains(t, "FAIL") && !strings.HasPrefix(t, "Benchmark")) {
				fmt.Println("   ", t)
			}
		}
	}
	return string(out)
}

var (
	headerRe = regexp.MustCompile(`^(goos|goarch|cpu):\s*(.+)$`)
	benchRe  = regexp.MustCompile(`^Benchmark(\S+?)-\d+\s+\d+\s+(.+)$`)
)

// parseRun parses `go test -bench -benchmem` output into a Run, taking the
// median over repeated samples (count>1) per benchmark.
func parseRun(text string) Run {
	run := Run{Metrics: map[string]Metric{}}
	samples := map[string][]Metric{} // name -> samples

	for _, line := range strings.Split(text, "\n") {
		if m := headerRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			switch m[1] {
			case "goos":
				run.Goos = m[2]
			case "goarch":
				run.Goarch = m[2]
			case "cpu":
				run.CPU = m[2]
			}
			continue
		}
		m := benchRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		name := normalizeName(m[1])
		met, ok := parseMetrics(m[2])
		if !ok {
			continue
		}
		samples[name] = append(samples[name], met)
	}
	for name, s := range samples {
		run.Metrics[name] = median(s)
	}
	return run
}

// normalizeName turns "Decode/tiny" / "Exec/fib_rec.fib" into the stable
// "Stage/key" form used as the metric key (the leading function name's
// "Benchmark" is already stripped by benchRe).
func normalizeName(n string) string { return n }

// parseMetrics reads the "X ns/op Y B/op Z allocs/op" tail of a bench line.
func parseMetrics(tail string) (Metric, bool) {
	f := strings.Fields(tail)
	var met Metric
	gotNs := false
	for i := 0; i+1 < len(f); i++ {
		v, err := strconv.ParseFloat(f[i], 64)
		if err != nil {
			continue
		}
		switch f[i+1] {
		case "ns/op":
			met.Ns = v
			gotNs = true
		case "B/op":
			met.Bytes = int64(v)
		case "allocs/op":
			met.Allocs = int64(v)
		}
	}
	return met, gotNs
}

func median(s []Metric) Metric {
	ns := make([]float64, len(s))
	by := make([]int64, len(s))
	al := make([]int64, len(s))
	for i, m := range s {
		ns[i], by[i], al[i] = m.Ns, m.Bytes, m.Allocs
	}
	sort.Float64s(ns)
	sort.Slice(by, func(i, j int) bool { return by[i] < by[j] })
	sort.Slice(al, func(i, j int) bool { return al[i] < al[j] })
	mid := len(s) / 2
	return Metric{Ns: ns[mid], Bytes: by[mid], Allocs: al[mid]}
}

// corpusEntry is one manifest module with the bits benchpub needs: its on-disk
// wasm path (for WARP), size, category, and exec export names.
type corpusEntry struct {
	Name, WasmPath, Category string
	Bytes                    int64
	Execs                    []string
}

// readCorpus reads the manifest for module metadata. Best-effort: nil on error.
func readCorpus() []corpusEntry {
	raw, err := os.ReadFile(filepath.Join("corpus", "manifest.json"))
	if err != nil {
		return nil
	}
	var m struct {
		Modules []struct {
			File, Path, Category string
			Exec                 []struct{ Export string }
		} `json:"modules"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	var out []corpusEntry
	for _, mod := range m.Modules {
		path := filepath.Join("corpus", mod.File)
		if mod.Path != "" {
			path = mod.Path
		}
		var b int64
		if fi, err := os.Stat(path); err == nil {
			b = fi.Size()
		}
		var ex []string
		for _, e := range mod.Exec {
			ex = append(ex, e.Export)
		}
		out = append(out, corpusEntry{
			Name: strings.TrimSuffix(mod.File, ".wasm"), WasmPath: path,
			Category: mod.Category, Bytes: b, Execs: ex,
		})
	}
	return out
}

var warpCompileRe = regexp.MustCompile(`compile_ms=([0-9.eE+-]+)`)

// collectWarp shells out to the prebuilt WARP harness for a compile-time
// comparison, recording WarpCompile/<module> (ns). The harness requires an
// exported function (it compiles then runs it), so coverage is limited to
// modules with a matching exec entry; failures are skipped. Best-effort.
func collectWarp(run *Run, cor []corpusEntry, harness string) {
	if harness == "auto" {
		harness = defaultWarpHarness
	}
	if _, err := os.Stat(harness); err != nil {
		fmt.Printf("benchpub: WARP harness not found (%s); skipping WARP\n", harness)
		return
	}
	n := 0
	for _, c := range cor {
		if c.Bytes == 0 {
			continue // referenced file absent
		}
		for _, ex := range c.Execs {
			out, _ := exec.Command(harness, c.WasmPath, ex).Output()
			if mm := warpCompileRe.FindSubmatch(out); mm != nil {
				if ms, err := strconv.ParseFloat(string(mm[1]), 64); err == nil {
					run.Metrics["WarpCompile/"+c.Name] = Metric{Ns: ms * 1e6}
					n++
					break
				}
			}
		}
	}
	fmt.Printf("benchpub: WARP compile times collected for %d module(s)\n", n)
}

func gitInfo(run *Run) {
	run.Version = strings.TrimSpace(git("describe", "--tags", "--always", "--dirty"))
	run.Commit = strings.TrimSpace(git("rev-parse", "--short", "HEAD"))
	date := strings.TrimSpace(git("show", "-s", "--format=%cI", "HEAD"))
	if date == "" {
		date = time.Now().Format(time.RFC3339)
	}
	run.Date = date
	if run.Version == "" {
		run.Version = run.Commit
	}
}

func git(args ...string) string {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func loadHistory(path string) History {
	var h History
	b, err := os.ReadFile(path)
	if err != nil {
		return h
	}
	_ = json.Unmarshal(b, &h)
	return h
}

// upsert replaces any run with the same commit (a re-run) and keeps the series
// ordered oldest-first by date.
func (h *History) upsert(run Run) {
	out := h.Runs[:0]
	for _, r := range h.Runs {
		if r.Commit != run.Commit {
			out = append(out, r)
		}
	}
	out = append(out, run)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	h.Runs = out
}

func writeJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	must(err)
	must(os.WriteFile(path, append(b, '\n'), 0o644))
}

// stageOf splits a metric key "Stage/key" into (stage, key).
func stageOf(name string) (stage, key string) {
	if i := strings.IndexByte(name, '/'); i >= 0 {
		return name[:i], name[i+1:]
	}
	return name, name
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "benchpub:", err)
		os.Exit(1)
	}
}
