package wagobench

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	wago "github.com/wago-org/wago"
	"github.com/wago-org/wasi"
)

// Warm instantiate of the real Rust/WASI modules: compile once, then time a fresh
// instance per iteration (no _start) — the realistic serving path (compile a
// module once, instantiate it per request). Companion to the Run/Compile rows;
// reuses wasiRunProgs from wasi_run_test.go.

func BenchmarkInstBigWago(b *testing.B) {
	for _, name := range wasiRunProgs {
		src, err := os.ReadFile("corpus/" + name + ".wasm")
		if err != nil {
			continue
		}
		c, err := wago.Compile(nil, src)
		if err != nil {
			b.Fatalf("%s compile: %v", name, err)
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: wasi.Imports(wasi.Config{Stdout: io.Discard, Args: []string{name}})})
				if err != nil {
					b.Fatalf("instantiate: %v", err)
				}
				in.Close()
			}
		})
	}
}

func BenchmarkInstBigWazero(b *testing.B) {
	ctx := context.Background()
	for _, name := range wasiRunProgs {
		src, err := os.ReadFile("corpus/" + name + ".wasm")
		if err != nil {
			continue
		}
		r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
		wasi_snapshot_preview1.MustInstantiate(ctx, r)
		cm, err := r.CompileModule(ctx, src)
		if err != nil {
			r.Close(ctx)
			b.Fatalf("%s compile: %v", name, err)
		}
		// WithStartFunctions() (empty) => instantiate without running _start.
		cfg := wazero.NewModuleConfig().WithStdout(io.Discard).WithArgs(name).WithName("").WithStartFunctions()
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				mod, err := r.InstantiateModule(ctx, cm, cfg)
				if err != nil {
					b.Fatalf("instantiate: %v", err)
				}
				mod.Close(ctx)
			}
		})
		r.Close(ctx)
	}
}
