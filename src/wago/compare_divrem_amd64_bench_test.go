//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func BenchmarkCompileCompareDivRemAMD64(b *testing.B) {
	for _, branch := range []bool{false, true} {
		name := "value"
		if branch {
			name = "branch"
		}
		b.Run(name, func(b *testing.B) {
			module := compareDivRemModule(wasm.I64, 0x80, 0x80, 0x51, branch)
			cfg := NewRuntimeConfig()
			var size int
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				compiled, err := Compile(cfg, module)
				if err != nil {
					b.Fatal(err)
				}
				size = compiled.CodeSize()
				compiled.Close()
			}
			b.ReportMetric(float64(size), "code-B")
		})
	}
}

func BenchmarkInvokeCompareDivRemAMD64(b *testing.B) {
	for _, branch := range []bool{false, true} {
		name := "value"
		if branch {
			name = "branch"
		}
		b.Run(name, func(b *testing.B) {
			compiled, instance := compileCompareDivRem(b, compareDivRemModule(wasm.I64, 0x80, 0x80, 0x51, branch))
			// Equal quotients return the same answer before and after the spill fix.
			got, err := instance.Invoke("run", 6, 2, 9, 3)
			if err != nil || len(got) != 1 || got[0] != 1 {
				b.Fatalf("result = %v, %v; want 1", got, err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := instance.Invoke("run", 6, 2, 9, 3); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(compiled.CodeSize()), "code-B")
		})
	}
}
