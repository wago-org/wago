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

// defaultWarpHarness is the WARP comparison binary (relative to bench/), built
// from warp/bench/main.cpp via cmake — it takes real i32 args and times a proper
// exec loop. See warp/build-bench.sh.
const defaultWarpHarness = "../warp/build-bench/bin/vb_bench"

// stampPath (bench-relative — benchpub runs with cwd=bench/) records the commit
// the last published/charted numbers reflect and the wall-clock time benchpub
// produced them, so staleness against HEAD is detectable without re-reading
// history.json. Local artifact — gitignored, like .bench-run.txt.
const stampPath = ".bench-stamp"

// stamp is the on-disk staleness marker written by writeStamp.
type stamp struct {
	Commit string `json:"commit"` // short commit the numbers reflect
	Dirty  bool   `json:"dirty"`  // working tree had uncommitted changes at run time
	RanAt  string `json:"ranAt"`  // RFC3339 wall-clock time benchpub ran
}

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
	warp := flag.String("warp", "", "WARP harness path for the comparison; \"auto\" uses the cmake-built vb_bench; empty skips")
	base := flag.String("base", "", "load this bench.json as the run and skip the suite (only re-collect WARP and re-render)")
	warpRun := flag.Bool("warp-run", false, "run the WARP harness over the corpus, print the numbers, and exit (no charts/publish)")
	flag.Parse()

	if *warpRun {
		h := *warp
		if h == "" {
			h = "auto"
		}
		runWarpOnly(h)
		return
	}

	var run Run
	switch {
	case *base != "":
		run = loadRun(*base) // reuse an existing suite run; just (re)collect WARP + re-render
	case *in != "":
		b, err := os.ReadFile(*in)
		must(err)
		text := string(b)
		run = parseRun(text)
		// Attribute the run to the commit stamped in the capture header (the
		// commit the numbers actually reflect), not current HEAD.
		gitInfoFromCapture(&run, captureCommit(text))
	default:
		run = parseRun(runSuite(*benchtime, *count))
		gitInfo(&run)
	}

	// Best-effort: never abort on stale/empty results — regenerate what we can
	// and warn. The stamp records what the current numbers reflect so staleness
	// against HEAD is detectable later (by benchpub and by `make`).
	if len(run.Metrics) == 0 {
		fmt.Println("benchpub: WARNING no benchmark results parsed; benches were not updated")
	}
	head, dirty := headState()
	fresh := *in == "" && *base == "" // a real suite run happened this invocation
	warnIfStale(run.Commit, head, dirty && fresh)
	writeStamp(run.Commit, dirty && fresh)

	cor := readCorpus()
	run.Modules = map[string]ModuleInfo{}
	for _, c := range cor {
		run.Modules[c.Name] = ModuleInfo{Category: c.Category, Bytes: c.Bytes}
	}
	if *warp != "" {
		collectWarp(&run, cor, *warp)
	}

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
	// -timeout 0 disables go test's default 10-minute cap: the full corpus at
	// count>1 (a 9 MB module decoded/validated repeatedly, plus wazero) easily
	// runs longer, and a timeout kills the whole binary mid-run.
	args := []string{"test", "-run", "^$", "-bench", suiteRegex, "-benchmem",
		"-timeout", "0", "-benchtime", benchtime, "-count", strconv.Itoa(count), "."}
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
	// `go test` emits a bare container line (e.g. "BenchmarkDecode") for a suite
	// stage whose sub-benchmarks were all filtered out; that would land as a bogus
	// module-less metric ("Decode") next to the real "Decode/<module>" entries.
	// Drop a slash-less name when a "name/..." child exists. Genuine top-level
	// metrics (the "Compile_wago"/"Compile_wazero" comparisons) have no children
	// and are kept.
	hasChild := map[string]bool{}
	for name := range samples {
		if i := strings.IndexByte(name, '/'); i >= 0 {
			hasChild[name[:i]] = true
		}
	}
	for name, s := range samples {
		if !strings.Contains(name, "/") && hasChild[name] {
			continue
		}
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
	return Metric{Ns: medianFloat(ns), Bytes: medianInt(by), Allocs: medianInt(al)}
}

// medianFloat/medianInt return the true median of a sorted slice, averaging the
// two middle elements for an even length (e.g. the default -count=6).
func medianFloat(x []float64) float64 {
	n := len(x)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return x[n/2]
	}
	return (x[n/2-1] + x[n/2]) / 2
}

func medianInt(x []int64) int64 {
	n := len(x)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return x[n/2]
	}
	return (x[n/2-1] + x[n/2]) / 2
}

type execSpec struct {
	Export string
	Args   []int32
}

// corpusEntry is one manifest module with the bits benchpub needs: its on-disk
// wasm path (for WARP), size, category, and exec entry points with args.
type corpusEntry struct {
	Name, WasmPath, Category string
	Bytes                    int64
	Execs                    []execSpec
}

// readCorpus reads the manifests for module metadata. Best-effort: nil on error.
// The hand-maintained manifest.json and the generated isa-manifest.json share
// the schema; both are read so WARP benches the ISA micro-suite too.
func readCorpus() []corpusEntry {
	var out []corpusEntry
	for _, file := range []string{"manifest.json", "isa-manifest.json"} {
		raw, err := os.ReadFile(filepath.Join("corpus", file))
		if err != nil {
			continue
		}
		var m struct {
			Modules []struct {
				File, Path, Category string
				Exec                 []execSpec
			} `json:"modules"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		for _, mod := range m.Modules {
			path := filepath.Join("corpus", mod.File)
			if mod.Path != "" {
				path = mod.Path
			}
			var b int64
			if fi, err := os.Stat(path); err == nil {
				b = fi.Size()
			}
			out = append(out, corpusEntry{
				Name: strings.TrimSuffix(mod.File, ".wasm"), WasmPath: path,
				Category: mod.Category, Bytes: b, Execs: mod.Exec,
			})
		}
	}
	return out
}

var (
	warpCompileRe = regexp.MustCompile(`compile_ms=([0-9.eE+-]+)`)
	warpExecRe    = regexp.MustCompile(`exec_ns=([0-9.eE+-]+)`)
)

// collectWarp shells out to the WARP harness (vb_bench) for each exec entry,
// passing the manifest's real i32 args, and records WarpCompile/<module> (ns)
// and WarpExec/<module>.<export> (ns). Best-effort: modules the harness can't
// compile/run are skipped.
func collectWarp(run *Run, cor []corpusEntry, harness string) {
	if harness == "auto" {
		harness = defaultWarpHarness
	}
	if _, err := os.Stat(harness); err != nil {
		fmt.Printf("benchpub: WARP harness not found (%s); skipping WARP\n", harness)
		return
	}
	nc, ne := 0, 0
	for _, c := range cor {
		if c.Bytes == 0 {
			continue // referenced file absent
		}
		compileDone := false
		for _, e := range c.Execs {
			argv := []string{c.WasmPath, e.Export}
			for _, a := range e.Args {
				argv = append(argv, strconv.Itoa(int(a)))
			}
			out, _ := exec.Command(harness, argv...).Output()
			mc := warpCompileRe.FindSubmatch(out)
			if mc == nil {
				continue // harness couldn't compile/run this export
			}
			if !compileDone {
				if ms, err := strconv.ParseFloat(string(mc[1]), 64); err == nil {
					run.Metrics["WarpCompile/"+c.Name] = Metric{Ns: ms * 1e6}
					compileDone = true
					nc++
				}
			}
			if me := warpExecRe.FindSubmatch(out); me != nil {
				if ns, err := strconv.ParseFloat(string(me[1]), 64); err == nil {
					run.Metrics["WarpExec/"+c.Name+"."+e.Export] = Metric{Ns: ns}
					ne++
				}
			}
		}
	}
	fmt.Printf("benchpub: WARP compile for %d module(s), exec for %d export(s)\n", nc, ne)
}

// runWarpOnly builds the corpus, runs the WARP harness over it, and prints the
// per-module compile/exec numbers (ns) to stdout — no history, JSON, or charts.
// Backs `make bench-warp`.
func runWarpOnly(harness string) {
	cor := readCorpus()
	run := Run{Metrics: map[string]Metric{}}
	collectWarp(&run, cor, harness)
	keys := make([]string, 0, len(run.Metrics))
	for k := range run.Metrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%-56s %14.1f ns\n", k, run.Metrics[k].Ns)
	}
}

// captureCommit extracts the "# git <hash>" stamp that `make bench` writes as
// the first line of a capture file, or "" if the capture carries no stamp.
func captureCommit(text string) string {
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if h := strings.TrimPrefix(ln, "# git "); h != ln {
			return strings.TrimSpace(h)
		}
		return "" // first non-blank line isn't the stamp
	}
	return ""
}

// gitInfoFromCapture stamps the run with the commit recorded in the capture
// header — the commit the numbers actually reflect — rather than current HEAD.
// Falls back to HEAD when the capture carries no stamp.
func gitInfoFromCapture(run *Run, captured string) {
	if captured == "" {
		gitInfo(run)
		return
	}
	run.Commit = short(captured)
	run.Version = strings.TrimSpace(git("describe", "--tags", "--always", captured))
	if run.Version == "" {
		run.Version = run.Commit
	}
	date := strings.TrimSpace(git("show", "-s", "--format=%cI", captured))
	if date == "" {
		date = time.Now().Format(time.RFC3339)
	}
	run.Date = date
}

// headState returns HEAD's short hash and whether the working tree is dirty.
func headState() (commit string, dirty bool) {
	commit = short(strings.TrimSpace(git("rev-parse", "HEAD")))
	dirty = strings.HasSuffix(strings.TrimSpace(git("describe", "--always", "--dirty")), "-dirty")
	return
}

// writeStamp records the commit the run's numbers reflect and the wall-clock
// time benchpub produced them. Best-effort — a write failure is not fatal.
func writeStamp(numbersCommit string, dirty bool) {
	s := stamp{Commit: short(numbersCommit), Dirty: dirty, RanAt: time.Now().Format(time.RFC3339)}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(stampPath, append(b, '\n'), 0o644)
}

// warnIfStale prints a best-effort warning when the numbers being published
// reflect a commit other than HEAD (or a dirty tree). It never fails the run.
func warnIfStale(numbersCommit, headCommit string, dirty bool) {
	switch {
	case numbersCommit == "":
		fmt.Println("benchpub: WARNING no commit recorded for these numbers; staleness unknown")
	case short(numbersCommit) != short(headCommit):
		fmt.Printf("benchpub: WARNING benches are stale — numbers reflect %s but HEAD is %s; run 'make bench' to regenerate\n",
			short(numbersCommit), short(headCommit))
	case dirty:
		fmt.Printf("benchpub: WARNING working tree is dirty at %s; benches may not reflect uncommitted changes\n", short(headCommit))
	}
}

// short truncates a hash to its 7-character prefix for display/comparison.
func short(h string) string {
	h = strings.TrimSpace(h)
	if len(h) > 7 {
		return h[:7]
	}
	return h
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

// loadRun reads a bench.json written by a previous run (used by -base to add
// WARP/re-render without re-running the suite).
func loadRun(path string) Run {
	b, err := os.ReadFile(path)
	must(err)
	var r Run
	must(json.Unmarshal(b, &r))
	if r.Metrics == nil {
		r.Metrics = map[string]Metric{}
	}
	return r
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
