//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"math"
	"os"
	"testing"
)

func TestExperimentalSumMatrixARM64(t *testing.T) {
	saved := shared.SumExperiment
	defer func() { shared.SumExperiment = saved }()
	for _, variant := range []string{"A", "B", "C", "D", "E", "F", "G", "H"} {
		shared.SumExperiment = variant
		for _, n := range []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16, 17, 511, 512, 513, 8192} {
			got, err := runArm64WrapperMem(t, linearSumLoopModuleARM64(t), n, func(mem []byte) {
				for i := 0; i+8 <= len(mem); i += 8 {
					binary.LittleEndian.PutUint64(mem[i:], uint64(i/8+1))
				}
			})
			if err != nil || uint64(got) != uint64(n)*uint64(n+1)/2 {
				t.Fatal(variant, n, got, err)
			}
		}
	}
}
func TestExperimentalSharedPlansARM64(t *testing.T) {
	for _, name := range []string{"map-i32", "map-f32", "dependent-i32", "dependent-f64", "simd-i32"} {
		raw, err := os.ReadFile("../amd64/testdata/loop_experiment/" + name + ".wasm")
		if err != nil {
			t.Fatal(err)
		}
		m, err := wasm.DecodeModule(raw)
		if err != nil {
			t.Fatal(err)
		}
		modes := []string{"count2", "count4", "guard2"}
		step := uint64(4)
		if name == "dependent-f64" {
			step = 8
		}
		if name == "simd-i32" {
			modes, step = []string{"simd2", "simd4"}, 16
		}
		if name == "map-i32" {
			modes = append(modes, "vector-i32-assert")
		}
		if name == "map-f32" {
			modes = append(modes, "vector-f32-assert")
		}
		for _, mode := range modes {
			for _, n := range []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16, 17} {
				scale, bias := uint64(3), uint64(7)
				if name == "map-f32" {
					scale, bias = uint64(math.Float32bits(3)), uint64(math.Float32bits(7))
				}
				got, err := runArm64WrapperWithOptions(t, m, CompileOptions{ExperimentalLoopMode: mode}, 128, 32768, n, scale, bias)
				if err != nil || got != 128+n*step {
					t.Fatal(name, mode, n, got, err)
				}
			}
		}
	}
}
func BenchmarkExperimentalSumMatrixARM64(b *testing.B) {
	m := linearSumLoopModuleARM64(b)
	b.Run("compile", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			cm, err := CompileModule(m)
			if err != nil {
				b.Fatal(err)
			}
			cm.CodeImage.Close()
		}
	})
	cm, err := CompileModule(m)
	if err != nil {
		b.Fatal(err)
	}
	defer cm.CodeImage.Close()
	eng, err := coreruntime.NewEngine()
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	jm, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		b.Fatal(err)
	}
	defer jm.Close()
	ar, err := coreruntime.NewArena(4096)
	if err != nil {
		b.Fatal(err)
	}
	defer ar.Close()
	code, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		b.Fatal(err)
	}
	defer coreruntime.Unmap(code)
	for i := 0; i < 8192; i++ {
		binary.LittleEndian.PutUint64(jm.CurrentBytes()[i*8:], uint64(i+1))
	}
	args, out, trap := ar.Alloc(8), ar.Alloc(8), ar.Alloc(coreruntime.TrapBufferBytes)
	for _, n := range []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16, 17, 511, 512, 513, 8192} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			binary.LittleEndian.PutUint32(args, n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, out); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if got, want := binary.LittleEndian.Uint64(out), uint64(n)*uint64(n+1)/2; got != want {
				b.Fatal(got, want)
			}
		})
	}
}
