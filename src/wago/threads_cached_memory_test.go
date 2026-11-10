//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestThreadsSharedBackingCacheLeavesUnsharedCodeUnchanged(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	for _, probe := range []struct {
		op   byte
		simd bool
	}{{0x28, false}, {0x36, false}, {0, true}, {11, true}} {
		module := threadsMemoryAccessModule(probe.op, probe.simd, false, false, 0)
		cached, err := cfg.WithOptimization("shared-memory-base", true).Compile(module)
		if err != nil {
			t.Fatal(err)
		}
		defer cached.Close()
		ordinary, err := cfg.WithOptimization("shared-memory-base", false).Compile(module)
		if err != nil {
			t.Fatal(err)
		}
		defer ordinary.Close()
		if !bytes.Equal(cached.code, ordinary.code) {
			t.Fatalf("unshared op=%x simd=%t changed native code", probe.op, probe.simd)
		}
	}
}

func threadsCachedMemoryCallModule(imported bool) []byte {
	callee := []byte{
		0x20, 0, 0x40, 0, 0x1a, // dynamic grow, then discard previous size
		0x20, 1, 0x41, 0x7f, 0x41, 33, 0xfc, 11, 0, // fill 33 bytes with 255
		0x20, 1, 0x41, 1, 0x6a, 0x20, 1, 0x41, 32, 0xfc, 10, 0, 0,
		0x41, 0, 0x41, 0, 0xfe, 0, 2, 0, 0x1a, // synchronous notify bridge
		0x20, 1, 0x41, 1, 0xfe, 0x1e, 2, 0, 0x0b, // RMW returns 0xffffffff
	}
	var caller []byte
	for n := int64(1); n <= 8; n++ {
		caller = append(caller, 0x42)
		caller = append(caller, wasmtest.SLEB64(n)...)
	}
	caller = append(caller, 0x41, 1, 0x41)
	caller = append(caller, wasmtest.SLEB32(65536)...)
	caller = append(caller, 0x10, 0, 0xad) // call with eight live values, extend result
	for n := 0; n < 8; n++ {
		caller = append(caller, 0x7c)
	}
	caller = append(caller, 0x0b)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
	))}
	if imported {
		entry := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
		entry = append(entry, 2, 3, 1, 3)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 3})))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(callee), wasmtest.Code(caller))),
	)
	return wasmtest.Module(sections...)
}

func TestThreadsSharedBackingCacheCallsGrowthPressureAndCodec(t *testing.T) {
	for _, imported := range []bool{false, true} {
		for _, cache := range []bool{false, true} {
			for _, regABI := range []bool{false, true} {
				t.Run(fmt.Sprintf("imported=%t/cache=%t/regabi=%t", imported, cache, regABI), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).
						WithOptimization("shared-memory-base", cache).WithOptimization("reg-abi", regABI)
					c, err := cfg.Compile(threadsCachedMemoryCallModule(imported))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					for _, compiled := range []*Compiled{c} {
						var opts any = InstantiateOptions{}
						if imported {
							memory, err := NewSharedMemory(1, 3)
							if err != nil {
								t.Fatal(err)
							}
							defer memory.Close()
							opts = testImports("env.memory", memory)
						}
						in, err := Instantiate(compiled, opts)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						got, err := in.Invoke("run")
						if err != nil || !slices.Equal(got, []uint64{0xffffffff + 36}) {
							t.Fatalf("result after nested call/grow/notify = %v, %v", got, err)
						}
						mem := in.mem()
						if len(mem) != 2*65536 || !slices.Equal(mem[65536:65540], []byte{0, 0, 0, 0}) || mem[65568] != 255 {
							t.Fatal("cached path addressed the wrong backing after growth")
						}
					}
				})
			}
		}
	}
}
