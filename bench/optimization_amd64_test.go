//go:build amd64

package wagobench

import (
	"testing"

	wago "github.com/wago-org/wago"
)

// BenchmarkExecCommuteSelfUpdate keeps the real corpus rows that exercise the
// AMD64 commutative self-update lowering in one explicit A/B watchpoint.
func BenchmarkCompileCommuteSelfUpdate(b *testing.B) {
	wanted := map[string]bool{
		"blake-as": true, "blake-as-simd": true,
		"sqlite3": true, "esbuild": true,
	}
	for _, m := range loadCorpus(b) {
		if !wanted[m.name()] || !m.supports("CompileFull") {
			continue
		}
		for _, enabled := range []bool{false, true} {
			mode := "off"
			if enabled {
				mode = "on"
			}
			cfg := wago.NewRuntimeConfig().WithOptimization("commute-self-update", enabled)
			b.Run(m.name()+"/"+mode, func(b *testing.B) {
				b.ReportAllocs()
				var compiled *wago.Compiled
				for i := 0; i < b.N; i++ {
					var err error
					compiled, err = cfg.Compile(m.bytes)
					if err != nil {
						b.Fatal(err)
					}
				}
				if compiled != nil {
					b.ReportMetric(float64(compiled.CodeSize()), "code-B")
					compiled.Close()
				}
			})
		}
	}
}

func BenchmarkExecCommuteSelfUpdate(b *testing.B) {
	wanted := map[string]map[string]bool{
		"spectralnorm":  {"run": true},
		"quicksort":     {"sortN": true},
		"json-as":       {"serializeN": true, "deserializeN": true},
		"blake-as":      {"hashN": true},
		"xjb-mulhi":     {"runN": true},
		"blake-as-simd": {"hashN": true},
	}
	for _, m := range loadCorpus(b) {
		exports := wanted[m.name()]
		if len(exports) == 0 || !m.supports("Exec") {
			continue
		}
		for _, enabled := range []bool{false, true} {
			mode := "off"
			if enabled {
				mode = "on"
			}
			cfg := wago.NewRuntimeConfig().WithOptimization("commute-self-update", enabled)
			compiled, err := cfg.Compile(m.bytes)
			if err != nil {
				b.Fatalf("%s/%s compile: %v", m.name(), mode, err)
			}
			instance, err := wago.Instantiate(compiled, wago.InstantiateOptions{Imports: hostStubs(compiled)})
			if err != nil {
				b.Fatalf("%s/%s instantiate: %v", m.name(), mode, err)
			}
			if m.Init != "" {
				if _, err := instance.Invoke(m.Init); err != nil {
					b.Fatalf("%s/%s init: %v", m.name(), mode, err)
				}
			}
			for _, entry := range m.Exec {
				if !exports[entry.Export] {
					continue
				}
				args := make([]uint64, len(entry.Args))
				for i, arg := range entry.Args {
					args[i] = wago.I32(arg)
				}
				fn, err := instance.PrepareFunction(entry.Export)
				if err != nil {
					b.Fatalf("%s/%s prepare %s: %v", m.name(), mode, entry.Export, err)
				}
				b.Run(m.name()+"."+entry.Export+"/"+mode, func(b *testing.B) {
					if _, err := fn.Invoke(args...); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if _, err := fn.Invoke(args...); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
			instance.Close()
			compiled.Close()
		}
	}
}
