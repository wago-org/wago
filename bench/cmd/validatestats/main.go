// Command validatestats measures byte-backed validate wall-clock latency over
// repeated runs.
//
// It reports average, median, and max duration for the current public validator
// path (DecodeModule + ValidateModule). Unlike `go test -bench`, these are
// per-run wall times intended for quick before/after validation-performance
// checks.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	wasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

const corpusDir = "corpus"

type execEntry struct {
	Export string  `json:"export"`
	Args   []int32 `json:"args"`
}

type corpusModule struct {
	File     string      `json:"file"`
	Path     string      `json:"path"`
	Category string      `json:"category"`
	Desc     string      `json:"desc"`
	Stages   []string    `json:"stages"`
	Init     string      `json:"init"`
	Exec     []execEntry `json:"exec"`

	bytes []byte
	avail bool
}

type manifest struct {
	Modules []corpusModule `json:"modules"`
}

type result struct {
	module    string
	runs      int
	avg       time.Duration
	median    time.Duration
	max       time.Duration
	durations []time.Duration
}

func main() {
	runs := flag.Int("runs", 20, "measured runs per module")
	warmup := flag.Int("warmup", 3, "unmeasured warmup runs per module")
	fileFlag := flag.String("file", "", "optional wasm file to measure instead of bench corpus")
	flag.Parse()

	if *runs <= 0 {
		fatalf("-runs must be > 0")
	}
	if *warmup < 0 {
		fatalf("-warmup must be >= 0")
	}

	mods, err := modules(*fileFlag)
	if err != nil {
		fatalf("%v", err)
	}

	fmt.Printf("byte-backed validate wall time: runs=%d warmup=%d modules=%d\n", *runs, *warmup, len(mods))
	fmt.Printf("%-32s %6s %12s %12s %12s\n", "module", "runs", "avg", "median", "max")
	fmt.Printf("%-32s %6s %12s %12s %12s\n", strings.Repeat("-", 32), strings.Repeat("-", 6), strings.Repeat("-", 12), strings.Repeat("-", 12), strings.Repeat("-", 12))

	moduleAvgs := make([]time.Duration, 0, len(mods))
	var corpusRunTotals []time.Duration
	if len(mods) > 1 {
		corpusRunTotals = make([]time.Duration, *runs)
	}
	for _, mod := range mods {
		res, err := measure(mod, *runs, *warmup)
		if err != nil {
			fatalf("%s: %v", mod.name(), err)
		}
		printResult(res)
		moduleAvgs = append(moduleAvgs, res.avg)
		for i, d := range res.durations {
			corpusRunTotals[i] += d
		}
	}

	if len(mods) > 1 {
		fmt.Printf("%-32s %6s %12s %12s %12s\n", strings.Repeat("-", 32), strings.Repeat("-", 6), strings.Repeat("-", 12), strings.Repeat("-", 12), strings.Repeat("-", 12))
		s := summarize(moduleAvgs)
		printResult(result{module: "MEAN(module avg)", runs: len(moduleAvgs), avg: s.avg, median: s.median, max: s.max})
		s = summarize(corpusRunTotals)
		printResult(result{module: "CORPUS(sum/run)", runs: *runs, avg: s.avg, median: s.median, max: s.max})
	}
}

func modules(file string) ([]corpusModule, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		return []corpusModule{{File: filepath.Base(file), bytes: b, avail: true}}, nil
	}

	mods := append(readManifest("manifest.json"), readManifest("isa-manifest.json")...)
	out := mods[:0]
	for i := range mods {
		mod := &mods[i]
		if !mod.supports("Validate") {
			continue
		}
		path := filepath.Join(corpusDir, mod.File)
		if mod.Path != "" {
			path = mod.Path
		}
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			mod.bytes = b
			mod.avail = true
			out = append(out, *mod)
		case mod.Path != "":
			fmt.Fprintf(os.Stderr, "corpus: %s not present (%s), skipping\n", mod.File, mod.Path)
		default:
			return nil, fmt.Errorf("read %s: %w", mod.File, err)
		}
	}
	return out, nil
}

func readManifest(file string) []corpusModule {
	raw, err := os.ReadFile(filepath.Join(corpusDir, file))
	if err != nil {
		fatalf("read %s: %v", file, err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		fatalf("parse %s: %v", file, err)
	}
	return m.Modules
}

func (m corpusModule) supports(stage string) bool {
	if len(m.Stages) == 0 {
		return true
	}
	for _, s := range m.Stages {
		if s == stage {
			return true
		}
	}
	return false
}

func (m corpusModule) name() string {
	base := m.File
	if base == "" {
		base = filepath.Base(m.Path)
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func validateBytes(b []byte) error {
	m, err := wasm.DecodeModule(b)
	if err != nil {
		return err
	}
	return wasm.ValidateModule(m)
}

func measure(mod corpusModule, runs, warmup int) (result, error) {
	for i := 0; i < warmup; i++ {
		if err := validateBytes(mod.bytes); err != nil {
			return result{}, fmt.Errorf("warmup %d: %w", i+1, err)
		}
	}

	durations := make([]time.Duration, runs)
	for i := 0; i < runs; i++ {
		start := time.Now()
		if err := validateBytes(mod.bytes); err != nil {
			return result{}, fmt.Errorf("run %d: %w", i+1, err)
		}
		durations[i] = time.Since(start)
	}
	s := summarize(durations)
	return result{module: mod.name(), runs: runs, avg: s.avg, median: s.median, max: s.max, durations: durations}, nil
}

type summary struct {
	avg    time.Duration
	median time.Duration
	max    time.Duration
}

func summarize(durations []time.Duration) summary {
	if len(durations) == 0 {
		return summary{}
	}
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var sum time.Duration
	var max time.Duration
	for _, d := range durations {
		sum += d
		if d > max {
			max = d
		}
	}
	median := sorted[len(sorted)/2]
	if len(sorted)%2 == 0 {
		median = (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
	}
	return summary{avg: sum / time.Duration(len(durations)), median: median, max: max}
}

func printResult(r result) {
	fmt.Printf("%-32s %6d %12s %12s %12s\n", r.module, r.runs, r.avg, r.median, r.max)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
