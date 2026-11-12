//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"

	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestThreadsLSEMatchesExclusiveLoops(t *testing.T) {
	if !shared.ARM64SupportsLSE() {
		t.Skip("host has no LSE instructions")
	}
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	for _, cmpxchg := range []bool{false, true} {
		for operation := 0; operation < 6; operation++ {
			if cmpxchg && operation != 0 {
				continue
			}
			for width, spec := range []struct {
				align byte
				typ   wasm.ValType
			}{
				{2, wasm.I32}, {3, wasm.I64}, {0, wasm.I32}, {1, wasm.I32},
				{0, wasm.I64}, {1, wasm.I64}, {2, wasm.I64},
			} {
				sub := byte(0x1e + operation*7 + width)
				module := sharedAtomicRMWModule(sub, spec.align, spec.typ)
				name := "rmw"
				if cmpxchg {
					sub = byte(0x48 + width)
					module = sharedAtomicCmpxchgModule(sub, spec.align, spec.typ)
					name = "cmpxchg"
				}
				t.Run(fmt.Sprintf("%s/%x", name, sub), func(t *testing.T) {
					fast, err := cfg.WithOptimization("atomic-lse", true).Compile(module)
					if err != nil {
						t.Fatal(err)
					}
					defer fast.Close()
					slow, err := cfg.WithOptimization("atomic-lse", false).Compile(module)
					if err != nil {
						t.Fatal(err)
					}
					defer slow.Close()
					if !fast.requiresARM64LSE || slow.requiresARM64LSE || bytes.Equal(fast.code, slow.code) {
						t.Fatal("LSE selection/requirements do not distinguish native and fallback paths")
					}

					instances := make([]*Instance, 0, 3)
					for _, compiled := range []*Compiled{slow, fast, fast} {
						memory, err := NewSharedMemory(1, 1)
						if err != nil {
							t.Fatal(err)
						}
						defer memory.Close()
						in, err := Instantiate(compiled, testImports("env.memory", memory))
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						instances = append(instances, in)
					}
					values := []uint64{0, 1, 0xff, 0xffff, 0xffffffff, 0x80000000ffffffff, 1 << 63, ^uint64(0)}
					for _, initial := range values {
						for _, operand := range values {
							for _, match := range []bool{false, true} {
								var reference []uint64
								var contents []byte
								for i, in := range instances {
									mem := in.mem()
									binary.LittleEndian.PutUint64(mem, initial)
									args := []uint64{operand}
									if cmpxchg {
										expected := initial ^ 1
										if match {
											expected = initial
										}
										args = []uint64{expected, operand}
									}
									got, err := in.Invoke(name, args...)
									if err != nil {
										t.Fatal(err)
									}
									if i == 0 {
										reference, contents = slices.Clone(got), slices.Clone(mem[:8])
									} else if !slices.Equal(got, reference) || !bytes.Equal(mem[:8], contents) {
										t.Fatalf("initial=%x operand=%x match=%t: result=%x/%x memory=%x/%x", initial, operand, match, got, reference, mem[:8], contents)
									}
								}
							}
						}
					}
				})
			}
		}
	}
}
