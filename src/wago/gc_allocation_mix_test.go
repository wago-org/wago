//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"os"
	"testing"
)

func allocationMixModule(t testing.TB) *Compiled {
	t.Helper()
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-allocation-mix.wasm")
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { compiled.Close() })
	return compiled
}

func allocationMixWant(name string, n uint64) uint64 {
	switch name {
	case "alternating":
		return uint64(uint32(n * n))
	case "alternating-fill":
		return uint64(uint32(n * (n - 1)))
	case "scalar-structs", "structs", "alternating-default":
		return uint64(uint32(n * (n - 1) / 2))
	default:
		panic("unknown allocation-mix export: " + name)
	}
}

var allocationMixExports = []string{"scalar-structs", "structs", "alternating", "alternating-default", "alternating-fill"}

// Keep the nested struct live across each array helper and across collections.
func TestGCAllocationMix(t *testing.T) {
	compiled := allocationMixModule(t)
	for _, stress := range []bool{false, true} {
		cfg := GCConfig{NurseryBytes: 4096, VerifyAfterCollect: true}
		if stress {
			cfg.CollectEveryAlloc = true
			cfg.StressNurseryBytes = 128
		}
		for _, name := range allocationMixExports {
			t.Run(name+map[bool]string{false: "/native", true: "/collect-every-alloc"}[stress], func(t *testing.T) {
				instance, err := Instantiate(compiled, InstantiateOptions{GC: cfg})
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				sizes := []uint64{1, 257, 4096}
				if stress {
					sizes = []uint64{1, 65}
				}
				for _, n := range sizes {
					got, err := instance.Invoke(name, n)
					if err != nil || len(got) != 1 || got[0] != allocationMixWant(name, n) {
						t.Fatalf("%s(%d) = %v, %v; want [%d]", name, n, got, err, allocationMixWant(name, n))
					}
				}
			})
		}
	}
}

// Collection stays enabled: DisableCollection bypasses native reservations and
// would hide the batch cancellation cost that this benchmark must measure.
func BenchmarkGCAllocationMix(b *testing.B) {
	compiled := allocationMixModule(b)
	for _, name := range allocationMixExports {
		b.Run(name, func(b *testing.B) {
			instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{
				NurseryBytes: 1 << 20, ThroughputHeapBytes: 64 << 20,
			}})
			if err != nil {
				b.Fatal(err)
			}
			defer instance.Close()
			const iterations = 8192
			invoke := func() {
				got, err := instance.Invoke(name, iterations)
				if err != nil || len(got) != 1 || got[0] != allocationMixWant(name, iterations) {
					b.Fatalf("%s(%d) = %v, %v; want [%d]", name, iterations, got, err, allocationMixWant(name, iterations))
				}
			}
			invoke()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				invoke()
			}
		})
	}
}
