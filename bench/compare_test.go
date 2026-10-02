package wagobench

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
)

// Cross-engine comparison benchmarks. wazero (a mature, spec-complete runtime)
// compiles and runs every module in the corpus, so the results sit alongside wago's own stage
// numbers (WARP is compared separately by benchpub, which shells out to its
// native harness). Results are named WazeroCompile/<module> and
// WazeroExec/<module>.<export> to match the Stage/<module> convention.

// BenchmarkWazeroCompile times wazero's CompileModule (decode+validate+compile)
// for every corpus module, the closest analogue to wago's CompileFull.
func BenchmarkWazeroCompile(b *testing.B) {
	ctx := context.Background()
	for _, m := range loadCorpus(b) {
		if !m.avail {
			continue
		}
		b.Run(m.name(), func(b *testing.B) {
			r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
			defer r.Close(ctx)
			// Probe compile-ability, then close the probe so it doesn't leak into
			// the timed section's allocation accounting.
			if cm, err := r.CompileModule(ctx, m.bytes); err != nil {
				b.Skipf("wazero cannot compile %s: %v", m.name(), err)
			} else {
				cm.Close(ctx)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cm, err := r.CompileModule(ctx, m.bytes)
				if err != nil {
					b.Fatal(err)
				}
				cm.Close(ctx)
			}
		})
	}
}

// BenchmarkWazeroExec times the host->wasm call through wazero for the same exec
// entries wago's BenchmarkExec uses, for a like-for-like execution comparison.
func BenchmarkWazeroExec(b *testing.B) {
	ctx := context.Background()
	for _, m := range loadCorpus(b) {
		if len(m.Exec) == 0 || !m.avail {
			continue
		}
		r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
		// Provide the no-op env.abort AssemblyScript modules import (never fires
		// on valid input), mirroring wago's hostStubs so AS modules run on both
		// engines for a like-for-like comparison. Harmless for import-free modules.
		if _, err := r.NewHostModuleBuilder("env").
			NewFunctionBuilder().WithFunc(func(uint32, uint32, uint32, uint32) {}).Export("abort").
			Instantiate(ctx); err != nil {
			r.Close(ctx)
			b.Fatalf("wazero env host module: %v", err)
		}
		mod, err := r.Instantiate(ctx, m.bytes)
		if err != nil {
			r.Close(ctx)
			b.Logf("wazero cannot instantiate %s: %v", m.name(), err)
			continue
		}
		// wago has no start section; AS init runs via an export. Mirror wago's
		// Init call so wazero's instance is set up identically before exec.
		if m.Init != "" {
			if init := mod.ExportedFunction(m.Init); init != nil {
				if _, err := init.Call(ctx); err != nil {
					r.Close(ctx)
					b.Logf("wazero init %s %s: %v", m.name(), m.Init, err)
					continue
				}
			}
		}
		for _, e := range m.Exec {
			fn := mod.ExportedFunction(e.Export)
			if fn == nil {
				continue
			}
			args := make([]uint64, len(e.Args))
			for i, a := range e.Args {
				args[i] = uint64(uint32(a))
			}
			b.Run(m.name()+"."+e.Export, func(b *testing.B) {
				b.ReportAllocs()
				// Warm up + reset, mirroring wago's BenchmarkExec, so any
				// first-call setup cost isn't charged to the timed loop.
				if _, err := fn.Call(ctx, args...); err != nil {
					b.Fatalf("warmup call: %v", err)
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := fn.Call(ctx, args...); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
		r.Close(ctx)
	}
}
