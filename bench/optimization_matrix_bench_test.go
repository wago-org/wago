package wagobench

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	wago "github.com/wago-org/wago"
)

const (
	matrixOptEnv       = "WAGO_MATRIX_OPT"
	matrixOnEnv        = "WAGO_MATRIX_ON"
	matrixOverridesEnv = "WAGO_MATRIX_OVERRIDES"
)

func matrixConfig(tb testing.TB) *wago.RuntimeConfig {
	tb.Helper()
	cfg := wago.NewRuntimeConfig().WithFunctionWorkers(1)
	if overrides := os.Getenv(matrixOverridesEnv); overrides != "" {
		for _, override := range strings.Split(overrides, ",") {
			name, value, ok := strings.Cut(override, "=")
			if !ok {
				tb.Fatalf("%s entry %q has no =", matrixOverridesEnv, override)
			}
			on, err := strconv.ParseBool(value)
			if err != nil {
				tb.Fatalf("%s entry %q: %v", matrixOverridesEnv, override, err)
			}
			cfg = cfg.WithOptimization(name, on)
		}
		if err := cfg.Validate(); err != nil {
			tb.Fatalf("%s: %v", matrixOverridesEnv, err)
		}
	}
	name := os.Getenv(matrixOptEnv)
	if name == "" {
		return cfg
	}
	on, err := strconv.ParseBool(os.Getenv(matrixOnEnv))
	if err != nil {
		tb.Fatalf("%s: %v", matrixOnEnv, err)
	}
	cfg = cfg.WithOptimization(name, on)
	if err := cfg.Validate(); err != nil {
		tb.Fatalf("optimization %s=%t: %v", name, on, err)
	}
	return cfg
}

func TestOptimizationMatrixInventory(t *testing.T) {
	for _, info := range wago.NewRuntimeConfig().OptimizationInfos() {
		fmt.Printf("MATRIX_OPT\t%s\t%t\t%t\t%s\t%s\n",
			info.Name, info.On, info.Experimental, info.Label, info.Desc)
	}
}

// BenchmarkMatrixCompileFull measures the public decode+validate+compile path
// under one immutable optimization selection. Function workers are fixed at one
// so this measures compiler work rather than scheduler policy.
func BenchmarkMatrixCompileFull(b *testing.B) {
	cfg := matrixConfig(b)
	eachModule(b, "CompileFull", func(b *testing.B, m corpusModule) {
		for i := 0; i < b.N; i++ {
			if _, err := cfg.Compile(m.bytes); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkMatrixExec measures prepared host-to-Wasm calls from code compiled
// under the same per-process optimization selection as the compile benchmark.
func BenchmarkMatrixExec(b *testing.B) {
	cfg := matrixConfig(b)
	for _, m := range loadCorpus(b) {
		if len(m.Exec) == 0 || !m.supports("Exec") {
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
			fn, err := in.PrepareFunction(e.Export)
			if err != nil {
				b.Fatalf("%s prepare %s: %v", m.name(), e.Export, err)
			}
			b.Run(m.name()+"."+e.Export, func(b *testing.B) {
				b.ReportAllocs()
				if _, err := fn.Invoke(args...); err != nil {
					b.Fatalf("warmup invoke: %v", err)
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := fn.Invoke(args...); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
		in.Close()
	}
}
