//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func sharedGrowFailureLiteral(wide bool, value uint64) []byte {
	if wide {
		return append([]byte{0x42}, wasmtest.SLEB64(int64(value))...)
	}
	return append([]byte{0x41}, wasmtest.SLEB32(int32(value))...)
}
func sharedGrowFailureModule(wide bool, index, min, max byte, delta uint64) []byte {
	return sharedGrowFailureModuleWithImports(wide, index, min, max, delta, false)
}
func sharedGrowFailureModuleWithImports(wide bool, index, min, max byte, delta uint64, imported bool) []byte {
	typ, flags, wireType := wasm.I32, byte(3), byte(0x7f)
	if wide {
		typ, flags, wireType = wasm.I64, 7, 0x7e
	}
	memories := [][]byte{{flags, min, max}, {1, 0, 1}}
	if index != 0 {
		memories[0], memories[1] = memories[1], memories[0]
	}
	prefix := []byte{0x20, 1, 0x42, 1, 0x7c, 0x21, 2, 0x20, 1, 0x42, 2, 0x7c}
	body := func(dynamic, trap bool) []byte {
		code := []byte{1, 1, 0x7e}
		if trap {
			code = append(code, sharedGrowFailureLiteral(wide, ^uint64(0))...)
			code = append(code, 0x28, 0x42, index, 0, 0x1a)
		}
		code = append(code, prefix...)
		if dynamic {
			code = append(code, 0x20, 0)
		} else {
			code = append(code, sharedGrowFailureLiteral(wide, delta)...)
		}
		code = append(code, 0x40, index, 0x20, 2, 0x0b)
		return append(wasmtest.ULEB(uint32(len(code))), code...)
	}
	// Runtime mode chooses constant failure, dynamic fallback or an even split.
	loop := []byte{2, 1, 0x7f, 1, wireType, 0x02, 0x40, 0x03, 0x40, 0x20, 3, 0x20, 0, 0x4f, 0x0d, 1, 0x20, 4,
		0x20, 2, 0x45, 0x20, 2, 0x41, 2, 0x46, 0x20, 3, 0x41, 1, 0x71, 0x45, 0x71, 0x72, 0x04, wireType}
	loop = append(loop, sharedGrowFailureLiteral(wide, delta)...)
	loop = append(loop, 0x40, index, 0x05, 0x20, 1, 0x40, index, 0x0b)
	add := byte(0x6a)
	if wide {
		add = 0x7c
	}
	loop = append(loop, add, 0x21, 4, 0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x0c, 0, 0x0b, 0x0b, 0x20, 4, 0x0b)
	loop = append(wasmtest.ULEB(uint32(len(loop))), loop...)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{typ, wasm.I64}, []wasm.ValType{wasm.I64, typ, wasm.I64}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, typ, wasm.I32}, []wasm.ValType{typ})))}
	if imported {
		var entries [][]byte
		for i, memory := range memories {
			entry := append(wasmtest.Name("env"), wasmtest.Name(fmt.Sprintf("memory%d", i))...)
			entry = append(entry, 2)
			entries = append(entries, append(entry, memory...))
		}
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entries...)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0}, []byte{0}, []byte{1})))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(memories...)))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("constant", 0, 0), wasmtest.ExportEntry("dynamic", 0, 1), wasmtest.ExportEntry("trap-before", 0, 2), wasmtest.ExportEntry("loop", 0, 3), wasmtest.ExportEntry("memory0", 2, 0), wasmtest.ExportEntry("memory1", 2, 1))),
		wasmtest.Section(10, wasmtest.Vec(body(false, false), body(true, false), body(false, true), loop)))
	return wasmtest.Module(sections...)

}
func sharedGrowFailureConfig(t testing.TB, wide bool) *RuntimeConfig {
	t.Helper()
	// These fixtures declare two memories even when the selected index is zero.
	features := CoreFeaturesV2 | CoreFeatureThreads | CoreFeatureMultiMemory
	if wide {
		features |= CoreFeatureMemory64
	}
	if !platformCoreFeatures().IsEnabled(features) {
		t.Skipf("module features %v are unavailable on this backend", features)
	}
	return NewRuntimeConfig().WithCoreFeatures(features).WithBoundsChecks(BoundsChecksExplicit)
}
func TestSharedGrowImpossibleUnsignedAndLiveValues(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, limits := range [][2]byte{{0, 0}, {0, 3}, {1, 3}, {3, 3}} {
				min, max := limits[0], limits[1]
				room := uint64(max - min)
				for _, delta := range []uint64{0, 1, room, room + 1, uint64(^uint32(0)), 1 << 32, ^uint64(0)} {
					t.Run(fmt.Sprintf("wide=%t/index=%d/min=%d/max=%d/delta=%x", wide, index, min, max, delta), func(t *testing.T) {
						c, err := sharedGrowFailureConfig(t, wide).Compile(sharedGrowFailureModule(wide, index, min, max, delta))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()

						for _, compiled := range []*Compiled{c} {
							for _, name := range []string{"constant", "dynamic"} {
								in, err := Instantiate(compiled)
								if err != nil {
									t.Fatal(err)
								}
								defer in.Close()
								effective := delta
								failure := ^uint64(0)
								if !wide {
									effective = uint64(uint32(delta))
									failure = uint64(^uint32(0))
								}
								want := failure
								if effective <= room {
									want = uint64(min)
								}
								got, err := in.Invoke(name, delta, 1<<40)
								if err != nil || len(got) != 3 || got[0] != 1<<40|2 || got[1] != want || got[2] != 1<<40|1 {
									t.Fatalf("%s: %v,%v; want %d", name, got, err, want)
								}
							}
							in, err := Instantiate(compiled)
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							_, err = in.Invoke("trap-before", delta, 0)
							var trap *TrapError
							if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
								t.Fatalf("prior memory trap: %v", err)
							}
						}
					})
				}
			}
		}
	}
}
func TestSharedGrowImpossibleImportedCurrentSizeAndQuota(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			t.Run(fmt.Sprintf("wide=%t/index=%d", wide, index), func(t *testing.T) {
				cfg := sharedGrowFailureConfig(t, wide)
				code, err := cfg.Compile(sharedGrowFailureModule(wide, index, 1, 3, 3))
				if err != nil {
					t.Fatal(err)
				}
				defer code.Close()
				provider, err := Instantiate(code)
				if err != nil {
					t.Fatal(err)
				}
				defer provider.Close()
				if got, err := provider.Invoke("dynamic", 1, 9); err != nil || len(got) != 3 || got[1] != 1 {
					t.Fatalf("producer grow: %v,%v", got, err)
				}
				imports := NewImports()
				for n := 0; n < 2; n++ {
					name := fmt.Sprintf("memory%d", n)
					m, err := provider.ExportedMemory(name)
					if err != nil {
						t.Fatal(err)
					}
					imports.Memory("env", name, m)
				}
				importedCode, err := cfg.Compile(sharedGrowFailureModuleWithImports(wide, index, 1, 3, 3, true))
				if err != nil {
					t.Fatal(err)
				}
				defer importedCode.Close()
				consumer, err := Instantiate(importedCode, InstantiateOptions{Imports: imports, memoryLimitPages: 2})
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				if err := provider.Close(); err != nil {
					t.Fatal(err)
				}
				failure := ^uint64(0)
				if !wide {
					failure = uint64(^uint32(0))
				}
				for _, test := range []struct {
					name        string
					delta, want uint64
				}{{"constant", 0, failure}, {"dynamic", 2, failure}, {"dynamic", 0, 2}} {
					got, err := consumer.Invoke(test.name, test.delta, 9)
					if err != nil || len(got) != 3 || got[0] != 11 || got[1] != test.want || got[2] != 10 {
						t.Fatalf("%s(%d):%v,%v;want%x", test.name, test.delta, got, err, test.want)
					}
				}
			})
		}
	}
}

func TestSharedGrowImpossibleCacheAndRegisterABI(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, cache := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					t.Run(fmt.Sprintf("wide=%t/index=%d/cache=%t/regABI=%t", wide, index, cache, regABI), func(t *testing.T) {
						cfg := sharedGrowFailureConfig(t, wide).WithOptimization("shared-memory-base", cache).WithOptimization("reg-abi", regABI)
						c, err := cfg.Compile(sharedGrowFailureModule(wide, index, 1, 1, 1))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()

						failure := ^uint64(0)
						if !wide {
							failure = uint64(^uint32(0))
						}
						for _, compiled := range []*Compiled{c} {
							in, err := Instantiate(compiled)
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							for _, name := range []string{"constant", "dynamic"} {
								got, err := in.Invoke(name, 1, 9)
								if err != nil || len(got) != 3 || got[0] != 11 || got[1] != failure || got[2] != 10 {
									t.Fatalf("%s: %v,%v", name, got, err)
								}
							}
						}
					})
				}
			}
		}
	}
}

func BenchmarkSharedGrowImpossibleLoop(b *testing.B) {
	const iterations = 1024
	for _, wide := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for mode, name := range []string{"constant-fail", "fallback", "mixed"} {
				b.Run(fmt.Sprintf("wide=%t/index=%d/%s", wide, index, name), func(b *testing.B) {
					c, err := sharedGrowFailureConfig(b, wide).Compile(sharedGrowFailureModule(wide, index, 1, 1, 1))
					if err != nil {
						b.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c)
					if err != nil {
						b.Fatal(err)
					}
					defer in.Close()
					want := ^uint64(0) - iterations + 1
					if !wide {
						want = uint64(uint32(want))
					}
					b.ReportAllocs()
					b.ResetTimer()
					for n := 0; n < b.N; n++ {
						got, err := in.Invoke("loop", iterations, 1, uint64(mode))
						if err != nil || len(got) != 1 || got[0] != want {
							b.Fatalf("loop: %v,%v;want%x", got, err, want)
						}
					}
					b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/iterations, "ns/grow")
					b.ReportMetric(float64(len(c.code)), "code-B")
				})
			}
		}
	}
}
func BenchmarkSharedGrowImpossibleCompile(b *testing.B) {
	for _, wide := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			b.Run(fmt.Sprintf("wide=%t/index=%d", wide, index), func(b *testing.B) {
				cfg, module := sharedGrowFailureConfig(b, wide), sharedGrowFailureModule(wide, index, 1, 1, 1)
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
