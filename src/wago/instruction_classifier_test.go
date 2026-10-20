//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func classifierExecutionModule() []byte {
	params := make([]wasm.ValType, 17)
	for i := range params {
		params[i] = wasm.I32
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/recipe", "classifier", 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 11, 0x20, 0, 0x10, 0, 0x6a, 0x0b}))),
	)
}

func classifierExecutionExtension(shape string) instructionRecipeExt {
	return instructionRecipeExt{name: "classifier", input: []int32{32}, output: 32,
		lower: func(c LoweringContext) error {
			x := c.Input(0)
			switch shape {
			case "normal":
				x = c.Add(x, c.Const(32, 1))
			case "shared", "unreachable":
				v := c.Const(32, 1)
				for range 8 {
					v = c.Add(v, v)
				}
				if shape == "shared" {
					x = c.Add(x, v)
				}
			case "deep":
				for range 128 {
					x = c.Not(x)
				}
			}
			c.Output(0, x)
			return nil
		},
		eval: func(v []uint32) uint32 {
			switch shape {
			case "normal":
				return v[0] + 1
			case "shared":
				return v[0] + 256
			default:
				return v[0]
			}
		},
	}
}

func TestInstructionClassifierSharedConstantsExecute(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	ext := classifierExecutionExtension("shared")
	if err := rt.Use(ext); err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(classifierExecutionModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	args := make([]uint64, 17)
	for _, v := range []uint32{0, 7, 0x7fffffff, 0xffffffff} {
		args[0] = uint64(v)
		want := v + 256 + 11
		if out, err := in.Invoke("run", args...); err != nil || len(out) != 1 || uint32(out[0]) != want {
			t.Fatalf("input%x: %v,%v;want%x", v, out, err, want)
		}
	}
}

func BenchmarkInstructionClassifierCompile(b *testing.B) {
	for _, shape := range []string{"normal", "shared", "deep", "unreachable"} {
		b.Run(shape, func(b *testing.B) {
			rt := NewRuntime()
			defer rt.Close()
			if err := rt.Use(classifierExecutionExtension(shape)); err != nil {
				b.Fatal(err)
			}
			module := classifierExecutionModule()
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
