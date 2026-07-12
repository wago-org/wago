//go:build linux && amd64

package amd64

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Compile-throughput corpus benchmarks over real-world modules. Run:
//
//	go test ./src/core/compiler/backend/railshot/amd64 -run x -bench 'CTFull' -benchmem
//	go test ./src/core/compiler/backend/railshot/amd64 -run x -bench 'CTFull/esbuild' -benchmem \
//	    -cpuprofile cpu.out -memprofile mem.out -benchtime 3x
//
// Stages are measured separately so pprof attributes cost to decode / validate /
// codegen. "Full" is the end-to-end pipeline a caller actually pays for.

func ctCorpusDir() string {
	// package dir is src/core/compiler/backend/railshot/amd64 → 6 up to repo root.
	return filepath.Join("..", "..", "..", "..", "..", "..", "bench", "corpus")
}

// ctModules lists representative corpus files spanning sizes/shapes.
var ctModules = []string{
	"ruby.wasm",       // 16MB, huge C module
	"esbuild.wasm",    // 11MB Go module
	"sqlite3.wasm",    // 900K C
	"regexmatch.wasm", // 1.1M
	"markdown.wasm",   // 320K
	"lua.wasm",        // 270K
	"wasm3.wasm",      // 184K
	"bignum.wasm",     // 110K
	"blake3sum.wasm",  // 57K SIMD
	"json-as-simd.wasm",
}

func ctRead(tb testing.TB, name string) []byte {
	tb.Helper()
	data, err := os.ReadFile(filepath.Join(ctCorpusDir(), name))
	if err != nil {
		tb.Skipf("corpus %s not present: %v", name, err)
	}
	return data
}

var ctSink any

func BenchmarkCTDecode(b *testing.B) {
	for _, name := range ctModules {
		data := ctRead(b, name)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m, err := wasm.DecodeModule(data)
				if err != nil {
					b.Fatal(err)
				}
				ctSink = m
			}
		})
	}
}

func BenchmarkCTValidate(b *testing.B) {
	for _, name := range ctModules {
		data := ctRead(b, name)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m, err := wasm.DecodeModule(data)
				if err != nil {
					b.Fatal(err)
				}
				if err := wasm.ValidateModule(m); err != nil {
					b.Fatal(err)
				}
				ctSink = m
			}
		})
	}
}

func BenchmarkCTCodegen(b *testing.B) {
	for _, name := range ctModules {
		data := ctRead(b, name)
		m, err := frontend.DecodeValidate(data)
		if err != nil {
			b.Skipf("%s: %v", name, err)
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModule(m)
				if err != nil {
					b.Fatal(err)
				}
				ctSink = cm
			}
		})
	}
}

func BenchmarkCTFull(b *testing.B) {
	for _, name := range ctModules {
		data := ctRead(b, name)
		if _, err := frontend.DecodeValidate(data); err != nil {
			b.Logf("skip %s: %v", name, err)
			continue
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m, err := frontend.DecodeValidate(data)
				if err != nil {
					b.Fatal(err)
				}
				cm, err := CompileModule(m)
				if err != nil {
					b.Fatal(err)
				}
				ctSink = cm
			}
		})
	}
}
