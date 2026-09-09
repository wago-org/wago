// Package wagobench benchmarks the wago pipeline stage-by-stage across the
// curated corpus in ../../corpus/catalog.json. Each stage is a
// separate top-level Benchmark so it can be filtered (e.g. -bench Compile), and
// fans out over the corpus via b.Run so results read as Stage/<module>. This is
// wago-only (no wazero) so the numbers track wago's own performance over time.
package wagobench

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wago "github.com/wago-org/wago"
	wasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

const corpusDir = "../../corpus"

var corpusSelector = flag.String("wago.corpus", "quick", "corpus profile, tag:<tag>, all, or comma-separated benchmark IDs")
var includeOptimizationAblations = flag.Bool("wago.bench.optimization-ablation", false, "benchmark large modules with each enabled optimization disabled in turn")

type commandEntry struct {
	Runtime      string            `json:"runtime"` // core or wasi; command runs in a fresh instance
	Export       string            `json:"export"`
	Platforms    []string          `json:"platforms"` // optional GOOS/GOARCH allowlist
	Args         []string          `json:"args"`
	Stdin        string            `json:"stdin"`   // optional path relative to corpus/
	Preopen      string            `json:"preopen"` // optional host directory relative to corpus/, mounted at /
	Inputs       map[string]string `json:"inputs"`  // relative path -> SHA-256 for every preopened input file
	Want         []uint64          `json:"want"`    // optional exact function results
	StdoutSHA256 string            `json:"stdout_sha256"`
	StderrSHA256 string            `json:"stderr_sha256"`
	Oracle       string            `json:"oracle"` // self-check or return; hashes are exact stream oracles
}

type execEntry struct {
	Export string   `json:"export"`
	Args   []int32  `json:"args"`
	Want   []uint64 `json:"want"`
}

type corpusModule struct {
	ID             string        `json:"id"`
	Artifact       string        `json:"artifact"`
	ArtifactSHA256 string        `json:"artifact_sha256"`
	Tags           []string      `json:"tags"`
	Suite          string        `json:"suite"` // optional upstream corpus name
	Desc           string        `json:"desc"`
	Stages         []string      `json:"stages"` // optional: stages this module supports (default: all)
	Init           string        `json:"init"`   // optional: export to call once after instantiate, before exec (e.g. AssemblyScript's _initialize; wago has no start section)
	Exec           []execEntry   `json:"exec"`
	SemanticExec   []string      `json:"semantic_exec"` // exact case IDs from catalog.json checks
	Command        *commandEntry `json:"command"`       // optional one-shot command/replay workload

	bytes []byte
}

// supports reports whether the module should be benchmarked at the given stage.
// An empty Stages list means every stage.
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

type catalog struct {
	Schema     int                 `json:"schema"`
	Profiles   map[string][]string `json:"profiles"`
	Checks     []json.RawMessage   `json:"checks"`
	Benchmarks []corpusModule      `json:"benchmarks"`
}

var (
	corpusOnce sync.Once
	corpus     []corpusModule
)

func loadCorpus(tb testing.TB) []corpusModule {
	corpusOnce.Do(func() {
		corpus = readCatalog(tb)
	})
	return corpus
}

// readCatalog loads, validates, selects, and resolves benchmark artifacts.
func readCatalog(tb testing.TB) []corpusModule {
	tb.Helper()
	file := filepath.Join(corpusDir, "catalog.json")
	raw, err := os.ReadFile(file)
	if err != nil {
		tb.Fatalf("read corpus catalog: %v", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var c catalog
	if err := dec.Decode(&c); err != nil {
		tb.Fatalf("parse corpus catalog: %v", err)
	}
	if c.Schema != 1 {
		tb.Fatalf("corpus catalog schema = %d, want 1", c.Schema)
	}
	selected := selectedIDs(tb, c, *corpusSelector)
	seen := make(map[string]bool, len(c.Benchmarks))
	var modules []corpusModule
	for i := range c.Benchmarks {
		mod := &c.Benchmarks[i]
		if mod.ID == "" || mod.Artifact == "" || mod.ArtifactSHA256 == "" {
			tb.Fatalf("corpus benchmark %d: id, artifact, and artifact_sha256 are required", i)
		}
		if seen[mod.ID] {
			tb.Fatalf("duplicate corpus benchmark id %q", mod.ID)
		}
		seen[mod.ID] = true
		if !selected[mod.ID] && !selectedByTag(*corpusSelector, mod.Tags) {
			continue
		}
		path := filepath.Join(corpusDir, filepath.FromSlash(mod.Artifact))
		b, err := os.ReadFile(path)
		if err != nil {
			tb.Fatalf("read corpus artifact %s: %v", mod.Artifact, err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(b)); got != mod.ArtifactSHA256 {
			tb.Fatalf("corpus artifact %s sha256 = %s, want %s", mod.Artifact, got, mod.ArtifactSHA256)
		}
		mod.bytes = b
		modules = append(modules, *mod)
	}
	for id := range selected {
		if !seen[id] {
			tb.Fatalf("corpus selector references unknown benchmark %q", id)
		}
	}
	if len(modules) == 0 {
		tb.Fatalf("corpus selector %q selected no benchmarks", *corpusSelector)
	}
	return modules
}

func selectedIDs(tb testing.TB, c catalog, selector string) map[string]bool {
	tb.Helper()
	if selector == "all" || strings.HasPrefix(selector, "tag:") {
		return map[string]bool{}
	}
	if ids, ok := c.Profiles[selector]; ok {
		return sliceSet(ids)
	}
	ids := strings.Split(selector, ",")
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			tb.Fatalf("invalid empty benchmark in corpus selector %q", selector)
		}
	}
	return sliceSet(ids)
}

func sliceSet(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[strings.TrimSpace(id)] = true
	}
	return out
}

func selectedByTag(selector string, tags []string) bool {
	return selector == "all" || strings.HasPrefix(selector, "tag:") && slices.Contains(tags, strings.TrimPrefix(selector, "tag:"))
}

func (m corpusModule) name() string { return m.ID }

// hostStubs supplies a no-op sync host function for every function import the
// module declares (e.g. AssemblyScript's multi-parameter env.abort, which never
// fires on valid input). Returns nil for import-free modules (the synthetic corpus).
func hostStubs(c *wago.Compiled) wago.Imports {
	if len(c.Imports) == 0 {
		return nil
	}
	im := make(wago.Imports, len(c.Imports))
	for _, name := range c.Imports {
		im[name] = wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {})
	}
	return im
}

// decoded returns a freshly decoded module (helper for the validate/compile
// stages, which time work downstream of decode).
func (m corpusModule) decoded(tb testing.TB) *wasm.Module {
	mod, err := wasm.DecodeModule(m.bytes)
	if err != nil {
		tb.Fatalf("%s decode: %v", m.name(), err)
	}
	return mod
}

func eachModule(b *testing.B, stage string, fn func(b *testing.B, m corpusModule)) {
	for _, m := range loadCorpus(b) {
		if !m.supports(stage) {
			continue
		}
		b.Run(m.name(), func(b *testing.B) {
			b.ReportAllocs()
			fn(b, m)
		})
	}
}

// BenchmarkDecode times the binary decode stage (bytes -> *Module).
func BenchmarkDecode(b *testing.B) {
	eachModule(b, "Decode", func(b *testing.B, m corpusModule) {
		for i := 0; i < b.N; i++ {
			if _, err := wasm.DecodeModule(m.bytes); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkValidate times type-checking/validation of an already-decoded module.
func BenchmarkValidate(b *testing.B) {
	eachModule(b, "Validate", func(b *testing.B, m corpusModule) {
		mod := m.decoded(b)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := wasm.ValidateModule(mod); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkValidateWorkers measures one decoded module's validation latency at
// forced function-worker counts. Module-level validation remains serial; only
// independent function bodies fan out. As with BenchmarkCompileWorkers, this is
// intra-module latency rather than multi-module throughput.
func BenchmarkValidateWorkers(b *testing.B) {
	wanted := map[string]bool{
		"tiny": true, "many_funcs": true, "json-as": true,
		"esbuild": true,
	}
	for _, m := range loadCorpus(b) {
		if !m.supports("Validate") || !wanted[m.name()] {
			continue
		}
		mod := m.decoded(b)
		b.Run(m.name(), func(b *testing.B) {
			for _, workers := range []int{1, 2, 4, 8} {
				b.Run(fmt.Sprintf("p%d", workers), func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if err := wasm.ValidateModuleWithWorkers(mod, workers); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}

// BenchmarkCompile times native codegen for an already decoded+validated module.
func BenchmarkCompile(b *testing.B) {
	eachModule(b, "Compile", func(b *testing.B, m corpusModule) {
		mod := m.decoded(b)
		if err := wasm.ValidateModule(mod); err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := benchCompileModule(mod); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkCompileCompact times serialized native codegen with the bounded
// native-compaction path enabled.
func BenchmarkCompileCompact(b *testing.B) {
	eachModule(b, "Compile", func(b *testing.B, m corpusModule) {
		mod := m.decoded(b)
		if err := wasm.ValidateModule(mod); err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := benchCompileModuleCompact(mod); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkCompileWorkers measures the latency of one backend module compile at
// forced worker counts. Decode and validation happen outside the timed loop.
// This intentionally does not use b.RunParallel: that would measure independent
// multi-module throughput rather than intra-module compile latency.
func BenchmarkCompileWorkers(b *testing.B) {
	wanted := map[string]bool{
		"tiny": true, "fib_rec": true, "many_funcs": true,
		"json-as": true, "blake-as": true, "esbuild": true,
	}
	for _, m := range loadCorpus(b) {
		if !m.supports("Compile") || !wanted[m.name()] {
			continue
		}
		mod := m.decoded(b)
		if err := wasm.ValidateModule(mod); err != nil {
			b.Fatalf("%s validate: %v", m.name(), err)
		}
		b.Run(m.name(), func(b *testing.B) {
			for _, workers := range []int{1, 2, 4, 8} {
				b.Run(fmt.Sprintf("p%d", workers), func(b *testing.B) {
					b.ReportAllocs()
					var cm *benchCompiledModule
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						var err error
						cm, err = benchCompileModuleWorkers(mod, workers)
						if err != nil {
							b.Fatal(err)
						}
					}
					b.StopTimer()
					if cm != nil {
						b.ReportMetric(float64(len(cm.Code)), "code-B")
					}
				})
			}
		})
	}
}

// BenchmarkCompileFull times the end-to-end decode+validate+compile entry point.
func BenchmarkCompileFull(b *testing.B) {
	eachModule(b, "CompileFull", func(b *testing.B, m corpusModule) {
		for i := 0; i < b.N; i++ {
			if _, err := wago.Compile(nil, m.bytes); err != nil {
				b.Fatal(err)
			}
		}
		b.StopTimer()
		compiled, err := wago.Compile(nil, m.bytes)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(compiled.CodeSize()), "code-B")
	})
}

// BenchmarkCompileFullOptimizationAblation attributes the compile-resource and
// generated-code cost of each enabled optimization. The matrix is intentionally
// opt-in: running every option across the real-module corpus is too expensive
// for ordinary benchmark and CI invocations.
func BenchmarkCompileFullOptimizationAblation(b *testing.B) {
	if !*includeOptimizationAblations {
		b.Skip("enable with -wago.bench.optimization-ablation")
	}
	wanted := map[string]bool{
		"json-as": true,
		"esbuild": true,
	}
	base := wago.NewRuntimeConfig().WithFunctionWorkers(1)
	infos := base.OptimizationInfos()
	for _, m := range loadCorpus(b) {
		if !m.supports("CompileFull") || !wanted[m.name()] {
			continue
		}
		b.Run(m.name(), func(b *testing.B) {
			run := func(b *testing.B, cfg *wago.RuntimeConfig) {
				b.ReportAllocs()
				var cm *wago.Compiled
				for i := 0; i < b.N; i++ {
					var err error
					cm, err = wago.Compile(cfg, m.bytes)
					if err != nil {
						b.Fatal(err)
					}
				}
				if cm != nil {
					b.ReportMetric(float64(cm.CodeSize()), "code-B")
				}
			}

			b.Run("default", func(b *testing.B) { run(b, base) })
			for _, info := range infos {
				if !info.On {
					continue
				}
				cfg := base.WithOptimization(info.Name, false)
				if cfg.Validate() != nil {
					continue
				}
				b.Run("no-"+info.Name, func(b *testing.B) {
					run(b, cfg)
				})
			}
		})
	}
}

// BenchmarkCompileFullWorkers measures the real public compile pipeline at
// forced worker maxima and in adaptive mode, including decode, validation,
// frontend checks, and complete backend codegen (including dynamic imports).
func BenchmarkCompileFullWorkers(b *testing.B) {
	wanted := map[string]bool{
		"tiny": true, "fib_rec": true, "many_funcs": true,
		"json-as": true, "blake-as": true, "esbuild": true,
	}
	for _, m := range loadCorpus(b) {
		if !m.supports("CompileFull") || !wanted[m.name()] {
			continue
		}
		b.Run(m.name(), func(b *testing.B) {
			for _, mode := range []struct {
				name    string
				workers int
			}{{"p1", 1}, {"p2", 2}, {"p4", 4}, {"p8", 8}, {"auto", 0}} {
				b.Run(mode.name, func(b *testing.B) {
					b.ReportAllocs()
					cfg := wago.NewRuntimeConfig().WithFunctionWorkers(mode.workers)
					var cm *wago.Compiled
					for i := 0; i < b.N; i++ {
						var err error
						cm, err = wago.Compile(cfg, m.bytes)
						if err != nil {
							b.Fatal(err)
						}
					}
					if cm != nil {
						b.ReportMetric(float64(cm.CodeSize()), "code-B")
					}
				})
			}
		})
	}
}

// BenchmarkCompileMultiModuleThroughput explicitly measures several independent
// full module compilations in parallel. Unlike BenchmarkCompileWorkers, this is
// a server-throughput/oversubscription benchmark, not single-module latency.
func BenchmarkCompileMultiModuleThroughput(b *testing.B) {
	wanted := map[string]bool{"many_funcs": true, "json-as": true, "esbuild": true}
	for _, m := range loadCorpus(b) {
		if !m.supports("CompileFull") || !wanted[m.name()] {
			continue
		}
		b.Run(m.name(), func(b *testing.B) {
			for _, mode := range []struct {
				name    string
				workers int
			}{{"p1", 1}, {"auto", 0}} {
				b.Run(mode.name, func(b *testing.B) {
					b.ReportAllocs()
					cfg := wago.NewRuntimeConfig().WithFunctionWorkers(mode.workers)
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							if _, err := wago.Compile(cfg, m.bytes); err != nil {
								b.Fatal(err)
							}
						}
					})
				})
			}
		})
	}
}

// BenchmarkInstantiate times instance setup for an already-compiled module.
func BenchmarkInstantiate(b *testing.B) {
	eachModule(b, "Instantiate", func(b *testing.B, m corpusModule) {
		c, err := wago.Compile(nil, m.bytes)
		if err != nil {
			b.Fatal(err)
		}
		imports := hostStubs(c)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
			if err != nil {
				b.Fatal(err)
			}
			in.Close()
		}
	})
}

// BenchmarkExec times the host->wasm call for each module's manifest exec
// entries, naming results Exec/<module>.<export>.
func BenchmarkExec(b *testing.B) {
	benchmarkExec(b, wago.NewRuntimeConfig())
}

// benchmarkExecCalls batches fast calls so every timed outer operation carries
// at least a millisecond of Wasm work. The reported ns/op is normalized back to
// one invocation, preserving the chart's per-call latency while avoiding timer
// noise dominating very small exports.
func benchmarkExecCalls(b *testing.B, invoke func() error) {
	const calibrationTarget = 2 * time.Millisecond
	batch := 1
	for {
		started := time.Now()
		for i := 0; i < batch; i++ {
			if err := invoke(); err != nil {
				b.Fatalf("calibration invoke: %v", err)
			}
		}
		elapsed := time.Since(started)
		if elapsed >= calibrationTarget {
			break
		}
		if elapsed <= 0 {
			batch *= 10
			continue
		}
		scaled := int(float64(batch) * float64(calibrationTarget) / float64(elapsed))
		if scaled <= batch {
			scaled = batch + 1
		}
		batch = scaled
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < batch; j++ {
			if err := invoke(); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*batch), "ns/op")
	b.ReportMetric(float64(batch), "calls/batch")
}

func benchmarkExec(b *testing.B, cfg *wago.RuntimeConfig) {
	for _, m := range loadCorpus(b) {
		if (len(m.Exec) == 0 && len(m.SemanticExec) == 0) || !m.supports("Exec") {
			continue
		}
		c, err := cfg.Compile(m.bytes)
		if err != nil {
			b.Fatalf("%s compile: %v", m.name(), err)
		}
		in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: hostStubs(c)})
		if err != nil {
			b.Fatalf("%s instantiate: %v", m.name(), err)
		}
		// wago has no start section, so AssemblyScript modules expose their
		// init (global setup) as an export the host calls once before exec.
		if m.Init != "" {
			if _, err := in.Invoke(m.Init); err != nil {
				b.Fatalf("%s init %s: %v", m.name(), m.Init, err)
			}
		}
		for _, e := range m.Exec {
			if e.Want == nil {
				b.Fatalf("%s.%s has no exact result oracle", m.ID, e.Export)
			}
			args := make([]uint64, len(e.Args))
			for i, a := range e.Args {
				args[i] = wago.I32(a)
			}
			fn, err := in.PrepareFunction(e.Export)
			if err != nil {
				b.Fatalf("%s prepare %s: %v", m.name(), e.Export, err)
			}
			if got, err := fn.Invoke(args...); err != nil {
				b.Fatalf("%s oracle invoke %s: %v", m.name(), e.Export, err)
			} else if !slices.Equal(got, e.Want) {
				b.Fatalf("%s.%s results = %v, want %v", m.name(), e.Export, got, e.Want)
			}
			b.Run(m.name()+"."+e.Export, func(b *testing.B) {
				benchmarkExecCalls(b, func() error {
					_, err := fn.Invoke(args...)
					return err
				})
			})
		}
		for _, semantic := range semanticExecCases(b, m) {
			if err := runSemanticOracle(semantic); err != nil {
				b.Fatalf("%s oracle: %v", semantic.ID, err)
			}
			prepared, err := prepareWagoSemanticExec(in, semantic)
			if err != nil {
				b.Fatalf("%s prepare: %v", semantic.ID, err)
			}
			b.Run(m.name()+"."+semantic.Invoke.Export, func(b *testing.B) {
				benchmarkExecCalls(b, prepared.invoke)
			})
		}
		in.Close()
	}
}

// BenchmarkExecParallel compares the default instance-local execution lease
// with the conservative process-wide lease across the executable corpus. Each
// benchmark worker owns an instance because Instance reuses invocation buffers.
func BenchmarkExecParallel(b *testing.B) {
	modes := []struct {
		name        string
		independent bool
	}{
		{name: "independent", independent: true},
		{name: "process", independent: false},
	}
	for _, m := range loadCorpus(b) {
		if len(m.Exec) == 0 || !m.supports("Exec") {
			continue
		}
		for _, mode := range modes {
			cfg := wago.NewRuntimeConfig().WithIndependentInstanceExecution(mode.independent)
			c, err := cfg.Compile(m.bytes)
			if err != nil {
				b.Fatalf("%s compile: %v", m.name(), err)
			}
			workers := runtime.GOMAXPROCS(0)
			instances := make([]*wago.Instance, workers)
			for i := range instances {
				instances[i], err = wago.Instantiate(c, wago.InstantiateOptions{Imports: hostStubs(c)})
				if err != nil {
					b.Fatalf("%s instantiate: %v", m.name(), err)
				}
				if m.Init != "" {
					if _, err := instances[i].Invoke(m.Init); err != nil {
						b.Fatalf("%s init %s: %v", m.name(), m.Init, err)
					}
				}
			}
			for _, e := range m.Exec {
				if e.Want == nil {
					b.Fatalf("%s.%s has no exact result oracle", m.ID, e.Export)
				}
				args := make([]uint64, len(e.Args))
				for i, a := range e.Args {
					args[i] = wago.I32(a)
				}
				functions := make([]*wago.PreparedFunction, len(instances))
				for i := range functions {
					functions[i], err = instances[i].PrepareFunction(e.Export)
					if err != nil {
						b.Fatalf("%s prepare %s: %v", m.name(), e.Export, err)
					}
					if got, err := functions[i].Invoke(args...); err != nil {
						b.Fatalf("%s warmup %s: %v", m.name(), e.Export, err)
					} else if !slices.Equal(got, e.Want) {
						b.Fatalf("%s.%s results = %v, want %v", m.name(), e.Export, got, e.Want)
					}
				}
				b.Run(mode.name+"/"+m.name()+"."+e.Export, func(b *testing.B) {
					var next atomic.Uint32
					b.ReportAllocs()
					b.SetParallelism(1)
					b.RunParallel(func(pb *testing.PB) {
						index := int(next.Add(1) - 1)
						fn := functions[index]
						for pb.Next() {
							if _, err := fn.Invoke(args...); err != nil {
								b.Errorf("invoke: %v", err)
								return
							}
						}
					})
				})
			}
			for _, in := range instances {
				if err := in.Close(); err != nil {
					b.Fatalf("%s close: %v", m.name(), err)
				}
			}
		}
	}
}
