//go:build (linux || darwin) && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

var bmi2ShiftBenchmarkSink uint64

func bmi2VariableShiftBenchmarkModule(tb testing.TB) *wasm.Module {
	tb.Helper()
	body := []byte{0x00, 0x20, 0x00} // no locals; initial i64 value
	for i := 0; i < 128; i++ {
		body = append(body, 0x20, 0x01, []byte{0x86, 0x88, 0x87}[i%3])
	}
	body = append(body, 0x0b)
	return mod1(tb, []wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, body)
}

func BenchmarkBMI2VariableShifts(b *testing.B) {
	m := bmi2VariableShiftBenchmarkModule(b)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"legacy-cl", false}, {"bmi2", true}} {
		b.Run(tc.name, func(b *testing.B) {
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{
				Optimizations: map[string]bool{"bmi2-shifts": tc.on},
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
			binary.LittleEndian.PutUint64(args, 0x9e3779b97f4a7c15)
			binary.LittleEndian.PutUint64(args[8:], 17)
			nativeBytes := 0
			for _, fnStats := range stats.Funcs {
				nativeBytes += fnStats.CodeBytes
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.ReportMetric(float64(nativeBytes), "native-bytes")
			for i := 0; i < b.N; i++ {
				if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
					b.Fatal(err)
				}
			}
			bmi2ShiftBenchmarkSink = binary.LittleEndian.Uint64(results)
		})
	}
}

func BenchmarkCompileBMI2VariableShifts(b *testing.B) {
	m := bmi2VariableShiftBenchmarkModule(b)
	for _, tc := range []struct {
		name string
		on   bool
	}{{"legacy-cl", false}, {"bmi2", true}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModuleWith(m, CompileOptions{Optimizations: map[string]bool{"bmi2-shifts": tc.on}})
				if err != nil {
					b.Fatal(err)
				}
				benchCompiledSink = cm
			}
		})
	}
}
