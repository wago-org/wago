//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Both memories are shared. The selected memory has the requested address
// width; the other memory has the opposite width and a different initial size.
func sharedGrowZeroModule(address64 bool, index byte, maximum byte) []byte {
	typ, limits := wasm.I32, byte(3)
	if address64 {
		typ, limits = wasm.I64, 7
	}
	memories := [][]byte{{limits, 1, maximum}, {limits ^ 4, 0, maximum}}
	if index != 0 {
		memories[0], memories[1] = memories[1], memories[0]
	}
	// Preserve a live operand and a dirty local across each selected path.
	run := []byte{0x01, 0x01, 0x7e,
		0x20, 1, 0x42, 1, 0x7c, 0x21, 2,
		0x20, 1, 0x42, 2, 0x7c,
		0x20, 0, 0x40, index, 0x20, 2, 0x0b}
	run = append(wasmtest.ULEB(uint32(len(run))), run...)
	// base=0/mask=0: zero; base=1/mask=0: failure at a fixed maximum;
	// base=0/mask=1: alternating zero and failed nonzero calls.
	loop := []byte{0x01, 0x01, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 3, 0x20, 0, 0x4f, 0x0d, 1,
		0x20, 3, 0x20, 2, 0x71, 0x20, 1, 0x72}
	if address64 {
		loop = append(loop, 0xad)
	}
	loop = append(loop, 0x40, index, 0x1a,
		0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x0c, 0,
		0x0b, 0x0b, 0x3f, index, 0x0b)
	loop = append(wasmtest.ULEB(uint32(len(loop))), loop...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{typ, wasm.I64}, []wasm.ValType{wasm.I64, typ, wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{typ}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(5, wasmtest.Vec(memories...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("loop", 0, 1),
			wasmtest.ExportEntry("memory", 2, uint32(index)))),
		wasmtest.Section(10, wasmtest.Vec(run, loop)),
	)
}

func sharedGrowZeroConfig() *RuntimeConfig {
	return NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
}

func TestSharedGrowDynamicZeroWidthsAndLiveValues(t *testing.T) {
	for _, address64 := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, cache := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					t.Run(fmt.Sprintf("i64=%t/index=%d/cache=%t/regabi=%t", address64, index, cache, regABI), func(t *testing.T) {
						cfg := sharedGrowZeroConfig().WithOptimization("shared-memory-base", cache).WithOptimization("reg-abi", regABI)
						c, err := cfg.Compile(sharedGrowZeroModule(address64, index, 3))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
							in, err := Instantiate(compiled, InstantiateOptions{memoryLimitPages: 2})
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							pages := uint64(1)
							for n, delta := range []uint64{0, 1, 0, 1, 1 << 32, ^uint64(0), 0} {
								rawDelta := delta
								want := uint64(^uint32(0))
								if address64 {
									want = ^uint64(0)
								} else {
									delta = uint64(uint32(delta))
								}
								if delta <= 2-pages {
									want = pages
									pages += delta
								}
								token := uint64(n) + 1<<40
								got, err := in.Invoke("run", rawDelta, token)
								if err != nil || len(got) != 3 || got[0] != token+2 || got[1] != want || got[2] != token+1 {
									t.Fatalf("grow(%x): %v, %v; want [%d %d %d]", delta, got, err, token+2, want, token+1)
								}
							}
						}
					})
				}
			}
		}
	}
}

func BenchmarkSharedGrowDynamicZeroLoop(b *testing.B) {
	const iterations = 1024
	for _, address64 := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, path := range []struct {
				name       string
				base, mask uint64
			}{{"zero", 0, 0}, {"fallback", 1, 0}, {"mixed", 0, 1}} {
				b.Run(fmt.Sprintf("i64=%t/index=%d/%s", address64, index, path.name), func(b *testing.B) {
					c, err := sharedGrowZeroConfig().Compile(sharedGrowZeroModule(address64, index, 1))
					if err != nil {
						b.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c)
					if err != nil {
						b.Fatal(err)
					}
					defer in.Close()
					b.ReportAllocs()
					b.ResetTimer()
					for n := 0; n < b.N; n++ {
						got, err := in.Invoke("loop", iterations, path.base, path.mask)
						if err != nil || len(got) != 1 || got[0] != 1 {
							b.Fatalf("loop: %v, %v", got, err)
						}
					}
					b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/iterations, "ns/grow")
					b.ReportMetric(float64(len(c.code)), "code-B")
				})
			}
		}
	}
}

func BenchmarkSharedGrowDynamicZeroCompile(b *testing.B) {
	for _, address64 := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			b.Run(fmt.Sprintf("i64=%t/index=%d", address64, index), func(b *testing.B) {
				cfg, module := sharedGrowZeroConfig(), sharedGrowZeroModule(address64, index, 1)
				b.ReportAllocs()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					c, err := cfg.Compile(module)
					if err != nil {
						b.Fatal(err)
					}
					if err := c.Close(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
