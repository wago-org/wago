//go:build !tinygo

package wago

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The loop models a byte checksum supplied by a compiler plugin. Its count and
// accumulator are guest parameters; the checksum must be evaluated each time.
type instructionChecksumExt struct{ calls *uint64 }

func (instructionChecksumExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.instruction-checksum", Version: "1.0.0", Stability: Experimental}
}

func (e instructionChecksumExt) Register(r *Registry) error {
	r.Capability(CapCompilerCodegen)
	for _, native := range []bool{false, true} {
		name := "portable"
		var lower InstructionLowerer
		if native {
			name = "native"
			lower = func(c LoweringContext) error {
				c.Output(0, c.Xor(c.Add(c.Input(0), c.Input(1)), c.Const(8, 0x5a)))
				return nil
			}
		}
		if err := r.Compiler().Instruction(InstructionSpec{
			Module: "wago:instr/checksum", Name: name, Input: []int32{8, 8}, Output: []int32{8}, Lower: lower,
			Handler: func(_ InstructionContext, args []Bits) ([]Bits, error) {
				*e.calls++
				out, err := BitsFromUint32(8, (args[0].Uint32()+args[1].Uint32())^0x5a)
				return []Bits{out}, err
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func instructionChecksumModule(mode string) []byte {
	// The same two imports remain present in each mode. Only the guest call site
	// differs, so the fallback and mixed cases keep identical plugin contracts.
	body := []byte{0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1}
	if mode == "mixed" {
		body = append(body, 0x20, 0, 0x41, 1, 0x71, 0x04, 0x7f,
			0x20, 1, 0x20, 0, 0x10, 0, 0x05, 0x20, 1, 0x20, 0, 0x10, 1, 0x0b)
	} else {
		importIndex := byte(0)
		if mode == "fallback" {
			importIndex = 1
		}
		body = append(body, 0x20, 1, 0x20, 0, 0x10, importIndex)
	}
	body = append(body, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("wago:instr/checksum", "native", 0), instructionFuncImport("wago:instr/checksum", "portable", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func checksumRuntime(t testing.TB, calls *uint64) *Runtime {
	t.Helper()
	rt := NewRuntime()
	t.Cleanup(func() { rt.Close() })
	if err := rt.Use(instructionChecksumExt{calls: calls}); err != nil {
		t.Fatal(err)
	}
	return rt
}

func TestCustomInstructionChecksumLoop(t *testing.T) {
	for _, mode := range []string{"fast", "fallback", "mixed"} {
		t.Run(mode, func(t *testing.T) {
			var calls uint64
			rt := checksumRuntime(t, &calls)
			mod, err := rt.Compile(instructionChecksumModule(mode))
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			if directory := os.Getenv("WAGO_RECIPE_CODE_DIR"); directory != "" {
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
				file, err := os.Create(filepath.Join(directory, mode+".bin"))
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := mod.Compiled().WriteCodeTo(file)
				closeErr := file.Close()
				if writeErr != nil || closeErr != nil {
					t.Fatalf("write code: %v; close: %v", writeErr, closeErr)
				}
			}
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
					t.Fatalf("count %d: %v, %v; want %d", count, out, err, want)
				}
			}
			t.Logf("code=%d bytes, handler calls=%d", mod.Compiled().CodeSize(), calls)
		})
	}
}

func BenchmarkInstructionRecipeInvoke(b *testing.B) {
	for _, mode := range []string{"fast", "fallback", "mixed"} {
		b.Run(mode, func(b *testing.B) {
			var calls uint64
			rt := checksumRuntime(b, &calls)
			mod, err := rt.Compile(instructionChecksumModule(mode))
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

func BenchmarkInstructionRecipeCompile(b *testing.B) {
	for _, mode := range []string{"fast", "fallback", "mixed"} {
		b.Run(mode, func(b *testing.B) {
			var calls uint64
			rt := checksumRuntime(b, &calls)
			module := instructionChecksumModule(mode)
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
