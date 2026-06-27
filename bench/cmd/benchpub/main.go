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

// suiteRegex selects exactly the stage suite (excludes the wago-vs-wazero
// benchmarks in bench_test.go, which are a separate comparison).
const suiteRegex = `^(BenchmarkDecode|BenchmarkValidate|BenchmarkCompile|BenchmarkCompileFull|BenchmarkInstantiate|BenchmarkExec)$`

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
	run.Modules = readModuleInfo()
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
	cmd.Stderr = os.Stderr
	fmt.Printf("benchpub: running suite (benchtime=%s count=%d)...\n", benchtime, count)
	b, err := cmd.Output()
	must(err)
	return string(b)
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

// readModuleInfo reads the corpus manifest for module categories and file sizes.
// Best-effort: returns nil if the manifest can't be read.
func readModuleInfo() map[string]ModuleInfo {
	raw, err := os.ReadFile(filepath.Join("corpus", "manifest.json"))
	if err != nil {
		return nil
	}
	var m struct {
		Modules []struct {
			File, Path, Category string
		} `json:"modules"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	out := map[string]ModuleInfo{}
	for _, mod := range m.Modules {
		name := strings.TrimSuffix(mod.File, ".wasm")
		path := filepath.Join("corpus", mod.File)
		if mod.Path != "" {
			path = mod.Path
		}
		var b int64
		if fi, err := os.Stat(path); err == nil {
			b = fi.Size()
		}
		out[name] = ModuleInfo{Category: mod.Category, Bytes: b}
	}
	return out
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
