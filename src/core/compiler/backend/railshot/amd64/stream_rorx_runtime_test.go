//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"golang.org/x/sys/cpu"
	"math/bits"
	"testing"
)

func TestStreamRorxRuntimeRetainedAlias(t *testing.T) {
	if !cpu.X86.HasBMI2 {
		t.Skip("native RORX execution requires host BMI2")
	}
	streamMemoryTestEnable(t)
	old, oldDense := streamingRorxEnabled, streamingPreserveDenseEnabled
	streamingRorxEnabled, streamingPreserveDenseEnabled = true, true
	t.Cleanup(func() { streamingRorxEnabled, streamingPreserveDenseEnabled = old, oldDense })
	for _, op := range []byte{0x77, 0x78, 0x89, 0x8a} {
		wide, _ := streamRotateShape(op)
		typ := streamNumericType(wide)
		for _, count := range []uint64{0, 7, 32, 65, ^uint64(0)} {
			body := append([]byte{0, 0x20, 0}, streamConstBytes(wide, count)...)
			xor := byte(0x73)
			if wide {
				xor = 0x85
			}
			body = append(body, op, 0x20, 0, xor, 0x0b)
			m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
			for _, interruptible := range []bool{false, true} {
				for _, x := range []uint64{0, 0xdeadbeef87654321, ^uint64(0)} {
					var stats ModuleStats
					opts := CompileOptions{Interruptible: interruptible, AMD64FeaturesSet: true, AMD64Features: shared.AMD64BMI2, Optimizations: map[string]bool{"bmi2-rorx": true}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, _, err := runMemAmd64WithOptions(t, m, opts, nil, x)
					if err != nil {
						t.Fatal(err)
					}
					n := int(count & 63)
					if op == 0x78 || op == 0x8a {
						n = -n
					}
					want := bits.RotateLeft64(x, n) ^ x
					if !wide {
						want = uint64(bits.RotateLeft32(uint32(x), n) ^ uint32(x))
					}
					if got != want {
						t.Fatalf("op=%x count=%x x=%x interruptible=%v got=%x want=%x", op, count, x, interruptible, got, want)
					}
					if diagnosticsEnabled && !regallocCheckEnabled {
						if len(stats.Funcs) != 1 || stats.Funcs[0].Peephole["streaming-function"] != 1 || stats.Funcs[0].Peephole["streaming-rotate-rorx"] != 1 {
							t.Fatal("runtime case did not execute positively selected streaming RORX", stats.Funcs)
						}
					}
				}
			}
		}
	}
}
