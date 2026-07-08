//go:build wago_wasi

package wagobench

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	wsys "github.com/tetratelabs/wazero/sys"
	wago "github.com/wago-org/wago"
	"github.com/wago-org/wasi"
)

// The real Rust/WASI programs (bench/corpus/rust-wasi) do their whole workload in
// _start, so "running" one is compile + instantiate + execute — that whole
// end-to-end run is what these benchmark (both engines race the same steps per
// iteration). It's the run-side companion to the same programs' Compile-tab
// numbers; wago's fast compile + execution outweigh its (separately) heavier
// instantiate, so it wins here even though pure instantiate favours wazero.
var wasiRunProgs = []string{"markdown", "jsonproc", "blake3sum", "base64x", "crcsum", "script", "regexmatch"}

func BenchmarkRunWago(b *testing.B) {
	for _, name := range wasiRunProgs {
		src, err := os.ReadFile("corpus/" + name + ".wasm")
		if err != nil {
			continue
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				c, err := wago.Compile(nil, src)
				if err != nil {
					b.Fatalf("compile: %v", err)
				}
				in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: wasi.Imports(wasi.Config{Stdout: io.Discard, Args: []string{name}})})
				if err != nil {
					b.Fatalf("instantiate: %v", err)
				}
				if _, err := in.Invoke("_start"); err != nil {
					var ex *wago.ExitError
					if !errors.As(err, &ex) {
						in.Close()
						b.Fatalf("run: %v", err)
					}
				}
				in.Close()
			}
		})
	}
}

func BenchmarkRunWazero(b *testing.B) {
	ctx := context.Background()
	for _, name := range wasiRunProgs {
		src, err := os.ReadFile("corpus/" + name + ".wasm")
		if err != nil {
			continue
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
				wasi_snapshot_preview1.MustInstantiate(ctx, r)
				cm, err := r.CompileModule(ctx, src)
				if err != nil {
					b.Fatalf("compile: %v", err)
				}
				mod, err := r.InstantiateModule(ctx, cm, wazero.NewModuleConfig().WithStdout(io.Discard).WithArgs(name).WithName(""))
				if err != nil {
					var ex *wsys.ExitError
					if !errors.As(err, &ex) {
						b.Fatalf("run: %v", err)
					}
				}
				if mod != nil {
					mod.Close(ctx)
				}
				r.Close(ctx)
			}
		})
	}
}
