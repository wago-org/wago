//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"context"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func instructionTailChecksumModule(mode string) []byte {
	step := []byte{0x20, 0, 0x20, 1, 0x12, 0}
	if mode == "fallback" {
		step[len(step)-1] = 1
	} else if mode == "mixed" {
		step = []byte{0x20, 1, 0x41, 1, 0x71, 0x04, 0x7f,
			0x20, 0, 0x20, 1, 0x12, 0, 0x05,
			0x20, 0, 0x20, 1, 0x12, 1, 0x0b}
	}
	step = append(step, 0x0b)
	body := []byte{
		0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1,
		0x20, 1, 0x20, 0, 0x10, 2, 0x21, 1,
		0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/checksum", "native", 0), instructionFuncImport("wago:instr/checksum", "portable", 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 3))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(step), wasmtest.Code(body))),
	)
}

func TestCustomImportedTailChecksumLoop(t *testing.T) {
	for _, mode := range []string{"fast", "fallback", "mixed"} {
		t.Run(mode, func(t *testing.T) {
			var calls uint64
			rt := checksumRuntime(t, &calls)
			mod, err := rt.Compile(instructionTailChecksumModule(mode))
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			in, err := rt.Instantiate(context.Background(), mod)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for _, count := range []int32{0, 1, 31, 256, 1023} {
				want := uint32(123)
				for n := count; n > 0; n-- {
					want = ((want + uint32(n)) ^ 0x5a) & 255
				}
				out, err := in.Invoke("run", I32(count), I32(123))
				if err != nil || len(out) != 1 || uint32(out[0]) != want {
					t.Fatalf("count%d: %v, %v; want%d", count, out, err, want)
				}
			}
			t.Logf("code=%d bytes, handler calls=%d", mod.Compiled().CodeSize(), calls)
		})
	}
}

func BenchmarkInstructionTailInvoke(b *testing.B) {
	for _, mode := range []string{"fast", "fallback", "mixed"} {
		b.Run(mode, func(b *testing.B) {
			var calls uint64
			rt := checksumRuntime(b, &calls)
			mod, err := rt.Compile(instructionTailChecksumModule(mode))
			if err != nil {
				b.Fatal(err)
			}
			defer mod.Close()
			in, err := rt.Instantiate(context.Background(), mod)
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := in.Invoke("run", I32(256), I32(123)); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(calls)/float64(b.N), "helpers/op")
			b.ReportMetric(256, "guest-ops/op")
			b.ReportMetric(float64(mod.Compiled().CodeSize()), "code-B")
		})
	}
}

func BenchmarkInstructionTailCompile(b *testing.B) {
	for _, mode := range []string{"fast", "fallback", "mixed"} {
		b.Run(mode, func(b *testing.B) {
			var calls uint64
			rt := checksumRuntime(b, &calls)
			module := instructionTailChecksumModule(mode)
			var size int
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				mod, err := rt.Compile(module)
				if err != nil {
					b.Fatal(err)
				}
				size = mod.Compiled().CodeSize()
				mod.Close()
			}
			b.StopTimer()
			b.ReportMetric(float64(size), "code-B")
		})
	}
}

func BenchmarkInstructionTailBudgetCompile(b *testing.B) {
	for _, steps := range []int{1, 4, 8, 12} {
		b.Run(fmt.Sprintf("shared%d", steps), func(b *testing.B) {
			calls := 0
			ext := sharedInstructionRecipe(steps, &calls)
			rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
			defer rt.Close()
			if err := rt.Use(ext); err != nil {
				b.Fatal(err)
			}
			module := instructionTailModule("wago:instr/recipe", ext.name, 1, true, false)
			var size int
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				mod, err := rt.Compile(module)
				if err != nil {
					b.Fatal(err)
				}
				size = mod.Compiled().CodeSize()
				mod.Close()
			}
			b.StopTimer()
			b.ReportMetric(float64(size), "code-B")
		})
	}
}
