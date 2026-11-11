//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const scalarFloatSelectIterations = 10000

// Keep the condition work in both revisions. The predictable case has a fixed
// seed; the unpredictable case advances xorshift32 after each select.
func scalarFloatSelectBenchmarkModule(t testing.TB, wide, unpredictable bool) *wasm.Module {
	t.Helper()
	ft, reinterpret := wasm.F32, byte(0xbc)
	if wide {
		ft, reinterpret = wasm.F64, 0xbd
	}
	body := []byte{2, 1, 0x7f, 1, 0x7e, 0x03, 0x40} // i32/i64 locals; loop
	body = append(body, 0x20, 5, 0x20, 0, 0x20, 1, 0x20, 2, 0x41, 1, 0x71, 0x45, 0x1b, reinterpret)
	if !wide {
		body = append(body, 0xad) // i64.extend_i32_u
	}
	body = append(body, 0x7c, 0x21, 5) // Add selected bits into checksum
	if unpredictable {
		for _, shift := range []struct{ count, opcode byte }{{13, 0x74}, {17, 0x76}, {5, 0x74}} {
			body = append(body, 0x20, 2, 0x20, 2, 0x41, shift.count, shift.opcode, 0x73, 0x21, 2)
		}
	}
	body = append(body, 0x20, 4, 0x41, 1, 0x6a, 0x22, 4, 0x20, 3, 0x49, 0x0d, 0, 0x0b, 0x20, 5, 0x0b)
	return mod1(t, []wasm.ValType{ft, ft, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, body)
}

func scalarFloatSelectBenchmarkCases(f func(string, bool, bool)) {
	for _, wide := range []bool{false, true} {
		name := "f32/"
		if wide {
			name = "f64/"
		}
		f(name+"predictable", wide, false)
		f(name+"unpredictable", wide, true)
	}
}

func BenchmarkScalarFloatSelectARM64(b *testing.B) {
	scalarFloatSelectBenchmarkCases(func(name string, wide, unpredictable bool) {
		b.Run(name, func(b *testing.B) {
			m := scalarFloatSelectBenchmarkModule(b, wide, unpredictable)
			cm, err := CompileModuleWith(m, CompileOptions{})
			if err != nil {
				b.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
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
			arena, err := coreruntime.NewArena(4096)
			if err != nil {
				b.Fatal(err)
			}
			defer arena.Close()
			code, entry, err := coreruntime.MapCode(cm.Code)
			if err != nil {
				b.Fatal(err)
			}
			defer coreruntime.Unmap(code)
			args, results, trap := arena.Alloc(32), arena.Alloc(8), arena.Alloc(coreruntime.TrapBufferBytes)
			a, other := uint64(0x7f812345), uint64(0x80000000)
			if wide {
				a, other = 0x7ff0000000001234, 0x8000000000000000
			}
			seed := uint32(0)
			if unpredictable {
				seed = 0x12345678
			}
			for i, value := range []uint64{a, other, uint64(seed), scalarFloatSelectIterations} {
				binary.LittleEndian.PutUint64(args[8*i:], value)
			}
			var want uint64
			for range scalarFloatSelectIterations {
				if seed&1 == 0 {
					want += a
				} else {
					want += other
				}
				if unpredictable {
					seed ^= seed << 13
					seed ^= seed >> 17
					seed ^= seed << 5
				}
			}
			call := func() {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			call()
			if got := binary.LittleEndian.Uint64(results); got != want {
				b.Fatalf("checksum = %x, want %x", got, want)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				call()
			}
			b.StopTimer()
			if got := binary.LittleEndian.Uint64(results); got != want {
				b.Fatalf("checksum = %x, want %x", got, want)
			}
			b.ReportMetric(float64(len(cm.Code)), "native-bytes")
			b.ReportMetric(scalarFloatSelectIterations, "selects/op")
		})
	})
}

func BenchmarkCompileScalarFloatSelectARM64(b *testing.B) {
	scalarFloatSelectBenchmarkCases(func(name string, wide, unpredictable bool) {
		b.Run(name, func(b *testing.B) {
			m := scalarFloatSelectBenchmarkModule(b, wide, unpredictable)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				cm, err := CompileModuleWith(m, CompileOptions{})
				if err != nil {
					b.Fatal(err)
				}
				if cm.CodeImage != nil {
					_ = cm.CodeImage.Close()
				}
			}
		})
	})
}
