//go:build (linux || darwin) && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

var carryArithmeticBenchmarkSink uint64

func widenedCarryArithmeticBenchmarkModule(tb testing.TB) *wasm.Module {
	tb.Helper()
	body := []byte{0x01, 0x01, 0x7e} // one i64 result local
	for i := range 128 {
		compare := byte(0x54) // i64.lt_u
		if i&1 != 0 {
			compare = 0x56 // i64.gt_u: exercise reversed-CMP carry selection.
		}
		body = append(body,
			0x20, 0x00, // x
			0x20, 0x01, 0x20, 0x02, compare,
			0xad, 0x7c, // widen carry; x + carry
			0x21, 0x03, // local.set result
		)
	}
	body = append(body, 0x20, 0x03, 0x0b)
	return mod1(tb, []wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, body)
}

func BenchmarkWidenedCarryArithmetic(b *testing.B) {
	m := widenedCarryArithmeticBenchmarkModule(b)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"materialized", false}, {"adc", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{
				Optimizations: map[string]bool{"widened-carry-arith": tc.on},
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
			args, results, trap := arena.Alloc(24), arena.Alloc(8), arena.Alloc(8)
			binary.LittleEndian.PutUint64(args, 7)
			binary.LittleEndian.PutUint64(args[8:], 1)
			binary.LittleEndian.PutUint64(args[16:], 2)
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(stats.Funcs[0].CodeBytes), "native-bytes")
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			carryArithmeticBenchmarkSink = binary.LittleEndian.Uint64(results)
		})
	}
}

func BenchmarkCompileWidenedCarryArithmetic(b *testing.B) {
	m := widenedCarryArithmeticBenchmarkModule(b)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"materialized", false}, {"adc", true}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"widened-carry-arith": tc.on}})
				if err != nil {
					b.Fatal(err)
				}
				benchCompiledSink = cm
			}
		})
	}
}
