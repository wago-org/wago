package wagobench

import (
	"os"
	"testing"

	"github.com/wago-org/wago/src/wago"
)

// perfKernel is one compute kernel invoked in the exec benchmark below.
type perfKernel struct {
	file, export string
	args         []uint64
}

var perfKernels = []perfKernel{
	{"nbody.wasm", "step", []uint64{2000}},
	{"spectralnorm.wasm", "run", []uint64{128}},
	{"fannkuch.wasm", "run", []uint64{9}},
	{"matmul.wasm", "run", []uint64{64}},
	{"quicksort.wasm", "sortN", []uint64{4096}},
	{"sha256.wasm", "hashN", []uint64{64}},
	{"raytrace.wasm", "render", []uint64{48}},
}

// BenchmarkKernels times each compute kernel's exec (compile+instantiate once,
// then Invoke b.N times). Run with WAGO_INLINE / WAGO_LOOP_PRECHECK on/off to
// measure the exec win of the gated optimizations against their code-size cost.
func BenchmarkKernels(b *testing.B) {
	for _, k := range perfKernels {
		k := k
		b.Run(k.export, func(b *testing.B) {
			src, err := os.ReadFile("corpus/" + k.file)
			if err != nil {
				b.Skipf("no %s", k.file)
			}
			cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)
			comp, err := wago.Compile(cfg, src)
			if err != nil {
				b.Fatalf("compile: %v", err)
			}
			in, err := wago.Instantiate(comp, wago.InstantiateOptions{Imports: wago.Imports{"env.abort": wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {})}})
			if err != nil {
				b.Fatalf("instantiate: %v", err)
			}
			defer in.Close()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := in.Invoke(k.export, k.args...); err != nil {
					b.Fatalf("invoke: %v", err)
				}
			}
		})
	}
}
