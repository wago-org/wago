//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func threadsMemory64Module(imported bool) []byte {
	return threadsMemory64ModuleWithSharing(imported, true)
}

func threadsMemory64ModuleWithSharing(imported, shared bool) []byte {
	limits := byte(5)
	if shared {
		limits = 7
	}
	types := [][]byte{
		wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
		wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
		wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I32}),
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
	if imported {
		entry := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(append(entry, 2, limits, 1, 3))))
	}
	indexes := []byte{0, 1, 1, 2, 3, 4, 5, 6, 1, 1, 0, 0, 5, 6, 1, 3}
	decls, exports := make([][]byte, len(indexes)), [][]byte{wasmtest.ExportEntry("memory", 2, 0)}
	names := []string{"load", "store", "add", "cas", "size", "grow", "wait32", "wait64", "notify", "wait-zero", "load-offset", "load-overflow", "wait32-overflow", "wait64-overflow", "notify-overflow", "grow-high-constant"}
	for i, index := range indexes {
		decls[i] = []byte{index}
		exports = append(exports, wasmtest.ExportEntry(names[i], 0, uint32(i)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(decls...)))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{limits, 1, 3})))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(
		wasmtest.Code([]byte{0x20, 0, 0xfe, 0x10, 2, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0xfe, 0x17, 2, 0, 0x20, 0, 0xfe, 0x10, 2, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0xfe, 0x1e, 2, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfe, 0x48, 2, 0, 0x0b}),
		wasmtest.Code([]byte{0x3f, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfe, 1, 2, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfe, 2, 3, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0xfe, 0, 2, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x42, 0, 0xfe, 1, 2, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0xfe, 0x10, 2, 0x80, 0x80, 0x80, 0x80, 0x10, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0xfe, 0x10, 2, 4, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfe, 1, 2, 4, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfe, 2, 3, 8, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0xfe, 0, 2, 4, 0x0b}),
		wasmtest.Code([]byte{0x42, 0x80, 0x80, 0x80, 0x80, 0x10, 0x40, 0, 0x0b}),
	)))
	return wasmtest.Module(sections...)
}

func TestThreadsSharedMemory64BoundsGrowthWaitsAndCodec(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMemory64)
	for _, imported := range []bool{false, true} {
		for _, cache := range []bool{false, true} {
			for _, regABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("imported=%t/cache=%t/regabi=%t", imported, cache, regABI), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).
						WithOptimization("shared-memory-base", cache).WithOptimization("reg-abi", regABI)
					compiled, err := cfg.Compile(threadsMemory64Module(imported))
					if err != nil {
						t.Fatal(err)
					}
					defer compiled.Close()
					for _, c := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
						imports := NewImports()
						if imported {
							providerModule, err := cfg.Compile(threadsMemory64Module(false))
							if err != nil {
								t.Fatal(err)
							}
							defer providerModule.Close()
							provider, err := Instantiate(providerModule)
							if err != nil {
								t.Fatal(err)
							}
							defer provider.Close()
							memory, err := provider.ExportedMemory("memory")
							if err != nil {
								t.Fatal(err)
							}
							imports.Memory("env", "memory", memory)
						}
						in, err := Instantiate(c, imports)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						check := func(name string, want uint64, args ...uint64) {
							t.Helper()
							got, err := in.Invoke(name, args...)
							if err != nil || len(got) != 1 || got[0] != want {
								t.Fatalf("%s: %v, %v; want %d", name, got, err, want)
							}
						}
						check("size", 1)
						check("grow", 1, 0)
						check("store", 7, 0, 7)
						check("add", 7, 0, 2)
						check("cas", 9, 0, 9, 7)
						check("load", 7, 0)
						check("wait32", 1, 0, 8, I64(-1))
						check("wait32", 2, 0, 7, 0)
						check("wait64", 2, 0, 7, 0)
						check("wait-zero", 2, 0, 7)
						check("notify", 0, 0, 1)
						for _, name := range []string{"load", "store", "add", "cas", "wait32", "wait64", "wait-zero", "notify"} {
							for _, address := range []uint64{1 << 32, math.MaxUint64 - 3} {
								args := []uint64{address, 7, 0}
								if name == "load" {
									args = args[:1]
								}
								if name == "store" || name == "add" || name == "wait-zero" || name == "notify" {
									args = args[:2]
								}
								if _, err := in.Invoke(name, args...); err == nil {
									t.Fatalf("%s accepted high address %#x", name, address)
								}
							}
						}
						if _, err := in.Invoke("load-offset", 0); err == nil {
							t.Fatal("64-bit memarg offset truncated")
						}
						if _, err := in.Invoke("load-overflow", math.MaxUint64-3); err == nil {
							t.Fatal("effective address wrapped into shared memory")
						}
						for _, tc := range []struct {
							name string
							args []uint64
						}{
							{"wait32-overflow", []uint64{math.MaxUint64 - 3, 7, 0}},
							{"wait64-overflow", []uint64{math.MaxUint64 - 7, 7, 0}},
							{"notify-overflow", []uint64{math.MaxUint64 - 3, 1}},
						} {
							if _, err := in.Invoke(tc.name, tc.args...); err == nil {
								t.Fatalf("%s wrapped into backing", tc.name)
							}
						}
						check("load", 7, 0)
						check("grow", math.MaxUint64, 1<<32)
						check("grow-high-constant", math.MaxUint64)
						check("size", 1)
						check("grow", 1, 1)
						check("size", 2)
						check("store", 11, 65536, 11)
						check("load", 11, 65536)
						check("grow", math.MaxUint64, math.MaxUint64)
						check("size", 2)
					}
				})
			}
		}
	}
}

func TestThreadsSharedMemory64BlockingWaitGrowthAndCancellation(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMemory64)
	for _, regABI := range []bool{false, true} {
		t.Run(fmt.Sprintf("regabi=%t", regABI), func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", regABI)
			producerModule, err := cfg.Compile(threadsMemory64Module(false))
			if err != nil {
				t.Fatal(err)
			}
			defer producerModule.Close()
			consumerModule, err := cfg.Compile(threadsMemory64Module(true))
			if err != nil {
				t.Fatal(err)
			}
			defer consumerModule.Close()
			producer, err := Instantiate(producerModule)
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			memory, err := producer.ExportedMemory("memory")
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := Instantiate(consumerModule, NewImports().Memory("env", "memory", memory))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			for _, wait := range []string{"wait32", "wait64"} {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				done := make(chan error, 1)
				go func() {
					got, err := producer.InvokeContext(ctx, wait, 0, 0, I64(-1))
					if err == nil && (len(got) != 1 || got[0] != 0) {
						err = fmt.Errorf("wait result = %v", got)
					}
					done <- err
				}()
				waitForMemoryWaiters(t, memory, 1)
				if wait == "wait32" {
					if got, err := consumer.Invoke("grow", 1); err != nil || got[0] != 1 {
						t.Fatalf("grow while waiting = %v, %v", got, err)
					}
				}
				if got, err := consumer.Invoke("notify", 0, 1); err != nil || got[0] != 1 {
					t.Fatalf("notify = %v, %v", got, err)
				}
				err := <-done
				cancel()
				if err != nil {
					t.Fatal(err)
				}
			}
			if got, err := producer.Invoke("store", 65536, 19); err != nil || got[0] != 19 {
				t.Fatalf("resumed producer growth = %v, %v", got, err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := producer.InvokeContext(ctx, "wait64", 0, 0, I64(-1)); done <- err }()
			waitForMemoryWaiters(t, memory, 1)
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled wait = %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("canceled wait did not resume")
			}
		})
	}
}

func threadsULEB64(v uint64) []byte {
	var result []byte
	for {
		b := byte(v & 127)
		v >>= 7
		if v != 0 {
			b |= 128
		}
		result = append(result, b)
		if v == 0 {
			return result
		}
	}
}

func TestThreadsSharedMemory64ScalarAndSIMDAccess(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMemory64)
	for _, regABI := range []bool{false, true} {
		cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", regABI)
		for _, simd := range []bool{false, true} {
			var ops []byte
			if simd {
				ops = []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 84, 85, 86, 87, 88, 89, 90, 91, 92, 93}
			} else {
				for op := byte(0x28); op <= 0x3e; op++ {
					ops = append(ops, op)
				}
			}
			for _, op := range ops {
				for _, offset := range []uint64{0, 1, 1 << 32, ^uint64(0) - 3} {
					t.Run(fmt.Sprintf("regabi=%t/simd=%t/op=%x/offset=%x", regABI, simd, op, offset), func(t *testing.T) {
						ordinary, err := cfg.Compile(threadsMemoryAccessModuleForAddress(op, simd, false, false, offset, true))
						if err != nil {
							t.Fatal(err)
						}
						defer ordinary.Close()
						reference, err := Instantiate(ordinary)
						if err != nil {
							t.Fatal(err)
						}
						defer reference.Close()
						shared, err := cfg.Compile(threadsMemoryAccessModuleForAddress(op, simd, true, false, offset, true))
						if err != nil {
							t.Fatal(err)
						}
						defer shared.Close()
						for _, compiled := range []*Compiled{shared, publicArtifactRoundTrip(t, shared)} {
							in, err := Instantiate(compiled)
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							for _, address := range []uint64{0, 1, 8, 65520, 65532, 65535, 65536, 1<<32 | 8, ^uint64(0) - 3, ^uint64(0)} {
								for _, instance := range []*Instance{reference, in} {
									mem := instance.mem()
									for i := range mem {
										mem[i] = byte(i*37 + 0x81)
									}
								}
								a, ae := reference.Invoke("access", address, 0x80000000ffffffff)
								b, be := in.Invoke("access", address, 0x80000000ffffffff)
								if (ae == nil) != (be == nil) {
									t.Fatalf("address=%x: unshared=%v shared=%v", address, ae, be)
								}
								if ae != nil {
									at, aok := ae.(*TrapError)
									bt, bok := be.(*TrapError)
									if !aok || !bok || at.Code != bt.Code {
										t.Fatalf("trap codes: %v vs %v", ae, be)
									}
								} else if !slices.Equal(a, b) {
									t.Fatalf("results: %x vs %x", a, b)
								}
								if !bytes.Equal(reference.mem(), in.mem()) {
									t.Fatalf("address=%x: memory contents differ", address)
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestThreadsSharedMemory64BulkBoundsAndOverlap(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMemory64)
	for _, regABI := range []bool{false, true} {
		for _, fill := range []bool{false, true} {
			for _, length := range []int{-1, 0, 1, 33, 64, 65} {
				t.Run(fmt.Sprintf("regabi=%t/fill=%t/length=%d", regABI, fill, length), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", regABI)
					c, err := cfg.Compile(threadsBulkMemoryModuleForAddress(fill, false, 1, length, true))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
						in, err := Instantiate(compiled)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						counts := []uint64{0}
						if length < 0 {
							counts = []uint64{0, 1, 33, 64, 65, 1024, 65536, 1 << 32, ^uint64(0)}
						}
						for _, n := range counts {
							for _, pair := range [][2]uint64{{0, 0}, {1, 0}, {0, 1}, {7, 1}, {1, 7}, {65520, 0}, {0, 65520}, {65536, 65536}, {65537, 0}, {1<<32 | 1, 7}, {1, 1<<32 | 7}, {^uint64(0) - 3, 0}, {0, ^uint64(0) - 3}} {
								mem := in.mem()
								for i := range mem {
									mem[i] = byte(i*37 + 0x81)
								}
								want := bytes.Clone(mem)
								count := n
								if length >= 0 {
									count = uint64(length)
								}
								dst, src := pair[0], pair[1]
								valid := dst <= uint64(len(mem)) && count <= uint64(len(mem))-dst
								if !fill {
									valid = valid && src <= uint64(len(mem)) && count <= uint64(len(mem))-src
								}
								if valid {
									if fill {
										for i := dst; i < dst+count; i++ {
											want[i] = byte(src)
										}
									} else {
										copy(want[dst:dst+count], want[src:src+count])
									}
								}
								_, err := in.Invoke("bulk", dst, src, n)
								if valid && err != nil {
									t.Fatalf("dst=%x src=%x count=%x: %v", dst, src, count, err)
								}
								if !valid {
									trap, ok := err.(*TrapError)
									if !ok || trap.Code != TrapLinMemOutOfBounds {
										t.Fatalf("dst=%x src=%x count=%x: expected bounds trap, got %v", dst, src, count, err)
									}
								}
								if !bytes.Equal(want, mem) {
									t.Fatalf("dst=%x src=%x count=%x: memory mismatch", dst, src, count)
								}
							}
						}
					}
				})
			}
		}
	}
}

func TestThreadsSharedMemory64DataSegmentsAndCodec(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMemory64)
	for _, imported := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			for _, count := range []int{-1, 0, 1, 33, 64} {
				t.Run(fmt.Sprintf("imported=%t/regabi=%t/count=%d", imported, regABI, count), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", regABI)
					c, err := cfg.Compile(threadsDataModuleForAddress(imported, 10, count, true))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
						imports := NewImports()
						if imported {
							providerModule, err := cfg.Compile(threadsMemory64Module(false))
							if err != nil {
								t.Fatal(err)
							}
							defer providerModule.Close()
							provider, err := Instantiate(providerModule)
							if err != nil {
								t.Fatal(err)
							}
							defer provider.Close()
							memory, err := provider.ExportedMemory("memory")
							if err != nil {
								t.Fatal(err)
							}
							imports.Memory("env", "memory", memory)
						}
						in, err := Instantiate(compiled, imports)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						if !bytes.Equal(in.mem()[8:13], []byte{0x11, 0x22, 0x55, 0x66, 0x77}) {
							t.Fatalf("active segment order: %x", in.mem()[8:13])
						}
						for _, args := range [][3]uint64{{32, 0, 64}, {33, 1, 33}, {65535, 7, 1}, {65536, 8, 0}, {65536, 0, 1}, {1<<32 | 32, 0, 1}, {^uint64(0) - 3, 0, 8}, {20, 65, 0}, {20, 1<<32 | 2, 1<<32 | 3}} {
							before := bytes.Clone(in.mem())
							dst, src, n := args[0], uint64(uint32(args[1])), uint64(uint32(args[2]))
							if count >= 0 {
								n = uint64(count)
							}
							valid := dst <= uint64(len(before)) && n <= uint64(len(before))-dst && src <= 64 && n <= 64-src
							want := bytes.Clone(before)
							if valid {
								copy(want[dst:dst+n], threadsDataSeed()[src:src+n])
							}
							_, err := in.Invoke("init", args[0], args[1], args[2])
							if (err == nil) != valid {
								t.Fatalf("init %v count=%d: %v; valid=%t", args, count, err, valid)
							}
							if !bytes.Equal(want, in.mem()) {
								t.Fatalf("init %v changed incorrect bytes", args)
							}
						}
						if got, err := in.Invoke("grow", 1); err != nil || got[0] != 1 {
							t.Fatalf("grow: %v, %v", got, err)
						}
						if _, err := in.Invoke("init", 65539, 0, 8); err != nil {
							t.Fatal(err)
						}
						if _, err := in.Invoke("drop"); err != nil {
							t.Fatal(err)
						}
						if _, err := in.Invoke("init", 0, 0, 1); (count != 0 && err == nil) || (count == 0 && err != nil) {
							t.Fatalf("init dropped segment: %v", err)
						}
					}
				})
			}
		}
	}
}

func TestThreadsUnsharedMemory64Atomics(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMemory64)
	for _, imported := range []bool{false, true} {
		for _, policy := range []uint32{0, 2} {
			for _, regABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("imported=%t/policy=%d/regabi=%t", imported, policy, regABI), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", regABI).WithMemoryLimitPages(policy)
					c, err := cfg.Compile(threadsMemory64ModuleWithSharing(imported, false))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
						imports := NewImports()
						if imported {
							providerModule, err := cfg.Compile(threadsMemory64ModuleWithSharing(false, false))
							if err != nil {
								t.Fatal(err)
							}
							defer providerModule.Close()
							provider, err := Instantiate(providerModule)
							if err != nil {
								t.Fatal(err)
							}
							defer provider.Close()
							memory, err := provider.ExportedMemory("memory")
							if err != nil {
								t.Fatal(err)
							}
							imports.Memory("env", "memory", memory)
						}
						in, err := Instantiate(compiled, imports)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						check := func(name string, want uint64, args ...uint64) {
							t.Helper()
							got, err := in.Invoke(name, args...)
							if err != nil || len(got) != 1 || got[0] != want {
								t.Fatalf("%s = %v, %v; want %d", name, got, err, want)
							}
						}
						check("store", 7, 0, 7)
						check("add", 7, 0, 2)
						check("cas", 9, 0, 9, 7)
						check("load", 7, 0)
						for _, name := range []string{"load", "load-offset", "load-overflow"} {
							for _, address := range []uint64{1<<32 | 8, ^uint64(0) - 3} {
								if _, err := in.Invoke(name, address); err == nil {
									t.Fatalf("%s accepted high address %x", name, address)
								}
							}
						}
						check("grow", 1, 1)
						check("size", 2)
						check("store", 19, 65536, 19)
						check("grow", ^uint64(0), 1<<32)
						check("grow-high-constant", ^uint64(0))
						check("notify", 0, 0, 1)
						for _, name := range []string{"wait32", "wait64"} {
							_, err := in.Invoke(name, 0, 0, 0)
							trap, ok := err.(*TrapError)
							if !ok || trap.Code != TrapExpectedSharedMemory {
								t.Fatalf("%s on unshared memory: %v", name, err)
							}
						}
					}
				})
			}
		}
	}
}
