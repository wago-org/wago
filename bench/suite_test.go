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
	"github.com/wago-org/wago/src/core/compiler/backend/amd64"
	wasm "github.com/wago-org/wago/src/core/compiler/wasm3"
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
		raw, err := os.ReadFile(filepath.Join(corpusDir, "manifest.json"))
		if err != nil {
			tb.Fatalf("read manifest: %v", err)
		}
		var m manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			tb.Fatalf("parse manifest: %v", err)
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
		corpus = m.Modules
	})
	return corpus
}

func (m corpusModule) name() string { return m.File[:len(m.File)-len(".wasm")] }

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
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			in, err := wago.Instantiate(c, nil)
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
		in, err := wago.Instantiate(c, nil)
		if err != nil {
			b.Fatalf("%s instantiate: %v", m.name(), err)
		}
		for _, e := range m.Exec {
			args := make([]wago.Value, len(e.Args))
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
