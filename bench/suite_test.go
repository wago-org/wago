// Package wagobench's suite benchmarks the wago pipeline stage-by-stage across a
// curated corpus of wasm modules (see corpus/manifest.json). Each stage is a
// separate top-level Benchmark so it can be filtered (e.g. -bench Compile), and
// fans out over the corpus via b.Run so results read as Stage/<module>. This is
// wago-only (no wazero) so the numbers track wago's own performance over time.
package wagobench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot"
	wasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

const corpusDir = "corpus"

type execEntry struct {
	Export string  `json:"export"`
	Args   []int32 `json:"args"`
}

type corpusModule struct {
	File     string      `json:"file"`
	Path     string      `json:"path"`     // optional: reference a wasm in place (relative to bench/)
	Category string      `json:"category"` // micro/loop/.../real/real-large
	Desc     string      `json:"desc"`
	Stages   []string    `json:"stages"` // optional: stages this module supports (default: all)
	Init     string      `json:"init"`   // optional: export to call once after instantiate, before exec (e.g. AssemblyScript's _initialize; wago has no start section)
	Exec     []execEntry `json:"exec"`

	bytes []byte
	avail bool // false when an optional referenced path is missing
}

// supports reports whether the module should be benchmarked at the given stage.
// An empty Stages list means every stage.
func (m corpusModule) supports(stage string) bool {
	if !m.avail {
		return false
	}
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

type manifest struct {
	Modules []corpusModule `json:"modules"`
}

var (
	corpusOnce sync.Once
	corpus     []corpusModule
)

func loadCorpus(tb testing.TB) []corpusModule {
	corpusOnce.Do(func() {
		// The hand-maintained corpus plus the generated ISA micro-suite
		// (corpus/isa-manifest.json, one export per opcode). Both share the schema.
		corpus = append(readManifest(tb, "manifest.json"), readManifest(tb, "isa-manifest.json")...)
	})
	return corpus
}

// readManifest loads one manifest file and resolves each module's bytes.
func readManifest(tb testing.TB, file string) []corpusModule {
	raw, err := os.ReadFile(filepath.Join(corpusDir, file))
	if err != nil {
		tb.Fatalf("read %s: %v", file, err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		tb.Fatalf("parse %s: %v", file, err)
	}
	for i := range m.Modules {
		mod := &m.Modules[i]
		path := filepath.Join(corpusDir, mod.File)
		if mod.Path != "" {
			path = mod.Path
		}
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			mod.bytes = b
			mod.avail = true
		case mod.Path != "":
			// An optional in-place reference (e.g. a real-world binary that
			// lives elsewhere in the tree) — skip rather than fail.
			tb.Logf("corpus: %s not present (%s), skipping", mod.File, mod.Path)
		default:
			tb.Fatalf("read %s: %v", mod.File, err)
		}
	}
	return m.Modules
}

func (m corpusModule) name() string { return m.File[:len(m.File)-len(".wasm")] }

// hostStubs supplies a no-op void host function for every function import the
// module declares (e.g. AssemblyScript's env.abort). The log-and-replay host
// model ignores all but the first i32 arg and returns nothing, so a shared
// no-op satisfies any numeric host import — enough for the corpus, whose real
// modules only import env.abort (which never fires on valid input). Returns nil
// for import-free modules (the synthetic corpus).
func hostStubs(c *wago.Compiled) wago.Imports {
	if len(c.Imports) == 0 {
		return nil
	}
	im := make(wago.Imports, len(c.Imports))
	for _, name := range c.Imports {
		im[name] = wago.HostFunc(func(int32) {})
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

// BenchmarkCompile times native codegen for an already decoded+validated module.
func BenchmarkCompile(b *testing.B) {
	eachModule(b, "Compile", func(b *testing.B, m corpusModule) {
		mod := m.decoded(b)
		if err := wasm.ValidateModule(mod); err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := amd64.CompileModule(mod); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkCompileFull times the end-to-end decode+validate+compile entry point.
func BenchmarkCompileFull(b *testing.B) {
	eachModule(b, "CompileFull", func(b *testing.B, m corpusModule) {
		for i := 0; i < b.N; i++ {
			if _, err := wago.Compile(m.bytes); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkInstantiate times instance setup for an already-compiled module.
func BenchmarkInstantiate(b *testing.B) {
	eachModule(b, "Instantiate", func(b *testing.B, m corpusModule) {
		c, err := wago.Compile(m.bytes)
		if err != nil {
			b.Fatal(err)
		}
		imports := hostStubs(c)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			in, err := wago.Instantiate(c, imports)
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
	for _, m := range loadCorpus(b) {
		if len(m.Exec) == 0 || !m.supports("Exec") {
			continue
		}
		c, err := wago.Compile(m.bytes)
		if err != nil {
			b.Fatalf("%s compile: %v", m.name(), err)
		}
		in, err := wago.Instantiate(c, hostStubs(c))
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
			args := make([]uint64, len(e.Args))
			for i, a := range e.Args {
				args[i] = wago.I32(a)
			}
			b.Run(m.name()+"."+e.Export, func(b *testing.B) {
				b.ReportAllocs()
				if _, err := in.Invoke(e.Export, args...); err != nil {
					b.Fatalf("warmup invoke: %v", err)
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := in.Invoke(e.Export, args...); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
		in.Close()
	}
}
