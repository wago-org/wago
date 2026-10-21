//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Both memories are shared. The selected memory has the requested address
// width; the other memory has the opposite width and a different initial size.
func sharedGrowZeroModule(address64 bool, index byte, maximum byte) []byte {
	return sharedGrowZeroModuleWithImports(address64, index, maximum, false)
}

func sharedGrowZeroModuleWithImports(address64 bool, index byte, maximum byte, imported bool) []byte {
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
	constant, less := byte(0x41), byte(0x49)
	if address64 {
		constant, less = 0x42, 0x54
	}
	// Signal native entry, poll grow(dynamic zero), then read the new page.
	observe := []byte{constant, 4, 0x41, 1, 0xfe, 0x17, 0x42, index, 0,
		0x03, 0x40, 0x20, 0, 0x40, index, constant, 2, less, 0x0d, 0, 0x0b,
		constant, 0x80, 0x80, 4, 0x28, 0x42, index, 0, 0x0b}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{typ, wasm.I64}, []wasm.ValType{wasm.I64, typ, wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{typ}),
			wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{wasm.I32}),
		)),
	}
	if imported {
		var entries [][]byte
		for i, memory := range memories {
			entry := append(wasmtest.Name("env"), wasmtest.Name(fmt.Sprintf("memory%d", i))...)
			entry = append(entry, 2)
			entries = append(entries, append(entry, memory...))
		}
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entries...)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{2})))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(memories...)))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("loop", 0, 1),
			wasmtest.ExportEntry("memory", 2, uint32(index)), wasmtest.ExportEntry("memory0", 2, 0),
			wasmtest.ExportEntry("memory1", 2, 1), wasmtest.ExportEntry("observe", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(run, loop, wasmtest.Code(observe))),
	)
	return wasmtest.Module(sections...)
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
						loaded := publicArtifactRoundTrip(t, c)
						defer loaded.Close()
						for _, compiled := range []*Compiled{c, loaded} {
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

func TestSharedGrowDynamicZeroObservesConcurrentGrowth(t *testing.T) {
	for _, address64 := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			t.Run(fmt.Sprintf("i64=%t/index=%d", address64, index), func(t *testing.T) {
				c, err := sharedGrowZeroConfig().Compile(sharedGrowZeroModule(address64, index, 3))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				memory, err := in.ExportedMemory("memory")
				if err != nil {
					t.Fatal(err)
				}
				flag := (*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[4]))
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				timer := time.AfterFunc(5*time.Second, cancel)
				defer timer.Stop()
				done := make(chan error, 1)
				go func() {
					got, err := in.InvokeContext(ctx, "observe", 0)
					if err == nil && (len(got) != 1 || got[0] != 0) {
						err = fmt.Errorf("new page = %v", got)
					}
					done <- err
				}()
				for atomic.LoadUint32(flag) == 0 {
					select {
					case err := <-done:
						t.Fatalf("native observer returned before growth: %v", err)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					default:
						runtime.Gosched()
					}
				}
				// The directory was published at one page when this activation began.
				if got := memory.growShared(1, 0); got != 1 {
					t.Errorf("growShared = %d, want 1", got)
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestSharedGrowDynamicZeroPreservesVectorPrefix(t *testing.T) {
	payload := []byte{1, 0x82, 3, 0xf4, 5, 0xa6, 7, 0xe8, 9, 0xba, 11, 0xdc, 13, 0xfe, 15, 0x90}
	for _, address64 := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, regABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("i64=%t/index=%d/regabi=%t", address64, index, regABI), func(t *testing.T) {
					typ, limits, wireType := wasm.I32, byte(3), byte(0x7f)
					if address64 {
						typ, limits, wireType = wasm.I64, 7, 0x7e
					}
					memories := [][]byte{{limits, 1, 2}, {1, 0, 1}}
					if index != 0 {
						memories[0], memories[1] = memories[1], memories[0]
					}
					// v128 is a two-slot live prefix below the grow operand. Retain
					// both halves after the common zero/nonzero result join.
					body := append([]byte{2, 1, 0x7b, 1, wireType, 0xfd, 0x0c}, payload...)
					body = append(body, 0x20, 0, 0x40, index, 0x21, 2, 0x22, 1,
						0xfd, 0x1d, 0, 0x20, 1, 0xfd, 0x1d, 1, 0x20, 2, 0x0b)
					body = append(wasmtest.ULEB(uint32(len(body))), body...)
					module := wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{wasm.I64, wasm.I64, typ}))),
						wasmtest.Section(3, wasmtest.Vec([]byte{0})),
						wasmtest.Section(5, wasmtest.Vec(memories...)),
						wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
						wasmtest.Section(10, wasmtest.Vec(body)),
					)
					c, err := sharedGrowZeroConfig().WithOptimization("reg-abi", regABI).Compile(module)
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					loaded := publicArtifactRoundTrip(t, c)
					defer loaded.Close()
					for _, compiled := range []*Compiled{c, loaded} {
						in, err := Instantiate(compiled)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						for n, delta := range []uint64{0, 1, 0, 1 << 32, 0} {
							want := uint64(2)
							if n < 2 {
								want = 1
							} else if n == 3 && address64 {
								want = ^uint64(0)
							}
							got, err := in.Invoke("run", delta)
							if err != nil || len(got) != 3 || got[0] != binary.LittleEndian.Uint64(payload) || got[1] != binary.LittleEndian.Uint64(payload[8:]) || got[2] != want {
								t.Fatalf("grow(%x): vector/results %x, %v; want grow %x", delta, got, err, want)
							}
						}
					}
				})
			}
		}
	}
}

func TestSharedGrowDynamicZeroPreservesCollectorLocal(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureGC)
	for _, address64 := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			t.Run(fmt.Sprintf("i64=%t/index=%d", address64, index), func(t *testing.T) {
				typ, limits, wireType := wasm.I32, byte(3), byte(0x7f)
				if address64 {
					typ, limits, wireType = wasm.I64, 7, 0x7e
				}
				memories := [][]byte{{limits, 1, 2}, {1, 0, 1}}
				if index != 0 {
					memories[0], memories[1] = memories[1], memories[0]
				}
				body := []byte{2, 1, 0x63, 0, 1, wireType,
					0x41, 0xc9, 0, 0xfb, 0, 0, 0x21, 1,
					0x20, 0, 0x40, index, 0x21, 2,
					0xfb, 1, 0, 0x1a, 0xfb, 1, 0, 0x1a, 0xfb, 1, 0, 0x1a,
					0x20, 1, 0xfb, 2, 0, 0, 0x20, 2, 0x0b}
				body = append(wasmtest.ULEB(uint32(len(body))), body...)
				module := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1},
						wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{wasm.I32, typ}))),
					wasmtest.Section(3, wasmtest.Vec([]byte{1})),
					wasmtest.Section(5, wasmtest.Vec(memories...)),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
					wasmtest.Section(10, wasmtest.Vec(body)),
				)
				c, err := sharedGrowZeroConfig().Compile(module)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				admission := c.GCNativeRootAdmission()
				if !admission.Required || !admission.Exact {
					t.Fatalf("collector local was not admitted to exact roots: %+v", admission)
				}
				in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny,
					TinyHeapBytes: 64, TinyBlockBytes: 16, TinyCollectEveryAlloc: true,
					TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				for n, delta := range []uint64{0, 1, 0, 1 << 32, 0} {
					want := uint64(2)
					if n < 2 {
						want = 1
					} else if n == 3 && address64 {
						want = ^uint64(0)
					}
					got, err := in.Invoke("run", delta)
					if err != nil || len(got) != 2 || got[0] != 73 || got[1] != want {
						t.Fatalf("grow(%x) then collect: %v, %v; want [73 %x]", delta, got, err, want)
					}
				}
			})
		}
	}
}

func TestSharedGrowDynamicZeroRetainsClosedProducer(t *testing.T) {
	for _, address64 := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			t.Run(fmt.Sprintf("i64=%t/index=%d", address64, index), func(t *testing.T) {
				cfg := sharedGrowZeroConfig()
				c, err := cfg.Compile(sharedGrowZeroModule(address64, index, 3))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				provider, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer provider.Close()
				imports := NewImports()
				for n := 0; n < 2; n++ {
					name := fmt.Sprintf("memory%d", n)
					memory, err := provider.ExportedMemory(name)
					if err != nil {
						t.Fatal(err)
					}
					imports.Memory("env", name, memory)
				}
				consumerCode, err := cfg.Compile(sharedGrowZeroModuleWithImports(address64, index, 3, true))
				if err != nil {
					t.Fatal(err)
				}
				defer consumerCode.Close()
				consumer, err := Instantiate(consumerCode, InstantiateOptions{Imports: imports, memoryLimitPages: 1})
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				// A different importer can grow beyond this consumer's quota. Zero
				// must still report the authoritative size after the owner closes.
				if got, err := provider.Invoke("run", 1, 9); err != nil || len(got) != 3 || got[1] != 1 {
					t.Fatalf("producer grow = %v, %v", got, err)
				}
				if err := provider.Close(); err != nil {
					t.Fatal(err)
				}
				for _, delta := range []uint64{0, 1, 0} {
					want := uint64(2)
					if delta != 0 {
						want = uint64(^uint32(0))
						if address64 {
							want = ^uint64(0)
						}
					}
					got, err := consumer.Invoke("run", delta, 9)
					if err != nil || len(got) != 3 || got[0] != 11 || got[1] != want || got[2] != 10 {
						t.Fatalf("grow(%d) after close = %v, %v", delta, got, err)
					}
				}
			})
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
