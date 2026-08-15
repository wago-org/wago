//go:build (linux || darwin) && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

var carryBenchmarkSink uint64

func teeAddCarryBenchmarkModule(tb testing.TB) *wasm.Module {
	tb.Helper()
	body := []byte{0x01, 0x01, 0x7e} // one i64 local: sum
	for i := 0; i < 128; i++ {
		body = append(body,
			0x20, 0x00, 0x20, 0x01, 0x7c, // a + b
			0x22, 0x02, // local.tee sum
			0x20, 0x00, 0x54, // sum <u a
			0xad, // i64.extend_i32_u
		)
		if i != 0 {
			body = append(body, 0x7c) // accumulate carry results
		}
	}
	body = append(body, 0x0b)
	return mod1(tb, []wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, body)
}

func BenchmarkTeeAddCarry(b *testing.B) {
	m := teeAddCarryBenchmarkModule(b)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"compare", false}, {"flags", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{
				Optimizations: map[string]bool{"tee-add-carry": tc.on},
				Stats:         &stats,
			})
			if err != nil {
				b.Fatal(err)
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
			args, results, trap := arena.Alloc(16), arena.Alloc(8), arena.Alloc(8)
			binary.LittleEndian.PutUint64(args, ^uint64(0))
			binary.LittleEndian.PutUint64(args[8:], 1)
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(stats.Funcs[0].CodeBytes), "native-bytes")
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			carryBenchmarkSink = binary.LittleEndian.Uint64(results)
		})
	}
}

func BenchmarkCompileTeeAddCarry(b *testing.B) {
	m := teeAddCarryBenchmarkModule(b)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"compare", false}, {"flags", true}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"tee-add-carry": tc.on}})
				if err != nil {
					b.Fatal(err)
				}
				benchCompiledSink = cm
			}
		})
	}
}
