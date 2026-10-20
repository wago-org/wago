//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func sharedGrowDynamicFailureModule(wide bool, index byte, imported bool) []byte {
	return sharedGrowDynamicFailureModuleWithMaximum(wide, index, imported, 3)
}
func sharedGrowDynamicFailureModuleWithMaximum(wide bool, index byte, imported bool, maximum uint64) []byte {
	typ, limits, wire, constant := wasm.I32, byte(3), byte(0x7f), byte(0x41)
	if wide {
		typ, limits, wire, constant = wasm.I64, 7, 0x7e, 0x42
	}
	selected := []byte{limits, 1}
	for remaining := maximum; ; {
		b := byte(remaining & 0x7f)
		remaining >>= 7
		if remaining != 0 {
			b |= 0x80
		}
		selected = append(selected, b)
		if remaining == 0 {
			break
		}
	}
	memories := [][]byte{selected, {limits ^ 6, 0, 1}}
	if index != 0 {
		memories[0], memories[1] = memories[1], memories[0]
	}
	body := func(raw []byte) []byte { return append(wasmtest.ULEB(uint32(len(raw))), raw...) }
	run := []byte{1, 1, 0x7e, 0x20, 1, 0x42, 1, 0x7c, 0x21, 2, 0x20, 1, 0x42, 2, 0x7c, 0x20, 0, 0x40, index, 0x20, 2, 0x0b}
	loop := []byte{2, 1, 0x7f, 1, wire, 0x02, 0x40, 0x03, 0x40, 0x20, 3, 0x20, 0, 0x4f, 0x0d, 1, 0x20, 4, 0x20, 3}
	and, or, add := byte(0x71), byte(0x72), byte(0x6a)
	if wide {
		loop = append(loop, 0xad)
		and, or, add = 0x83, 0x84, 0x7c
	}
	loop = append(loop, 0x20, 2, and, 0x20, 1, or, 0x40, index, add, 0x21, 4, 0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x0c, 0, 0x0b, 0x0b, 0x20, 4, 0x0b)
	spin := []byte{constant, 4, 0x41, 1, 0xfe, 0x17, 0x42, index, 0, 0x03, 0x40, 0x20, 0, 0x40, index, 0x1a, 0x0c, 0, 0x0b, 0x0b}
	trap := []byte{1, 1, 0x7e, constant, 0x7f, 0x28, 0x42, index, 0, 0x1a}
	trap = append(trap, run[3:]...)
	vector := []byte{0xfd, 12, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 0x20, 0, 0x40, index, 0x0b}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{typ, wasm.I64}, []wasm.ValType{wasm.I64, typ, wasm.I64}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, typ, typ}, []wasm.ValType{typ}),
		wasmtest.FuncType([]wasm.ValType{typ}, nil),
		wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{wasm.V128, typ})))}
	if imported {
		var entries [][]byte
		for n, m := range memories {
			entry := append(wasmtest.Name("env"), wasmtest.Name(fmt.Sprintf("memory%d", n))...)
			entries = append(entries, append(append(entry, 2), m...))
		}
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entries...)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{2}, []byte{0}, []byte{3})))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(memories...)))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(
		wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("loop", 0, 1), wasmtest.ExportEntry("spin", 0, 2), wasmtest.ExportEntry("trap-before", 0, 3), wasmtest.ExportEntry("vector", 0, 4),
		wasmtest.ExportEntry("memory", 2, uint32(index)), wasmtest.ExportEntry("memory0", 2, 0), wasmtest.ExportEntry("memory1", 2, 1))),
		wasmtest.Section(10, wasmtest.Vec(body(run), body(loop), wasmtest.Code(spin), body(trap), wasmtest.Code(vector))))
	return wasmtest.Module(sections...)
}

func sharedGrowDynamicFailureConfig() *RuntimeConfig {
	return NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
}
func sharedGrowDynamicFailureResult(wide bool) uint64 {
	if wide {
		return ^uint64(0)
	}
	return uint64(^uint32(0))
}

func TestSharedGrowDynamicFailureBoundariesAndJoins(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, cache := range []bool{false, true} {
				for _, regABI := range []bool{false, true} {
					t.Run(fmt.Sprintf("wide=%t/index=%d/cache=%t/regABI=%t", wide, index, cache, regABI), func(t *testing.T) {
						cfg := sharedGrowDynamicFailureConfig().WithOptimization("shared-memory-base", cache).WithOptimization("reg-abi", regABI)
						c, err := cfg.Compile(sharedGrowDynamicFailureModule(wide, index, false))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						loaded := publicArtifactRoundTrip(t, c)
						defer loaded.Close()
						failure := sharedGrowDynamicFailureResult(wide)
						for _, compiled := range []*Compiled{c, loaded} {
							limit, err := Instantiate(compiled)
							if err != nil {
								t.Fatal(err)
							}
							got, err := limit.Invoke("run", 2, 1<<40)
							limit.Close()
							if err != nil || len(got) != 3 || got[1] != 1 {
								t.Fatalf("request equal to remaining limit: %x, %v", got, err)
							}
							for _, name := range []string{"run", "vector"} {
								in, err := Instantiate(compiled)
								if err != nil {
									t.Fatal(err)
								}
								defer in.Close()
								steps := []struct{ delta, want uint64 }{{3, failure}, {0, 1}, {1, 1}, {2, failure}, {0, 2}, {^uint64(0), failure}, {1 << 32, 2}}
								if wide {
									steps[len(steps)-1].want = failure
								}
								for _, step := range steps {
									var got []uint64
									if name == "run" {
										got, err = in.Invoke(name, step.delta, 1<<40)
									} else {
										got, err = in.Invoke(name, step.delta)
									}
									if err != nil || len(got) != 3 {
										t.Fatalf("%s(%x):%x,%v", name, step.delta, got, err)
									}
									if name == "run" {
										if got[0] != (1<<40)+2 || got[1] != step.want || got[2] != (1<<40)+1 {
											t.Fatalf("run(%x):%x;want%x", step.delta, got, step.want)
										}
									} else if got[0] != 0x0706050403020100 || got[1] != 0x0f0e0d0c0b0a0908 || got[2] != step.want {
										t.Fatalf("vector(%x):%x;want%x", step.delta, got, step.want)
									}
								}
								_, err = in.Invoke("trap-before", 3, 1<<40)
								var trap *TrapError
								if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
									t.Fatalf("prior trap: %v", err)
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestSharedGrowDynamicFailureRetainsImportedOwner(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			t.Run(fmt.Sprintf("wide=%t/index=%d", wide, index), func(t *testing.T) {
				cfg := sharedGrowDynamicFailureConfig()
				code, err := cfg.Compile(sharedGrowDynamicFailureModule(wide, index, false))
				if err != nil {
					t.Fatal(err)
				}
				defer code.Close()
				provider, err := Instantiate(code)
				if err != nil {
					t.Fatal(err)
				}
				defer provider.Close()
				if got, err := provider.Invoke("run", 1, 9); err != nil || len(got) != 3 || got[1] != 1 {
					t.Fatalf("provider grow:%v,%v", got, err)
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
				imported, err := cfg.Compile(sharedGrowDynamicFailureModule(wide, index, true))
				if err != nil {
					t.Fatal(err)
				}
				defer imported.Close()
				consumer, err := Instantiate(imported, InstantiateOptions{Imports: imports, memoryLimitPages: 2})
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				if err := provider.Close(); err != nil {
					t.Fatal(err)
				}
				for _, step := range []struct{ delta, want uint64 }{{3, sharedGrowDynamicFailureResult(wide)}, {2, sharedGrowDynamicFailureResult(wide)}, {0, 2}} {
					got, err := consumer.Invoke("run", step.delta, 9)
					if err != nil || len(got) != 3 || got[0] != 11 || got[1] != step.want || got[2] != 10 {
						t.Fatalf("imported grow(%x):%x,%v", step.delta, got, err)
					}
				}
			})
		}
	}
}

func TestSharedGrowDynamicFailureCancellation(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			t.Run(fmt.Sprintf("wide=%t/index=%d", wide, index), func(t *testing.T) {
				c, err := sharedGrowDynamicFailureConfig().Compile(sharedGrowDynamicFailureModule(wide, index, false))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				memory, err := in.ExportedMemory("memory")
				if err != nil {
					in.Close()
					t.Fatal(err)
				}
				flag := (*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[4]))
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				watchdog := time.AfterFunc(5*time.Second, cancel)
				defer watchdog.Stop()
				done := make(chan error, 1)
				go func() { _, err := in.InvokeContext(ctx, "spin", 3); done <- err }()
				for atomic.LoadUint32(flag) == 0 {
					select {
					case err := <-done:
						in.Close()
						t.Fatalf("spin returned before native entry:%v", err)
					case <-ctx.Done():
						t.Fatal("native entry timed out")
					default:
						runtime.Gosched()
					}
				}
				cancel()
				select {
				case err := <-done:
					in.Close()
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancel:%v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("native loop ignored cancellation")
				}
			})
		}
	}
}

func BenchmarkSharedGrowDynamicFailureLoop(b *testing.B) {
	const iterations = 1024
	for _, wide := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, path := range []struct {
				name       string
				base, mask uint64
			}{{"proven-failure", 3, 0}, {"quota-helper-failure", 1, 0}, {"mixed-failures", 1, 2}} {
				b.Run(fmt.Sprintf("wide=%t/index=%d/%s", wide, index, path.name), func(b *testing.B) {
					c, err := sharedGrowDynamicFailureConfig().Compile(sharedGrowDynamicFailureModule(wide, index, false))
					if err != nil {
						b.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c, InstantiateOptions{memoryLimitPages: 1})
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
						got, err := in.Invoke("loop", iterations, path.base, path.mask)
						if err != nil || len(got) != 1 || got[0] != want {
							b.Fatalf("loop:%x,%v", got, err)
						}
					}
					b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/iterations, "ns/grow")
					b.ReportMetric(float64(len(c.code)), "code-B")
				})
			}
		}
	}
}
func BenchmarkSharedGrowDynamicFailureCompile(b *testing.B) {
	for _, wide := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			b.Run(fmt.Sprintf("wide=%t/index=%d", wide, index), func(b *testing.B) {
				cfg, module := sharedGrowDynamicFailureConfig(), sharedGrowDynamicFailureModule(wide, index, false)
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

func TestSharedGrowDynamicFailurePreservesCollectorLocal(t *testing.T) {
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
				c, err := sharedGrowDynamicFailureConfig().Compile(module)
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
				for n, delta := range []uint64{3, 0, 1, 3, 0, 1 << 32, 0} {
					want := uint64(2)
					if n == 1 || n == 2 {
						want = 1
					}
					if n == 0 || n == 3 || n == 5 && address64 {
						want = sharedGrowDynamicFailureResult(address64)
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

func sharedGrowDynamicFailureLargeConsumer(t *testing.T, index byte, cfg *RuntimeConfig) *Instance {
	t.Helper()
	providerCode, err := cfg.Compile(sharedGrowDynamicFailureModule(true, index, false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { providerCode.Close() })
	provider, err := Instantiate(providerCode)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { provider.Close() })
	imports := NewImports()
	for n := 0; n < 2; n++ {
		name := fmt.Sprintf("memory%d", n)
		m, err := provider.ExportedMemory(name)
		if err != nil {
			t.Fatal(err)
		}
		imports.Memory("env", name, m)
	}
	code, err := cfg.Compile(sharedGrowDynamicFailureModuleWithMaximum(true, index, true, (1<<40)+1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { code.Close() })
	consumer, err := Instantiate(code, InstantiateOptions{Imports: imports, memoryLimitPages: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { consumer.Close() })
	return consumer
}
func TestSharedGrowDynamicFailureLargeDeclaredBound(t *testing.T) {
	for _, index := range []byte{0, 1} {
		t.Run(fmt.Sprintf("index=%d", index), func(t *testing.T) {
			in := sharedGrowDynamicFailureLargeConsumer(t, index, sharedGrowDynamicFailureConfig())
			for _, step := range []struct{ delta, want uint64 }{{(1 << 40) + 1, ^uint64(0)}, {1 << 40, ^uint64(0)}, {1 << 39, ^uint64(0)}, {0, 1}, {1, 1}, {0, 2}} {
				got, err := in.Invoke("run", step.delta, 9)
				if err != nil || len(got) != 3 || got[0] != 11 || got[1] != step.want || got[2] != 10 {
					t.Fatalf("grow(%x):%x,%v;want%x", step.delta, got, err, step.want)
				}
			}
		})
	}
}
